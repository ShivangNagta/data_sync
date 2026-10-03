package server

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shivangnagta/data_sync/proto/sync"
)

type Service struct {
	sync.UnimplementedSyncServiceServer
	app  *SyncService
	auth *AuthInterceptor
}

func NewService(app *SyncService, auth *AuthInterceptor) *Service {
	return &Service{app: app, auth: auth}
}

func (s *Service) GetSyncPlan(ctx context.Context, req *sync.GetSyncPlanRequest) (*sync.GetSyncPlanResponse, error) {
	manifest := make(map[string]FileState, len(req.LocalFiles))
	for _, f := range req.LocalFiles {
		manifest[f.Path] = FileState{Path: f.Path, Size: f.Size, Hash: f.Hash}
	}

	actions, err := s.app.ComputeSyncPlan(ctx, manifest)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "compute plan: %v", err)
	}

	resp := &sync.GetSyncPlanResponse{}
	for _, a := range actions {
		resp.Actions = append(resp.Actions, &sync.SyncAction{
			Path:   a.Path,
			Action: actionToProto(a.Action),
			Hash:   a.Hash,
			Size:   a.Size,
		})
	}

	return resp, nil
}

func (s *Service) UploadFile(stream sync.SyncService_UploadFileServer) error {
	var meta *sync.UploadFileMeta
	var data []byte

	for {
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return status.Errorf(codes.Internal, "receive upload: %v", err)
		}

		switch p := req.Payload.(type) {
		case *sync.UploadFileRequest_Meta:
			meta = p.Meta
		case *sync.UploadFileRequest_Data:
			data = append(data, p.Data...)
		}
	}

	if meta == nil {
		return status.Error(codes.InvalidArgument, "missing upload metadata")
	}

	if err := s.app.ApplyUpload(stream.Context(), meta.Path, data, meta.Hash); err != nil {
		return status.Errorf(codes.Internal, "apply upload: %v", err)
	}

	return stream.SendAndClose(&sync.UploadFileResponse{Accepted: true})
}

func (s *Service) DownloadFile(req *sync.DownloadFileRequest, stream sync.SyncService_DownloadFileServer) error {
	data, hash, size, err := s.app.FetchFile(stream.Context(), req.Path)
	if err != nil {
		return status.Errorf(codes.NotFound, "fetch file: %v", err)
	}

	if err := stream.Send(&sync.DownloadFileResponse{
		Payload: &sync.DownloadFileResponse_Meta{
			Meta: &sync.DownloadFileMeta{
				Path: req.Path,
				Size: size,
				Hash: hash,
			},
		},
	}); err != nil {
		return err
	}

	const chunkSize = 64 * 1024
	for len(data) > 0 {
		n := len(data)
		if n > chunkSize {
			n = chunkSize
		}
		if err := stream.Send(&sync.DownloadFileResponse{
			Payload: &sync.DownloadFileResponse_Data{
				Data: data[:n],
			},
		}); err != nil {
			return err
		}
		data = data[n:]
	}

	return nil
}

func (s *Service) DeleteFile(ctx context.Context, req *sync.DeleteFileRequest) (*sync.DeleteFileResponse, error) {
	if err := s.app.ApplyDelete(ctx, req.Path); err != nil {
		return nil, status.Errorf(codes.Internal, "apply delete: %v", err)
	}
	return &sync.DeleteFileResponse{Deleted: true}, nil
}

func actionToProto(action string) sync.SyncAction_ActionType {
	switch action {
	case "upload":
		return sync.SyncAction_UPLOAD
	case "download":
		return sync.SyncAction_DOWNLOAD
	case "delete":
		return sync.SyncAction_DELETE
	default:
		return sync.SyncAction_UPLOAD
	}
}
