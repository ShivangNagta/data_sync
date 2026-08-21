package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
	"github.com/shivangnagta/data_sync/internal/storage"
)

func main() {
	db, err := sql.Open("sqlite", "./test.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create tables
	_, err = db.Exec(storage.CreateLocalFilesTable)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(storage.CreatePendingOperationsTable)
	if err != nil {
		log.Fatal(err)
	}

	// Test RecordChange
	err = storage.RecordChange(db, "./test.txt", "create")
	if err != nil {
		log.Fatal(err)
	}
	err = storage.RecordChange(db, "./test.txt", "modify")
	if err != nil {
		log.Fatal(err)
	}

	// Test GetPendingOps
	ops, err := storage.GetPendingOps(db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Pending ops: %d\n", len(ops))
	for _, op := range ops {
		fmt.Printf("  [%d] %s %s\n", op.ID, op.OpType, op.Path)
	}

	// Test MarkOpCompleted
	err = storage.MarkOpCompleted(db, 1)
	if err != nil {
		log.Fatal(err)
	}

	// Verify
	ops, err = storage.GetPendingOps(db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Pending ops after completion: %d\n", len(ops))
}
