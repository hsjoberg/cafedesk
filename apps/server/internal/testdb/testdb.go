// Package testdb gives each test a database of its own on the PostgreSQL
// of TEST_DATABASE_URL, migrated, and dropped when the test ends.
//
//	docker compose up -d postgres
//	TEST_DATABASE_URL=postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable go test ./...
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"cafedesk/apps/server/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New returns a pool on a new, migrated database, or skips the test when
// TEST_DATABASE_URL isn't set.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := Empty(t)
	if _, err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// Empty returns a pool on a new database with nothing in it, not even the
// schema.
func Empty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run the tests that need PostgreSQL")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	b := make([]byte, 6)
	rand.Read(b)
	name := "cafedesk_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close(context.Background())
	})
	return pool
}
