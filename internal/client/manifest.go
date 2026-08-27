// This file builds the client manifest. The manifest is DB-driven: it starts
// from the files tracked in local_files (recorded by the watcher/reconciler)
// and re-hashes only files that have pending operations (which changed since
// they were last recorded). It does not re-scan the entire disk every poll.

package client

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/shivangnagta/data_sync/internal/client/storage"
	"github.com/shivangnagta/data_sync/proto/sync"
)

// buildManifest returns the set of local files (relative slash paths + size +
// content hash) reported to the server, plus the list of pending paths (files
// with un-synced local changes). It is derived from the local DB, not a full
// disk scan.
func buildManifest(db *sql.DB, root string) (map[string]*sync.FileState, []string, error) {
	tracked, err := storage.ListFiles(db)
	if err != nil {
		return nil, nil, fmt.Errorf("list tracked files: %w", err)
	}

	manifest := make(map[string]*sync.FileState, len(tracked))
	var pending []string

	for _, f := range tracked {
		p := filepath.ToSlash(f.Path)
		fs := &sync.FileState{Path: p, Size: f.Size, Hash: f.Hash}

		pendingOp, err := storage.PendingOpType(db, p)
		if err != nil {
			return nil, nil, err
		}
		if pendingOp != "" {
			pending = append(pending, p)
			// A pending delete means the file is gone locally; nothing to
			// upload, so exclude it from the manifest.
			if pendingOp == "delete" {
				continue
			}
			// The file changed since we recorded its hash -> re-hash it now.
			full := filepath.Join(root, filepath.FromSlash(p))
			size, hash, err := storage.HashFileContent(full)
			if err != nil {
				return nil, nil, err
			}
			fs.Size = size
			fs.Hash = hash
		}

		manifest[p] = fs
	}

	return manifest, pending, nil
}
