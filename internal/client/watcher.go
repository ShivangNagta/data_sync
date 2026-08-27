// This file contains the main watcher component
// It uses fsnotify to listen to changes in the filesystem
// using internal kernel level apis like kqueue/ inotify
// It runs a goroutine for the event based notification

package client

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/shivangnagta/data_sync/internal/client/storage"
)

// db : the database connection object
// folder : the folder that needs to be watched
// fswatch : watcher object from the fsnotify library
type Watcher struct {
	db      *sql.DB
	folder  string
	fswatch *fsnotify.Watcher
}

// creates a new Watcher object
func NewWatcher(db *sql.DB, folder string) (*Watcher, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return nil, fmt.Errorf("abs folder: %w", err)
	}

	fswatch, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	return &Watcher{
		db:      db,
		folder:  abs,
		fswatch: fswatch,
	}, nil
}

// Starts the fsnotify event watching for the folder
// TODO: Add recursive watching
func (w *Watcher) Start() error {
	err := w.fswatch.Add(w.folder)
	if err != nil {
		return fmt.Errorf("add folder to watch: %w", err)
	}

	go w.watchEvents()

	return nil
}

// event handler for the fsnotify events
// adds the changes in the database
func (w *Watcher) watchEvents() {
	for event := range w.fswatch.Events {
		opType := mapEventToOp(event.Op)
		if opType == "" {
			continue
		}

		rel, err := filepath.Rel(w.folder, event.Name)
		if err != nil {
			fmt.Printf("Error computing relative path: %v\n", err)
			continue
		}
		rel = filepath.ToSlash(rel)

		err = storage.RecordChange(w.db, w.folder, rel, opType)
		if err != nil {
			fmt.Printf("Error recording change: %v\n", err)
		}
	}
}

// Renaming treated as a delete for now
// TODO: Look into it to avoid inconsistencies
func mapEventToOp(op fsnotify.Op) string {
	switch {
	case op&fsnotify.Create == fsnotify.Create:
		return "create"
	case op&fsnotify.Write == fsnotify.Write:
		return "modify"
	case op&fsnotify.Remove == fsnotify.Remove:
		return "delete"
	case op&fsnotify.Rename == fsnotify.Rename:
		return "delete"
	default:
		return ""
	}
}
