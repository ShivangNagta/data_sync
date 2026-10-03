package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

type SyncService struct {
	files *FileRepository
	r2    *R2Client
	hub   *Hub
}

func NewSyncService(files *FileRepository, r2 *R2Client, hub *Hub) *SyncService {
	return &SyncService{files: files, r2: r2, hub: hub}
}

type SyncAction struct {
	Path   string
	Action string // "upload", "download", "delete"
	Hash   string
	Size   int64
}

type FileState struct {
	Path string
	Size int64
	Hash string
}

type Uploader struct {
	DeviceID string
}

func (s *SyncService) ComputeSyncPlan(ctx context.Context, clientFiles map[string]FileState) ([]SyncAction, error) {
	var actions []SyncAction

	for path, clientState := range clientFiles {
		serverFile, exists, err := s.files.GetFileByPath(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("get file: %w", err)
		}

		if !exists {
			actions = append(actions, SyncAction{Path: path, Action: "upload"})
			continue
		}

		if serverFile.IsDeleted() {
			actions = append(actions, SyncAction{Path: path, Action: "delete"})
			continue
		}

		if serverFile.Hash != clientState.Hash {
			actions = append(actions, SyncAction{
				Path:   path,
				Action: "download",
				Hash:   serverFile.Hash,
				Size:   serverFile.Size,
			})
		}
	}

	serverFiles, err := s.files.ListAllFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list server files: %w", err)
	}
	for _, sf := range serverFiles {
		if _, known := clientFiles[sf.Path]; known {
			continue
		}
		actions = append(actions, SyncAction{
			Path:   sf.Path,
			Action: "download",
			Hash:   sf.Hash,
			Size:   sf.Size,
		})
	}

	return actions, nil
}

func (s *SyncService) ApplyUpload(ctx context.Context, path string, data []byte, hash string, lastSeenHash string) error {
	if err := verifyHash(data, hash); err != nil {
		return err
	}

	found, exists, err := s.files.GetFileByPath(ctx, path)
	if err != nil {
		return fmt.Errorf("get file: %w", err)
	}

	if exists && !found.IsDeleted() && found.Hash != lastSeenHash {
		return fmt.Errorf("conflict: current hash is %s", found.Hash)
	}

	if err := s.r2.Put(ctx, hash, data); err != nil {
		return fmt.Errorf("store in r2: %w", err)
	}

	if err := s.files.UpsertFile(ctx, path, hash, int64(len(data))); err != nil {
		return fmt.Errorf("upsert file: %w", err)
	}

	BroadcastFileChange(s.hub, path)

	return nil
}

func (s *SyncService) ApplyDelete(ctx context.Context, path string) error {
	found, exists, err := s.files.GetFileByPath(ctx, path)
	if err != nil {
		return fmt.Errorf("get file: %w", err)
	}
	if !exists || found.IsDeleted() {
		return nil
	}

	if err := s.r2.Delete(ctx, path); err != nil {
		return fmt.Errorf("delete from r2: %w", err)
	}

	if err := s.files.MarkDeleted(ctx, path); err != nil {
		return fmt.Errorf("mark deleted: %w", err)
	}

	BroadcastFileChange(s.hub, path)

	return nil
}

func (s *SyncService) FetchFile(ctx context.Context, path string) (data []byte, hash string, size int64, err error) {
	found, exists, err := s.files.GetFileByPath(ctx, path)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get file: %w", err)
	}
	if !exists {
		return nil, "", 0, errors.New("file not found")
	}

	data, err = s.r2.Get(ctx, path)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get from r2: %w", err)
	}

	return data, found.Hash, found.Size, nil
}

func verifyHash(data []byte, claim string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != claim {
		return fmt.Errorf("expected %s, got %s", claim, actual)
	}
	return nil
}
