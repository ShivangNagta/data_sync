// This is the repository, just handles the direct database queries
// db pointer -> sql.DB is passed explicitly to allow dependency injection
// the actual db connection can be handled by the caller

package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Path represent the path to a file
// OpType represents one of the operation type which could be
// create, modify or delete
// Status represents the current status of the operation
// it could be pending or completed
type PendingOp struct {
	ID     int
	Path   string
	OpType string
	Status string
}

// hashedFile is a file path plus its computed size and content hash.
type hashedFile struct {
	Path string
	Size int64
	Hash string
}

// HashFileContent computes the SHA-256 hash and size of a file.
func HashFileContent(path string) (int64, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	sum := sha256.Sum256(data)
	return int64(len(data)), hex.EncodeToString(sum[:]), nil
}

// RecordChange updates the file information when a file is created, modified,
// or deleted. relPath is the path relative to the sync root (slash-normalized,
// what we store in local_files / report to the server); root is the sync
// folder root used to locate the file on disk for hashing.
func RecordChange(db *sql.DB, root, relPath string, opType string) error {
	// A delete removes content on disk, so we cannot hash it; clear hash/size.
	if opType == "delete" {
		return recordDelete(db, relPath)
	}

	size, hash, err := HashFileContent(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return fmt.Errorf("hash file: %w", err)
	}

	err = upsertFile(db, relPath, "pending", size, hash)
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"INSERT INTO pending_operations (file_id, op_type, status) SELECT id, ?, 'pending' FROM local_files WHERE path = ?",
		opType,
		relPath,
	)
	if err != nil {
		return fmt.Errorf("insert pending_operations: %w", err)
	}
	return nil
}

// recordDelete marks a file as pending-delete and clears its stored metadata.
func recordDelete(db *sql.DB, path string) error {
	err := upsertFile(db, path, "pending", 0, "")
	if err != nil {
		return err
	}
	_, err = db.Exec(
		"INSERT INTO pending_operations (file_id, op_type, status) SELECT id, 'delete', 'pending' FROM local_files WHERE path = ?",
		path,
	)
	if err != nil {
		return fmt.Errorf("insert pending delete: %w", err)
	}
	return nil
}

// upsertFile inserts a local_files row if absent, otherwise updates its
// metadata and state.
func upsertFile(db *sql.DB, path, state string, size int64, hash string) error {
	_, err := db.Exec(`
		INSERT INTO local_files (path, state, size, hash) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET state = ?, size = ?, hash = ?
	`, path, state, size, hash, state, size, hash)
	if err != nil {
		return fmt.Errorf("upsert local_files: %w", err)
	}
	return nil
}

// Untrack removes a file from sync entirely: drops its local_files row and any
// pending operations, leaving the file itself untouched on disk. Used when a
// file becomes excluded (e.g. .DS_Store or a stray temp row) after having been
// tracked.
func Untrack(db *sql.DB, path string) error {
	_, err := db.Exec(
		"DELETE FROM pending_operations WHERE file_id IN (SELECT id FROM local_files WHERE path = ?)",
		path,
	)
	if err != nil {
		return fmt.Errorf("delete pending ops: %w", err)
	}
	_, err = db.Exec("DELETE FROM local_files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("delete local_files: %w", err)
	}
	return nil
}

// MarkDownloaded records a file that was just pulled from the server into the
// local DB using the size/hash the server reported, so the next manifest
// includes it as already-synced. This avoids re-downloading it in the same
// session and stops it from being re-uploaded as if it were a new local file.
// A freshly downloaded file has no pending operation: it's already in sync.
func MarkDownloaded(db *sql.DB, path string, size int64, hash string) error {
	return upsertFile(db, path, "synced", size, hash)
}

// Returns all the pending operations that have not been synced yet
// A slice of a custom type PendingOp is used as the return type
func GetPendingOps(db *sql.DB) ([]PendingOp, error) {
	rows, err := db.Query(
		"SELECT po.id, lf.path, po.op_type, po.status FROM pending_operations po JOIN local_files lf ON po.file_id = lf.id WHERE po.status = 'pending'",
	)
	if err != nil {
		return nil, fmt.Errorf("query pending ops: %w", err)
	}
	defer rows.Close()

	var ops []PendingOp
	for rows.Next() {
		var op PendingOp
		if err := rows.Scan(&op.ID, &op.Path, &op.OpType, &op.Status); err != nil {
			return nil, fmt.Errorf("scan pending op: %w", err)
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// Marks a pending operation as completed
func MarkOpCompleted(db *sql.DB, opID int) error {
	_, err := db.Exec(
		"UPDATE pending_operations SET status = 'completed' WHERE id = ?",
		opID,
	)
	if err != nil {
		return fmt.Errorf("mark op completed: %w", err)
	}
	return nil
}

// PendingPaths returns the set of file paths that currently have pending
// (not-yet-synced) operations. Used to protect un-synced local edits.
func PendingPaths(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(
		"SELECT DISTINCT lf.path FROM pending_operations po JOIN local_files lf ON po.file_id = lf.id WHERE po.status = 'pending'",
	)
	if err != nil {
		return nil, fmt.Errorf("query pending paths: %w", err)
	}
	defer rows.Close()

	paths := make(map[string]bool)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan pending path: %w", err)
		}
		paths[p] = true
	}
	return paths, rows.Err()
}

// PendingOpType returns the op type ("create", "modify", "delete") of the most
// recent pending operation for a path, or "" if none exists.
func PendingOpType(db *sql.DB, path string) (string, error) {
	var op string
	err := db.QueryRow(`
		SELECT po.op_type FROM pending_operations po
		JOIN local_files lf ON po.file_id = lf.id
		WHERE lf.path = ? AND po.status = 'pending'
		ORDER BY po.id DESC LIMIT 1
	`, path).Scan(&op)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query pending op type: %w", err)
	}
	return op, nil
}

// TrackedFile is a locally tracked file with its known metadata.
type TrackedFile struct {
	Path string
	Size int64
	Hash string
}

// ListFiles returns all tracked local files (path, size, hash) from the DB.
// This is the DB-driven basis for the client manifest.
func ListFiles(db *sql.DB) ([]TrackedFile, error) {
	rows, err := db.Query("SELECT path, size, hash FROM local_files")
	if err != nil {
		return nil, fmt.Errorf("query local_files: %w", err)
	}
	defer rows.Close()

	var files []TrackedFile
	for rows.Next() {
		var f TrackedFile
		if err := rows.Scan(&f.Path, &f.Size, &f.Hash); err != nil {
			return nil, fmt.Errorf("scan local file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetTracked returns a single tracked file's metadata, or ok=false if absent.
func GetTracked(db *sql.DB, path string) (TrackedFile, bool, error) {
	var f TrackedFile
	err := db.QueryRow("SELECT path, size, hash FROM local_files WHERE path = ?", path).Scan(&f.Path, &f.Size, &f.Hash)
	if err == sql.ErrNoRows {
		return TrackedFile{}, false, nil
	}
	if err != nil {
		return TrackedFile{}, false, fmt.Errorf("get tracked: %w", err)
	}
	return f, true, nil
}

// MarkAllCompleted marks every pending operation as completed.
func MarkAllCompleted(db *sql.DB) error {
	_, err := db.Exec("UPDATE pending_operations SET status = 'completed' WHERE status = 'pending'")
	if err != nil {
		return fmt.Errorf("mark all completed: %w", err)
	}
	return nil
}
