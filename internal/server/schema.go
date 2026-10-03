package server

import (
	"database/sql"
	"fmt"
)

const CreateDevicesTable = `
CREATE TABLE IF NOT EXISTS devices (
  device_id TEXT PRIMARY KEY,
  name      TEXT NOT NULL,
  token     TEXT NOT NULL,
  last_seen DATETIME
)
`

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
	var err error
	for _, stmt := range []string{
		"DELETE FROM files",
	} {
		if _, err = db.Exec(stmt); err != nil {
			return fmt.Errorf("clear db (%s): %w", stmt, err)
		}
	}
	return nil
}
