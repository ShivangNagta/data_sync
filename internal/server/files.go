package server

import (
	"context"
	"database/sql"
	"fmt"
)

type File struct {
	FileID         string
	Path           string
	CurrentVersion string
}

// FileRepository provides data access to the files table.
type FileRepository struct {
	db *sql.DB
}

func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

// CreateFile inserts a new logical file.
func (r *FileRepository) CreateFile(ctx context.Context, fileID, path string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO files (file_id, path) VALUES (?, ?)",
		fileID, path,
	)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	return nil
}

// GetFileByPath looks up a file by its path.
// Returns (false, nil) if no file matches.
func (r *FileRepository) GetFileByPath(ctx context.Context, path string) (File, bool, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT file_id, path, COALESCE(current_version, '') FROM files WHERE path = ?",
		path,
	)

	var f File
	if err := row.Scan(&f.FileID, &f.Path, &f.CurrentVersion); err != nil {
		if err == sql.ErrNoRows {
			return File{}, false, nil
		}
		return File{}, false, fmt.Errorf("get file by path: %w", err)
	}
	return f, true, nil
}

// ListAllFiles returns every logical file on the server.
// Used by ComputeSyncPlan to find files a client doesn't have yet.
func (r *FileRepository) ListAllFiles(ctx context.Context) ([]File, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT file_id, path, COALESCE(current_version, '') FROM files",
	)
	if err != nil {
		return nil, fmt.Errorf("list all files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.FileID, &f.Path, &f.CurrentVersion); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// SetCurrentVersion advances the head of a file.
func (r *FileRepository) SetCurrentVersion(ctx context.Context, fileID, versionID string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE files SET current_version = ? WHERE file_id = ?",
		versionID, fileID,
	)
	if err != nil {
		return fmt.Errorf("set current version: %w", err)
	}
	return nil
}
