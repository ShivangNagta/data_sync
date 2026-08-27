// This file contains the initial DDL queries

package storage

var CreateLocalFilesTable = `
	CREATE TABLE IF NOT EXISTS local_files (
		id INTEGER PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		state TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		hash TEXT NOT NULL DEFAULT ''
	)
`

var CreatePendingOperationsTable = `
	CREATE TABLE IF NOT EXISTS pending_operations (
		id INTEGER PRIMARY KEY,
		file_id INTEGER NOT NULL,
		op_type TEXT NOT NULL,
		status TEXT NOT NULL,
		FOREIGN KEY (file_id) REFERENCES local_files(id)
	)
`
