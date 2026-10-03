package client

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shivangnagta/data_sync/internal/client/storage"
)

type SyncEngine struct {
	backend SyncBackend
	token   string
	db      *sql.DB
	mu      sync.Mutex
}

var ignoredBase = map[string]bool{
	".ds_store": true,
}

func isIgnored(rel string) bool {
	base := filepath.Base(rel)
	return strings.HasPrefix(base, ".sync-tmp-") ||
		ignoredBase[strings.ToLower(base)]
}

func NewHTTPBackendEngine(backend *HTTPBackend, db *sql.DB) *SyncEngine {
	return &SyncEngine{backend: backend, token: backend.Token, db: db}
}

func (e *SyncEngine) EventsURL() string {
	return e.backend.EventsURL()
}

func (e *SyncEngine) Sync(ctx context.Context, root string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

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

	var files []*FileState
	for _, f := range manifest {
		files = append(files, f)
	}

	actions, err := e.backend.GetSyncPlan(ctx, files)
	if err != nil {
		return fmt.Errorf("get sync plan: %w", err)
	}

	fmt.Printf("sync: plan has %d action(s) for %d local file(s)\n", len(actions), len(files))
	for _, action := range actions {
		fmt.Printf("sync: %s %s\n", action.Action, action.Path)
		switch action.Action {
		case ActionUpload:
			if err := e.upload(ctx, root, action.Path); err != nil {
				return fmt.Errorf("upload %s: %w", action.Path, err)
			}
		case ActionDownload:
			if err := e.download(ctx, root, action); err != nil {
				return fmt.Errorf("download %s: %w", action.Path, err)
			}
		case ActionDelete:
			if err := e.deleteLocal(root, action.Path); err != nil {
				return fmt.Errorf("delete %s: %w", action.Path, err)
			}
		}
	}

	return storage.MarkAllCompleted(e.db)
}

func (e *SyncEngine) buildManifest(root string) (map[string]*FileState, error) {
	tracked, err := storage.ListFiles(e.db)
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	manifest := make(map[string]*FileState, len(tracked))
	for _, f := range tracked {
		p := filepath.ToSlash(f.Path)
		fs := &FileState{Path: p, Size: f.Size, Hash: f.Hash}
		lastSeenHash, err := storage.GetLastSeenHash(e.db, p)
		if err != nil {
			return nil, err
		}
		fs.LastSeenHash = lastSeenHash

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

	lastSeenHash, err := storage.GetLastSeenHash(e.db, path)
	if err != nil {
		return err
	}

	resp, err := e.backend.Upload(ctx, &UploadFileMeta{Path: path, Size: size, Hash: hash, LastSeenHash: lastSeenHash}, content)
	if err != nil {
		return err
	}
	if resp.Conflict {
		return fmt.Errorf("conflict: server has %s", resp.CurrentHash)
	}
	return storage.MarkUploaded(e.db, path, hash)
}

func (e *SyncEngine) download(ctx context.Context, root string, action *SyncAction) error {
	full := filepath.Join(root, filepath.FromSlash(action.Path))
	stream, err := e.backend.Download(ctx, action)
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

	if _, err := io.Copy(tmp, stream); err != nil {
		tmp.Close()
		return err
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

func (e *SyncEngine) StartupScan(root string) error {
	tracked, err := storage.ListFiles(e.db)
	if err != nil {
		return fmt.Errorf("list tracked: %w", err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[filepath.ToSlash(f.Path)] = true
	}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isIgnored(rel) {
			return nil
		}

		_, hash, err := storage.HashFileContent(p)
		if err != nil {
			return err
		}

		if !trackedSet[rel] {
			if err := storage.RecordChange(e.db, root, rel, "create"); err != nil {
				return fmt.Errorf("record create %s: %w", rel, err)
			}
		} else {
			for _, f := range tracked {
				if filepath.ToSlash(f.Path) == rel && f.Hash != hash {
					if err := storage.RecordChange(e.db, root, rel, "modify"); err != nil {
						return fmt.Errorf("record modify %s: %w", rel, err)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk root: %w", err)
	}

	onDisk := make(map[string]bool)
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		onDisk[filepath.ToSlash(rel)] = true
		return nil
	})
	for _, f := range tracked {
		p := filepath.ToSlash(f.Path)
		if !onDisk[p] {
			if err := storage.RecordChange(e.db, root, p, "delete"); err != nil {
				return fmt.Errorf("record delete %s: %w", p, err)
			}
		}
	}

	return nil
}

func (e *SyncEngine) deleteRemote(ctx context.Context, path string) error {
	lastSeenHash, err := storage.GetLastSeenHash(e.db, path)
	if err != nil {
		return err
	}
	return e.backend.Delete(ctx, path, lastSeenHash)
}
