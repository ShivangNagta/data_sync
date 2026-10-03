package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/tursodatabase/go-libsql"
	"google.golang.org/grpc"

	srv "github.com/shivangnagta/data_sync/internal/server"
	"github.com/shivangnagta/data_sync/proto/sync"
)

func main() {
	_ = godotenv.Load()

	clearDB := flag.Bool("clear-db", false, "delete all rows from the metadata DB then exit")
	clearR2 := flag.Bool("clear-r2", false, "delete all objects from the R2 bucket then exit")
	reset := flag.Bool("reset", false, "clear both the metadata DB and the R2 bucket then exit (db first)")
	flag.Parse()

	addr := getenv("SYNC_LISTEN", ":54321")

	libsqlURL := os.Getenv("TURSO_URL")
	if libsqlURL == "" {
		log.Fatal("TURSO_URL is required")
	}
	db, err := sql.Open("libsql", libsqlURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	r2, err := srv.NewR2Client(
		os.Getenv("R2_ENDPOINT"),
		os.Getenv("R2_ACCESS_KEY"),
		os.Getenv("R2_SECRET_KEY"),
		os.Getenv("R2_BUCKET"),
	)
	if err != nil {
		log.Fatalf("r2 client: %v", err)
	}

	if *reset {
		*clearDB, *clearR2 = true, true
	}
	if *clearDB {
		log.Print("clearing metadata DB...")
		if err := srv.ClearDatabase(db); err != nil {
			log.Fatalf("clear db: %v", err)
		}
		log.Print("metadata DB cleared")
	}
	if *clearR2 {
		log.Print("clearing R2 bucket...")
		if err := r2.Clear(context.Background()); err != nil {
			log.Fatalf("clear r2: %v", err)
		}
		log.Print("R2 bucket cleared")
	}
	if *clearDB || *clearR2 {
		return
	}

	files := srv.NewFileRepository(db)
	app := srv.NewSyncService(files, r2)
	auth := srv.NewAuthInterceptor()
	service := srv.NewService(app, auth)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.Unary()),
		grpc.ChainStreamInterceptor(auth.Stream()),
	)
	sync.RegisterSyncServiceServer(gs, service)

	log.Printf("sync server listening on %s", addr)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func migrate(db *sql.DB) error {
	stmts := []string{
		srv.CreateFilesTable,
		srv.CreateFilesPathIndex,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(context.Background(), s); err != nil {
			return err
		}
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
