package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
	"github.com/joho/godotenv"

	"github.com/shivangnagta/data_sync/internal/client"
	"github.com/shivangnagta/data_sync/internal/client/storage"
)

func main() {
	_ = godotenv.Load()

	once := flag.Bool("once", false, "run a single sync cycle then exit (no watcher, no ticker)")
	flag.Parse()

	// Local SQLite database for pending operations tracking.
	db, err := sql.Open("sqlite", dbName())
	if err != nil {
		log.Fatalf("open local db: %v", err)
	}
	defer db.Close()
	// Serialize all access on a single connection. The watcher goroutine and
	// the sync loop both write to the same SQLite file; a pooled (multi-
	// connection) DB surfaces SQLITE_BUSY on concurrent writes, especially on
	// slower mobile storage.
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		log.Fatalf("migrate local db: %v", err)
	}

	// Folder to sync (absolute). Validate it early so a missing/mispasted
	// SYNC_FOLDER doesn't silently sync the current working directory (which
	// could be the whole repo, .git included).
	folder, err := filepath.Abs(getenv("SYNC_FOLDER", "."))
	if err != nil {
		log.Fatalf("resolve folder: %v", err)
	}
	fi, err := os.Stat(folder)
	if err != nil {
		log.Fatalf("sync folder %q is not accessible: %v", folder, err)
	}
	if !fi.IsDir() {
		log.Fatalf("sync folder %q is not a directory", folder)
	}
	log.Printf("syncing folder: %s", folder)

	// Connection + device registration/token persistence.
	sc, err := client.NewSyncClient(client.ClientConfig{
		Addr:      getenv("SYNC_ADDR", "localhost:54321"),
		Name:      getenv("DEVICE_NAME", "default"),
		TokenFile: getenv("SYNC_TOKEN_FILE", "./sync_token.json"),
	})
	if err != nil {
		log.Fatalf("client: %v", err)
	}
	defer sc.Close()
	log.Printf("device registered: %s", sc.DeviceID)

	engine := client.NewSyncEngine(sc, db)

	// Reconcile disk vs DB at startup to catch anything fsnotify missed
	// (offline edits, deletions without events, files created while stopped).
	// This is the rare full-scan safety net; the steady-state path is DB-driven.
	if err := client.Reconcile(db, folder); err != nil {
		log.Printf("reconcile failed: %v", err)
	} else {
		log.Printf("reconcile done")
	}

	ctx := context.Background()

	if *once {
		// One-shot mode: sync once and exit. No watcher, no ticker.
		// Intended for mobile/tablet where background daemons are killed
		// by the OS and fsnotify may not work (FUSE, sandbox restrictions).
		if err := engine.Sync(ctx, folder); err != nil {
			log.Fatalf("sync failed: %v", err)
		}
		return
	}

	// Daemon mode: watch for local changes and sync periodically.
	w, err := client.NewWatcher(db, folder)
	if err != nil {
		log.Fatalf("watcher: %v", err)
	}
	if err := w.Start(); err != nil {
		log.Fatalf("start watcher: %v", err)
	}

	// Initial sync on startup.
	if err := engine.Sync(ctx, folder); err != nil {
		log.Printf("initial sync failed: %v", err)
	}

	// Sync periodically so changes from other devices propagate.
	interval := time.Duration(getDuration(getenv("SYNC_INTERVAL", "30"))) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := engine.Sync(ctx, folder); err != nil {
			log.Printf("sync failed: %v", err)
		}
	}
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(storage.CreateLocalFilesTable); err != nil {
		return err
	}
	_, err := db.Exec(storage.CreatePendingOperationsTable)
	return err
}

func dbName() string {
	if v := os.Getenv("SYNC_DB"); v != "" {
		return v
	}
	return "./test.db"
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getDuration(s string) int {
	n, _ := strconv.ParseInt(s, 10, 64)
	return int(n)
}
