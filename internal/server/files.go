package server

import (
	"context"
	"database/sql"
	"fmt"
)

type File struct {
	Path      string
	Hash      string
	Size      int64
	DeletedAt string
}

func (f File) IsDeleted() bool { return f.DeletedAt != "" }

type FileRepository struct {
	db *sql.DB
}

func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

func (r *FileRepository) GetFileByPath(ctx context.Context, path string) (File, bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT path, hash, size, COALESCE(deleted_at, '')
		 FROM files WHERE path = ?`,
		path,
	)

	var f File
	if err := row.Scan(&f.Path, &f.Hash, &f.Size, &f.DeletedAt); err != nil {
		if err == sql.ErrNoRows {
			return File{}, false, nil
		}
		return File{}, false, fmt.Errorf("get file by path: %w", err)
	}
	return f, true, nil
}

func (r *FileRepository) ListAllFiles(ctx context.Context) ([]File, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT path, hash, size FROM files WHERE deleted_at IS NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Path, &f.Hash, &f.Size); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *FileRepository) UpsertFile(ctx context.Context, path, hash string, size int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO files (path, hash, size) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET hash = ?, size = ?, deleted_at = NULL`,
		path, hash, size, hash, size,
	)
	if err != nil {
		return fmt.Errorf("upsert file: %w", err)
	}
	return nil
}

func (r *FileRepository) UpdateFileIfHash(ctx context.Context, path, oldHash, hash string, size int64) (bool, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE files SET hash = ?, size = ?, deleted_at = NULL
		 WHERE path = ? AND hash = ? AND deleted_at IS NULL`,
		hash, size, path, oldHash,
	)
	if err != nil {
		return false, fmt.Errorf("compare-and-swap file: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("compare-and-swap rows affected: %w", err)
	}
	return rows == 1, nil
}

func (r *FileRepository) MarkDeleted(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE files SET deleted_at = CURRENT_TIMESTAMP WHERE path = ?",
		path,
	)
	if err != nil {
		return fmt.Errorf("mark deleted: %w", err)
	}
	return nil
}
