package storage

var CreateLocalFilesTable = `
	CREATE TABLE local_files (
		id INTEGER PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		size INTEGER,
		mod_time DATETIME,
		state TEXT NOT NULL,
		updated_at DATETIME
	)
`

var AddLocalFileTest = `
	INSERT INTO local_files VALUES (
		1, "./test", 1, 1, '1', 1
	)
`

var CreatePendingOperationsTable = `
	CREATE TABLE pending_operations (
		id INTEGER PRIMARY KEY,
		file_id INTEGER NOT NULL,
		op_type TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME,
		FOREIGN KEY (file_id) REFERENCES local_files(id)
	)
`

var AddPendingOperationsTest = `
	INSERT INTO pending_operations VALUES (
		1, 1, '1', '1', 1
	)
`
