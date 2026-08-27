// This file contains the sync engine: it fetches the sync plan from the
// server and executes (upload/download) it against the local folder.

package client

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shivangnagta/data_sync/internal/client/storage"
	"github.com/shivangnagta/data_sync/proto/sync"
)

// SyncEngine coordinates talking to the server and applying changes locally.
type SyncEngine struct {
	client *SyncClient
	db     *sql.DB // local SQLite used for pending operations tracking
}

func NewSyncEngine(c *SyncClient, db *sql.DB) *SyncEngine {
	return &SyncEngine{client: c, db: db}
}

// Sync runs one full sync pass: build manifest, ask the server for a plan,
// execute it, and mark completed pending ops.
func (e *SyncEngine) Sync(ctx context.Context, root string) error {
	manifest, pending, err := buildManifest(e.db, root)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}

	req := &sync.GetSyncPlanRequest{}
	for _, f := range manifest {
		req.LocalFiles = append(req.LocalFiles, f)
	}

	// Paths with pending local edits must be uploaded, never overwritten by a
	// download. The manifest returned them; mark them so execution protects them.
	pendingSet := make(map[string]bool, len(pending))
	for _, p := range pending {
		pendingSet[p] = true
	}

	resp, err := e.client.API().GetSyncPlan(e.client.AuthContext(ctx), req)
	if err != nil {
		return fmt.Errorf("get sync plan: %w", err)
	}

	fmt.Printf("sync: plan has %d action(s) for %d local file(s)\n", len(resp.Actions), len(req.LocalFiles))
	for _, action := range resp.Actions {
		fmt.Printf("sync: %s %s\n", action.Action, action.Path)
		switch action.Action {
		case sync.SyncAction_UPLOAD:
			if err := e.upload(ctx, root, action.Path); err != nil {
				return fmt.Errorf("upload %s: %w", action.Path, err)
			}
		case sync.SyncAction_DOWNLOAD:
			// Protect un-synced local edits: if we have a pending change for
			// this file, upload our version instead of overwriting it.
			if pendingSet[action.Path] {
				if err := e.upload(ctx, root, action.Path); err != nil {
					return fmt.Errorf("re-upload %s: %w", action.Path, err)
				}
				continue
			}
			if err := e.download(ctx, root, action); err != nil {
				return fmt.Errorf("download %s: %w", action.Path, err)
			}
		case sync.SyncAction_DELETE:
			if err := e.deleteLocal(root, action.Path); err != nil {
				return fmt.Errorf("delete %s: %w", action.Path, err)
			}
		}
	}

	// Mark all pending ops as completed after a successful pass.
	return storage.MarkAllCompleted(e.db)
}

func (e *SyncEngine) upload(ctx context.Context, root, path string) error {
	full := filepath.Join(root, filepath.FromSlash(path))
	content, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	size, hash, err := storage.HashFileContent(full)
	if err != nil {
		return err
	}

	stream, err := e.client.API().UploadFile(e.client.AuthContext(ctx))
	if err != nil {
		return err
	}
	if err := stream.Send(&sync.UploadFileRequest{
		Payload: &sync.UploadFileRequest_Meta{
			Meta: &sync.UploadFileMeta{Path: path, Size: size, Hash: hash},
		},
	}); err != nil {
		return err
	}
	// whole-file transfer as a single data message.
	// TODO: Add chunking system
	if err := stream.Send(&sync.UploadFileRequest{
		Payload: &sync.UploadFileRequest_Data{Data: content},
	}); err != nil {
		return err
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return err
	}
	return nil
}

func (e *SyncEngine) download(ctx context.Context, root string, action *sync.SyncAction) error {
	full := filepath.Join(root, filepath.FromSlash(action.Path))
	stream, err := e.client.API().DownloadFile(e.client.AuthContext(ctx), &sync.DownloadFileRequest{Path: action.Path})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}

	// Write to a temp file, then rename into place so we never leave a
	// half-written file at the final path.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".sync-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			tmp.Close()
			return err
		}
		if d, ok := msg.Payload.(*sync.DownloadFileResponse_Data); ok {
			if _, err := tmp.Write(d.Data); err != nil {
				tmp.Close()
				return err
			}
		}
	}

	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}

func (e *SyncEngine) deleteLocal(root, path string) error {
	full := filepath.Join(root, filepath.FromSlash(path))
	return os.Remove(full)
}
