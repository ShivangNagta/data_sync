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
	// Propagate local deletions to the server FIRST. A deleted file is
	// excluded from the manifest, so without an explicit delete the server
	// would treat its absence as "unknown file" and plan a download for it -
	// re-downloading what the user just deleted.
	ops, err := storage.GetPendingOps(e.db)
	if err != nil {
		return fmt.Errorf("get pending ops: %w", err)
	}
	pendingSet := make(map[string]bool, len(ops))
	for _, op := range ops {
		pendingSet[op.Path] = true
		if op.OpType == "delete" {
			if err := e.deleteRemote(ctx, op.Path); err != nil {
				return fmt.Errorf("delete %s: %w", op.Path, err)
			}
			// Server acknowledged the deletion; stop tracking the file so it
			// never re-enters the manifest.
			if err := storage.Untrack(e.db, op.Path); err != nil {
				return fmt.Errorf("untrack %s: %w", op.Path, err)
			}
		}
	}

	manifest, _, err := buildManifest(e.db, root)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}

	req := &sync.GetSyncPlanRequest{}
	for _, f := range manifest {
		req.LocalFiles = append(req.LocalFiles, f)
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
			// Protect un-synced local edits: if we changed this file since the
			// deletion, our newer edit wins (last-write-wins) - upload it.
			if pendingSet[action.Path] {
				if err := e.upload(ctx, root, action.Path); err != nil {
					return fmt.Errorf("re-upload %s: %w", action.Path, err)
				}
				continue
			}
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
	if err := os.Rename(tmpName, full); err != nil {
		return err
	}
	// Record the file so the next manifest sees it as already-synced instead
	// of re-downloading it (or re-uploading it as a new local file).
	return storage.MarkDownloaded(e.db, action.Path, action.Size, action.Hash)
}

func (e *SyncEngine) deleteLocal(root, path string) error {
	full := filepath.Join(root, filepath.FromSlash(path))
	// The file may already be gone (e.g. server and local deletes raced);
	// that's fine - the goal is its absence.
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Stop tracking it so it doesn't get re-reported as a local file.
	return storage.Untrack(e.db, path)
}

// deleteRemote tells the server to tombstone a file (delete on all devices).
func (e *SyncEngine) deleteRemote(ctx context.Context, path string) error {
	_, err := e.client.API().DeleteFile(
		e.client.AuthContext(ctx),
		&sync.DeleteFileRequest{Path: path},
	)
	return err
}
