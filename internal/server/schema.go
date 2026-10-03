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

func ResetDatabase(db *sql.DB) error {
	for _, table := range []string{"conflicts", "file_versions", "files", "devices"} {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return fmt.Errorf("drop %s table: %w", table, err)
		}
	}
	if _, err := db.Exec(CreateFilesTable); err != nil {
		return fmt.Errorf("recreate files table: %w", err)
	}
	if _, err := db.Exec(CreateFilesPathIndex); err != nil {
		return fmt.Errorf("recreate files path index: %w", err)
	}
	return nil
}
