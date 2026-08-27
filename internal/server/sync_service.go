package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shivangnagta/data_sync/proto/sync"
)

// SyncService implements the gRPC SyncServiceServer interface.
// It coordinates metadata (Turso) and bytes (R2).
type SyncService struct {
	sync.UnimplementedSyncServiceServer
	devices  *DeviceRepository
	files    *FileRepository
	versions *VersionRepository
	r2       *R2Client
}

func NewSyncService(
	devices *DeviceRepository,
	files *FileRepository,
	versions *VersionRepository,
	r2 *R2Client,
) *SyncService {
	return &SyncService{
		devices:  devices,
		files:    files,
		versions: versions,
		r2:       r2,
	}
}

// RegisterDevice creates a device and returns its ID + bearer token.
func (s *SyncService) RegisterDevice(ctx context.Context, req *sync.RegisterDeviceRequest) (*sync.RegisterDeviceResponse, error) {
	deviceID := newID()
	token := newToken()

	if err := s.devices.RegisterDevice(ctx, deviceID, req.Name, token); err != nil {
		return nil, status.Errorf(codes.Internal, "register device: %v", err)
	}

	return &sync.RegisterDeviceResponse{
		DeviceId: deviceID,
		Token:    token,
	}, nil
}

// GetSyncPlan compares the client's manifest against server state (Turso)
// and returns what the client must upload, download, or delete.
func (s *SyncService) GetSyncPlan(ctx context.Context, req *sync.GetSyncPlanRequest) (*sync.GetSyncPlanResponse, error) {
	resp := &sync.GetSyncPlanResponse{}

	// Build a lookup of what the client has: path -> FileState
	clientFiles := make(map[string]*sync.FileState, len(req.LocalFiles))
	for _, f := range req.LocalFiles {
		clientFiles[f.Path] = f
	}

	for path, clientState := range clientFiles {
		serverFile, exists, err := s.files.GetFileByPath(ctx, path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get file: %v", err)
		}

		// Server doesn't know this file -> client should upload it.
		if !exists {
			resp.Actions = append(resp.Actions, &sync.SyncAction{
				Path:   path,
				Action: sync.SyncAction_UPLOAD,
			})
			continue
		}

		// Compare content hash. If different, the server has a newer version
		// than the client -> client should download.
		head, hasVersion, err := s.versions.GetHeadVersion(ctx, serverFile.FileID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get head version: %v", err)
		}
		if hasVersion && head.RootHash != clientState.Hash {
			resp.Actions = append(resp.Actions, &sync.SyncAction{
				Path:       path,
				Action:     sync.SyncAction_DOWNLOAD,
				VersionId:  head.VersionID,
				Hash:       head.RootHash,
				Size:       head.Size,
			})
		}
	}

	return resp, nil
}

// UploadFile accepts a streamed file from a client and stores it in R2 +
// records the new version in Turso. Currently uses last-writer-wins.
func (s *SyncService) UploadFile(stream sync.SyncService_UploadFileServer) error {
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

	// Integrity check: verify the received bytes match the claimed hash.
	if err := verifyHash(data, meta.Hash); err != nil {
		return status.Errorf(codes.InvalidArgument, "hash mismatch: %v", err)
	}

	foundFile, found, err := s.files.GetFileByPath(stream.Context(), meta.Path)
	if err != nil {
		return status.Errorf(codes.Internal, "get file: %v", err)
	}

	var fileID, baseVersion string
	if found {
		fileID = foundFile.FileID
		head, _, err := s.versions.GetHeadVersion(stream.Context(), fileID)
		if err != nil {
			return status.Errorf(codes.Internal, "get head: %v", err)
		}
		baseVersion = head.VersionID
	} else {
		fileID = newID()
		if err := s.files.CreateFile(stream.Context(), fileID, meta.Path); err != nil {
			return status.Errorf(codes.Internal, "create file: %v", err)
		}
	}

	versionID := newID()

	// Store bytes in R2, then record version in Turso.
	if err := s.r2.Put(stream.Context(), fileID, versionID, data); err != nil {
		return status.Errorf(codes.Internal, "store in r2: %v", err)
	}

	version := FileVersion{
		VersionID:     versionID,
		FileID:        fileID,
		DeviceID:      "",
		BaseVersionID: baseVersion,
		Size:          int64(len(data)),
		RootHash:      meta.Hash,
	}
	if err := s.versions.InsertVersion(stream.Context(), version); err != nil {
		return status.Errorf(codes.Internal, "record version: %v", err)
	}
	if err := s.files.SetCurrentVersion(stream.Context(), fileID, versionID); err != nil {
		return status.Errorf(codes.Internal, "set current version: %v", err)
	}

	return stream.SendAndClose(&sync.UploadFileResponse{
		Accepted:     true,
		NewVersionId: versionID,
	})
}

// DownloadFile streams a file's bytes back to the client.
func (s *SyncService) DownloadFile(req *sync.DownloadFileRequest, stream sync.SyncService_DownloadFileServer) error {
	file, found, err := s.files.GetFileByPath(stream.Context(), req.Path)
	if err != nil {
		return status.Errorf(codes.Internal, "get file: %v", err)
	}
	if !found {
		return status.Errorf(codes.NotFound, "file not found: %s", req.Path)
	}

	head, hasVersion, err := s.versions.GetHeadVersion(stream.Context(), file.FileID)
	if err != nil {
		return status.Errorf(codes.Internal, "get head: %v", err)
	}
	if !hasVersion {
		return status.Errorf(codes.NotFound, "no version for file: %s", req.Path)
	}

	data, err := s.r2.Get(stream.Context(), file.FileID, head.VersionID)
	if err != nil {
		return status.Errorf(codes.Internal, "get from r2: %v", err)
	}

	// Send metadata first, then stream the data in chunks.
	if err := stream.Send(&sync.DownloadFileResponse{
		Payload: &sync.DownloadFileResponse_Meta{
			Meta: &sync.DownloadFileMeta{
				Path:      req.Path,
				VersionId: head.VersionID,
				Size:      int64(len(data)),
				Hash:      head.RootHash,
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

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func verifyHash(data []byte, claim string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != claim {
		return fmt.Errorf("expected %s, got %s", claim, actual)
	}
	return nil
}
