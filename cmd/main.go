package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"github.com/shivangnagta/data_sync/internal/storage"
	
)

func main() {
	dsnURI := "./test.db"
	db, err := sql.Open("sqlite", dsnURI)
	if err != nil {
		println("ERROR: Could not connect to the DB.")
	}
	db.Exec(storage.CreateLocalFilesTable)
	db.Exec(storage.CreatePendingOperationsTable)
	db.Exec(storage.AddLocalFileTest)
	db.Exec(storage.AddPendingOperationsTest)
	defer db.Close()
}
