package server

import (
	"database/sql"
	"fmt"
)

// Server-side (Turso) schema.
// Metadata only - file bytes live in Cloudflare R2.

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
	file_id         TEXT PRIMARY KEY,
	path            TEXT NOT NULL,
	current_version TEXT
)
`

// Lookups by path happen on every sync-plan poll; index so lookups are
// point-reads instead of full table scans (keeps Turso rows-read low).
const CreateFilesPathIndex = `
CREATE INDEX IF NOT EXISTS idx_files_path ON files (path)
`

const CreateFileVersionsTable = `
CREATE TABLE IF NOT EXISTS file_versions (
	version_id      TEXT PRIMARY KEY,
	file_id         TEXT NOT NULL,
	device_id       TEXT NOT NULL,
	base_version_id TEXT,
	size            INTEGER NOT NULL,
	root_hash       TEXT NOT NULL,
	created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (file_id) REFERENCES files(file_id),
	FOREIGN KEY (device_id) REFERENCES devices(device_id)
)
`

// Head-version lookups filter by file_id on every poll; index to avoid scans.
const CreateFileVersionsFileIndex = `
CREATE INDEX IF NOT EXISTS idx_fv_file_id ON file_versions (file_id)
`

const CreateConflictsTable = `
CREATE TABLE IF NOT EXISTS conflicts (
	conflict_id TEXT PRIMARY KEY,
	file_id     TEXT NOT NULL,
	version_a   TEXT NOT NULL,
	version_b   TEXT NOT NULL,
	status      TEXT NOT NULL,
	FOREIGN KEY (file_id) REFERENCES files(file_id)
)
`

// ClearDatabase removes all rows from every metadata table. Order matters for
// foreign keys: children (file_versions, conflicts) must be cleared before
// parents (files, devices). Indexes and schema are left intact so migrate()
// still succeeds after a reset.
func ClearDatabase(db *sql.DB) error {
	var err error
	for _, stmt := range []string{
		"DELETE FROM file_versions",
		"DELETE FROM conflicts",
		"DELETE FROM files",
		"DELETE FROM devices",
	} {
		if _, err = db.Exec(stmt); err != nil {
			return fmt.Errorf("clear db (%s): %w", stmt, err)
		}
	}
	return nil
}
