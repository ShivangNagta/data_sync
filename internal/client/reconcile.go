// This file implements reconciliation: a rare, full disk walk that compares
// the filesystem against the local DB to detect changes that fsnotify missed
// (crashes, killed process, offline editing, deletions without events).
// It runs at startup (and optionally on a slow interval) -- the common sync
// path stays DB-driven and cheap.

package client

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/shivangnagta/data_sync/internal/client/storage"
)

// Reconcile walks the sync folder and updates the local DB so that the
// DB-driven manifest reflects reality: it records new files, re-hashes changed
// files, and records deletions for files missing from disk.
func Reconcile(db *sql.DB, root string) error {
	tracked, err := storage.ListFiles(db)
	if err != nil {
		return fmt.Errorf("list tracked: %w", err)
	}
	onDisk := make(map[string]bool)
	diskHashes := make(map[string]string)

	// Walk the folder, finding every regular file.
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
		onDisk[rel] = true

		_, hash, err := storage.HashFileContent(p)
		if err != nil {
			return err
		}
		diskHashes[rel] = hash
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk root: %w", err)
	}

	// For each tracked file, compare DB state vs disk.
	for _, f := range tracked {
		p := filepath.ToSlash(f.Path)

		if !onDisk[p] {
			// Tracked but gone from disk -> deletion (probably missed event).
			if err := storage.RecordChange(db, root, p, "delete"); err != nil {
				return fmt.Errorf("record delete %s: %w", p, err)
			}
			continue
		}
		if diskHashes[p] != f.Hash {
			// Content changed but no event recorded -> re-record as modify.
			if err := storage.RecordChange(db, root, p, "modify"); err != nil {
				return fmt.Errorf("record modify %s: %w", p, err)
			}
		}
	}

	// New files on disk not tracked in DB -> create.
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[filepath.ToSlash(f.Path)] = true
	}
	for p := range onDisk {
		if !trackedSet[p] {
			if err := storage.RecordChange(db, root, p, "create"); err != nil {
				return fmt.Errorf("record create %s: %w", p, err)
			}
		}
	}

	return nil
}
