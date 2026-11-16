// Command migrate applies the embedded SQL migrations in lexical order.
//
// It is safe to run on every deploy: applied versions are recorded in
// schema_migrations and skipped. Each migration runs in its own transaction,
// so a failure leaves the schema at the last fully applied version.
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/betva/backend/internal/config"
	"github.com/betva/backend/internal/db"
	"github.com/betva/backend/migrations"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("migrate: db connect: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		log.Fatalf("migrate: create schema_migrations: %v", err)
	}

	applied := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		log.Fatalf("migrate: read schema_migrations: %v", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			log.Fatalf("migrate: scan version: %v", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatalf("migrate: read schema_migrations: %v", err)
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		log.Fatalf("migrate: list embedded migrations: %v", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	ran := 0
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		sqlBytes, err := migrations.FS.ReadFile(name)
		if err != nil {
			log.Fatalf("migrate: read %s: %v", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatalf("migrate: begin %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("migrate: apply %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("migrate: record %s: %v", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("migrate: commit %s: %v", name, err)
		}
		log.Printf("migrate: applied %s", name)
		ran++
	}

	if ran == 0 {
		fmt.Println("migrate: schema up to date")
	} else {
		fmt.Printf("migrate: %d migration(s) applied\n", ran)
	}
}
