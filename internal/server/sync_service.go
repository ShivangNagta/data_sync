package server

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shivangnagta/data_sync/proto/sync"
)

// SyncService is the gRPC transport (controller) layer.
// It translates between proto messages and domain types, delegating
// business logic to the application service. It stays thin.
type Service struct {
	sync.UnimplementedSyncServiceServer
	app *SyncService
	auth *AuthInterceptor
}

func NewService(app *SyncService, auth *AuthInterceptor) *Service {
	return &Service{app: app, auth: auth}
}

// RegisterDevice creates a device and returns its ID + bearer token.
func (s *Service) RegisterDevice(ctx context.Context, req *sync.RegisterDeviceRequest) (*sync.RegisterDeviceResponse, error) {
	deviceID := newID()
	token := newToken()

	if err := s.app.RegisterDevice(ctx, req.Name, deviceID, token); err != nil {
		return nil, status.Errorf(codes.Internal, "register device: %v", err)
	}

	return &sync.RegisterDeviceResponse{
		DeviceId: deviceID,
		Token:    token,
	}, nil
}

// GetSyncPlan delegates manifest comparison to the application layer.
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
			Path:      a.Path,
			Action:    actionToProto(a.Action),
			VersionId: a.VersionID,
			Hash:      a.Hash,
			Size:      a.Size,
		})
	}

	return resp, nil
}

// UploadFile accepts a streamed file and delegates storage to the app layer.
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

	up, ok := UploaderFrom(stream.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing device identity")
	}

	versionID, err := s.app.ApplyUpload(stream.Context(), meta.Path, data, meta.Hash, up)
	if err != nil {
		return status.Errorf(codes.Internal, "apply upload: %v", err)
	}

	return stream.SendAndClose(&sync.UploadFileResponse{
		Accepted:     true,
		NewVersionId: versionID,
	})
}

// DownloadFile streams a file's bytes back to the client.
func (s *Service) DownloadFile(req *sync.DownloadFileRequest, stream sync.SyncService_DownloadFileServer) error {
	data, versionID, hash, size, err := s.app.FetchFile(stream.Context(), req.Path)
	if err != nil {
		return status.Errorf(codes.NotFound, "fetch file: %v", err)
	}

	if err := stream.Send(&sync.DownloadFileResponse{
		Payload: &sync.DownloadFileResponse_Meta{
			Meta: &sync.DownloadFileMeta{
				Path:      req.Path,
				VersionId: versionID,
				Size:      size,
				Hash:      hash,
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
