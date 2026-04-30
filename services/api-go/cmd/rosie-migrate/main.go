package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var databaseURL string
	var migrationsDir string
	var dryRun bool
	flag.StringVar(&databaseURL, "database-url", os.Getenv("ROSIE_DATABASE_URL"), "PostgreSQL connection string")
	flag.StringVar(&migrationsDir, "migrations-dir", "migrations", "directory containing .sql migration files")
	flag.BoolVar(&dryRun, "dry-run", false, "print migration files without applying them")
	flag.Parse()

	if strings.TrimSpace(databaseURL) == "" {
		log.Fatal("database-url is required")
	}

	paths, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		log.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		log.Fatalf("no migration files found in %s", migrationsDir)
	}

	if dryRun {
		for _, path := range paths {
			log.Printf("would apply %s", path)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatal(err)
	}

	for _, path := range paths {
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("apply %s: %v", path, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("commit %s: %v", path, err)
		}
		log.Printf("applied %s", path)
	}
}
