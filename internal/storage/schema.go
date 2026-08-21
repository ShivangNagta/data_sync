// Contains the initial DDL queries

package storage

var CreateLocalFilesTable = `
	CREATE TABLE local_files (
		id INTEGER PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		state TEXT NOT NULL
	)
`

var AddLocalFileTest = `
	INSERT INTO local_files VALUES (
		1, './test', '1'
	)
`

var CreatePendingOperationsTable = `
	CREATE TABLE pending_operations (
		id INTEGER PRIMARY KEY,
		file_id INTEGER NOT NULL,
		op_type TEXT NOT NULL,
		status TEXT NOT NULL,
		FOREIGN KEY (file_id) REFERENCES local_files(id)
	)
`

var AddPendingOperationsTest = `
	INSERT INTO pending_operations VALUES (
		1, 1, '1', '1'
	)
`
