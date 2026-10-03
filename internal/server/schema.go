package server

import (
	"database/sql"
	"fmt"
)

const CreateFilesTable = `
CREATE TABLE IF NOT EXISTS files (
  path       TEXT PRIMARY KEY,
  hash       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  deleted_at DATETIME
)
`

const CreateFilesPathIndex = `
CREATE INDEX IF NOT EXISTS idx_files_path ON files (path)
`

func ClearDatabase(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM files")
	if err != nil {
		return fmt.Errorf("clear db: %w", err)
	}
	return nil
}
