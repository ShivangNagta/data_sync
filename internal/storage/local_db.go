// This is the repository, just handles the direct database queries
// db pointer -> sql.DB is passed explicitly to allow dependency injection
// the actual db connection can be handled by the caller

package storage

import (
	"database/sql"
	"fmt"
)

// Path represent the path to a file
// OpType represents one of the operation type which could be
// create, modify or delete
// Status represents the current status of the operation 
// it could be pending or completed
type PendingOp struct {
	ID     int
	Path   string
	OpType string
	Status string
}


// RecordChange updates the file information when a new file gets added
// or an existing file gets updated or deleted. 
func RecordChange(db *sql.DB, path string, opType string) error {
	// Check if file exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM local_files WHERE path = ?)", path).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check local_files: %w", err)
	}

	// Insert if not exists, or update state to pending if exists
	if !exists {
		_, err = db.Exec("INSERT INTO local_files (path, state) VALUES (?, 'pending')", path)
		if err != nil {
			return fmt.Errorf("insert local_files: %w", err)
		}
	} else {
		_, err = db.Exec("UPDATE local_files SET state = 'pending' WHERE path = ?", path)
		if err != nil {
			return fmt.Errorf("update local_files: %w", err)
		}
	}

	// Insert pending operation
	_, err = db.Exec(
		"INSERT INTO pending_operations (file_id, op_type, status) SELECT id, ?, 'pending' FROM local_files WHERE path = ?",
		opType,
		path,
	)
	if err != nil {
		return fmt.Errorf("insert pending_operations: %w", err)
	}

	return nil
}

// Returns all the pending operations that have not been synced yet
// A slice of a custom type PendingOp is used as the return type
func GetPendingOps(db *sql.DB) ([]PendingOp, error) {
	rows, err := db.Query(
		"SELECT po.id, lf.path, po.op_type, po.status FROM pending_operations po JOIN local_files lf ON po.file_id = lf.id WHERE po.status = 'pending'",
	)
	if err != nil {
		return nil, fmt.Errorf("query pending ops: %w", err)
	}
	defer rows.Close()

	var ops []PendingOp
	for rows.Next() {
		var op PendingOp
		if err := rows.Scan(&op.ID, &op.Path, &op.OpType, &op.Status); err != nil {
			return nil, fmt.Errorf("scan pending op: %w", err)
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// Marks a pending operation as completed
func MarkOpCompleted(db *sql.DB, opID int) error {
	_, err := db.Exec(
		"UPDATE pending_operations SET status = 'completed' WHERE id = ?",
		opID,
	)
	if err != nil {
		return fmt.Errorf("mark op completed: %w", err)
	}
	return nil
}
