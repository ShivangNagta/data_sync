package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// SyncService is the business (application) layer.
// It contains the sync rules and operates on domain types,
// independent of the transport (gRPC) protocol.
type SyncService struct {
	files    *FileRepository
	versions *VersionRepository
	devices  *DeviceRepository
	r2       *R2Client
}

// NewSyncService constructs the application (business) layer.
func NewSyncService(files *FileRepository, versions *VersionRepository, devices *DeviceRepository, r2 *R2Client) *SyncService {
	return &SyncService{files: files, versions: versions, devices: devices, r2: r2}
}

// SyncAction represents one directive in a sync plan.
type SyncAction struct {
	Path      string
	Action    string // "upload", "download", "delete"
	VersionID string
	Hash      string
	Size      int64
}

// Uploader identifies the device performing the upload.
type Uploader struct {
	DeviceID string
}

// RegisterDevice creates a device and returns its ID and bearer token.
func (s *SyncService) RegisterDevice(ctx context.Context, name, deviceID, token string) error {
	if err := s.devices.RegisterDevice(ctx, deviceID, name, token); err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

// ComputeSyncPlan compares the client's manifest against server state
// and returns what the client must upload, download, or delete.
func (s *SyncService) ComputeSyncPlan(ctx context.Context, clientFiles map[string]FileState) ([]SyncAction, error) {
	var actions []SyncAction

	for path, clientState := range clientFiles {
		serverFile, exists, err := s.files.GetFileByPath(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("get file: %w", err)
		}

		// Server doesn't know this file -> client should upload it.
		if !exists {
			actions = append(actions, SyncAction{Path: path, Action: "upload"})
			continue
		}

		// If the server has a file but no version yet (e.g. an interrupted or
		// orphaned upload), the client must upload to give it content.
		head, hasVersion, err := s.versions.GetHeadVersion(ctx, serverFile.FileID)
		if err != nil {
			return nil, fmt.Errorf("get head version: %w", err)
		}
		if !hasVersion {
			actions = append(actions, SyncAction{Path: path, Action: "upload"})
			continue
		}

		// Compare content hash. If different, the server's head is
		// ahead of the client -> client should download.
		if head.RootHash != clientState.Hash {
			actions = append(actions, SyncAction{
				Path:      path,
				Action:    "download",
				VersionID: head.VersionID,
				Hash:      head.RootHash,
				Size:      head.Size,
			})
		}
	}

	return actions, nil
}

// ApplyUpload stores a new version of a file. MVP uses last-writer-wins.
// Returns the new version id and whether it superseded an existing head.
func (s *SyncService) ApplyUpload(ctx context.Context, path string, data []byte, hash string, up Uploader) (versionID string, err error) {
	// Integrity verification before any state change.
	if err := verifyHash(data, hash); err != nil {
		return "", err
	}

	found, exists, err := s.files.GetFileByPath(ctx, path)
	if err != nil {
		return "", fmt.Errorf("get file: %w", err)
	}

	var fileID, baseVersion string
	if exists {
		fileID = found.FileID
		head, _, err := s.versions.GetHeadVersion(ctx, fileID)
		if err != nil {
			return "", fmt.Errorf("get head: %w", err)
		}
		baseVersion = head.VersionID
	} else {
		fileID = newID()
		if err := s.files.CreateFile(ctx, fileID, path); err != nil {
			return "", fmt.Errorf("create file: %w", err)
		}
	}

	versionID = newID()

	// Commit order: bytes to R2 first, then record version + advance head.
	// ("upload bytes first, verify them, then advance the canonical version")
	if err := s.r2.Put(ctx, fileID, versionID, data); err != nil {
		return "", fmt.Errorf("store in r2: %w", err)
	}

	version := FileVersion{
		VersionID:     versionID,
		FileID:        fileID,
		DeviceID:      up.DeviceID,
		BaseVersionID: baseVersion,
		Size:          int64(len(data)),
		RootHash:      hash,
	}
	if err := s.versions.InsertVersion(ctx, version); err != nil {
		return "", fmt.Errorf("record version: %w", err)
	}
	if err := s.files.SetCurrentVersion(ctx, fileID, versionID); err != nil {
		return "", fmt.Errorf("set current version: %w", err)
	}

	return versionID, nil
}

// FetchFile retrieves the current bytes for a file path.
func (s *SyncService) FetchFile(ctx context.Context, path string) (data []byte, versionID, hash string, size int64, err error) {
	found, exists, err := s.files.GetFileByPath(ctx, path)
	if err != nil {
		return nil, "", "", 0, fmt.Errorf("get file: %w", err)
	}
	if !exists {
		return nil, "", "", 0, errors.New("file not found")
	}

	head, hasVersion, err := s.versions.GetHeadVersion(ctx, found.FileID)
	if err != nil {
		return nil, "", "", 0, fmt.Errorf("get head: %w", err)
	}
	if !hasVersion {
		return nil, "", "", 0, errors.New("no version for file")
	}

	data, err = s.r2.Get(ctx, found.FileID, head.VersionID)
	if err != nil {
		return nil, "", "", 0, fmt.Errorf("get from r2: %w", err)
	}

	return data, head.VersionID, head.RootHash, head.Size, nil
}

// FileState is a client-reported file in its manifest.
type FileState struct {
	Path string
	Size int64
	Hash string
}

func verifyHash(data []byte, claim string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != claim {
		return fmt.Errorf("expected %s, got %s", claim, actual)
	}
	return nil
}
