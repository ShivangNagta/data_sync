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

type SyncEngine struct {
	client *SyncClient
	db     *sql.DB
}

func NewSyncEngine(c *SyncClient, db *sql.DB) *SyncEngine {
	return &SyncEngine{client: c, db: db}
}

func (e *SyncEngine) Sync(ctx context.Context, root string) error {
	ops, err := storage.GetPendingOps(e.db)
	if err != nil {
		return fmt.Errorf("get pending ops: %w", err)
	}
	for _, op := range ops {
		if op.OpType == "delete" {
			if err := e.deleteRemote(ctx, op.Path); err != nil {
				return fmt.Errorf("delete %s: %w", op.Path, err)
			}
			if err := storage.Untrack(e.db, op.Path); err != nil {
				return fmt.Errorf("untrack %s: %w", op.Path, err)
			}
		}
	}

	manifest, err := e.buildManifest(root)
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
			if err := e.download(ctx, root, action); err != nil {
				return fmt.Errorf("download %s: %w", action.Path, err)
			}
		case sync.SyncAction_DELETE:
			if err := e.deleteLocal(root, action.Path); err != nil {
				return fmt.Errorf("delete %s: %w", action.Path, err)
			}
		}
	}

	return storage.MarkAllCompleted(e.db)
}

func (e *SyncEngine) buildManifest(root string) (map[string]*sync.FileState, error) {
	tracked, err := storage.ListFiles(e.db)
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	manifest := make(map[string]*sync.FileState, len(tracked))
	for _, f := range tracked {
		p := filepath.ToSlash(f.Path)
		fs := &sync.FileState{Path: p, Size: f.Size, Hash: f.Hash}

		pendingOp, err := storage.PendingOpType(e.db, p)
		if err != nil {
			return nil, err
		}
		if pendingOp != "" {
			if pendingOp == "delete" {
				continue
			}
			full := filepath.Join(root, filepath.FromSlash(p))
			size, hash, err := storage.HashFileContent(full)
			if err != nil {
				return nil, err
			}
			fs.Size = size
			fs.Hash = hash
		}

		manifest[p] = fs
	}

	return manifest, nil
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
	if err := stream.Send(&sync.UploadFileRequest{
		Payload: &sync.UploadFileRequest_Data{Data: content},
	}); err != nil {
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
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
	return storage.MarkDownloaded(e.db, action.Path, action.Size, action.Hash)
}

func (e *SyncEngine) deleteLocal(root, path string) error {
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return storage.Untrack(e.db, path)
}

func (e *SyncEngine) deleteRemote(ctx context.Context, path string) error {
	_, err := e.client.API().DeleteFile(
		e.client.AuthContext(ctx),
		&sync.DeleteFileRequest{Path: path},
	)
	return err
}
