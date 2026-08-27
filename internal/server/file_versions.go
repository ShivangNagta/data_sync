package server

import (
	"context"
	"database/sql"
	"fmt"
)

type FileVersion struct {
	VersionID     string
	FileID        string
	DeviceID      string
	BaseVersionID string
	Size          int64
	RootHash      string
}

// VersionRepository provides data access to the file_versions table.
type VersionRepository struct {
	db *sql.DB
}

func NewVersionRepository(db *sql.DB) *VersionRepository {
	return &VersionRepository{db: db}
}

// InsertVersion records an immutable file version.
// BaseVersionID is stored as-is (empty string means "no base").
func (r *VersionRepository) InsertVersion(ctx context.Context, v FileVersion) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO file_versions (version_id, file_id, device_id, base_version_id, size, root_hash)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		v.VersionID, v.FileID, v.DeviceID, v.BaseVersionID, v.Size, v.RootHash,
	)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}
	return nil
}

// GetHeadVersion returns the current head version of a file.
// Returns (false, nil) if the file has no versions yet.
func (r *VersionRepository) GetHeadVersion(ctx context.Context, fileID string) (FileVersion, bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT version_id, file_id, device_id, COALESCE(base_version_id, ''), size, root_hash
		 FROM file_versions WHERE file_id = ?
		 ORDER BY created_at DESC LIMIT 1`,
		fileID,
	)

	var v FileVersion
	if err := row.Scan(&v.VersionID, &v.FileID, &v.DeviceID, &v.BaseVersionID, &v.Size, &v.RootHash); err != nil {
		if err == sql.ErrNoRows {
			return FileVersion{}, false, nil
		}
		return FileVersion{}, false, fmt.Errorf("get head version: %w", err)
	}
	return v, true, nil
}
