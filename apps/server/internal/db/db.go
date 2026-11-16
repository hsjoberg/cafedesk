// Package db opens the server's PostgreSQL database and keeps its schema
// up to date.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to the database at url.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New(`DATABASE_URL is not set. For the local Postgres, in PowerShell:
  $env:DATABASE_URL = 'postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable'
or in bash:
  export DATABASE_URL='postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable'`)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("can't reach the database (is Postgres running? docker compose up -d postgres): %w", err)
	}
	return pool, nil
}

// migrationLock is the advisory lock taken while migrating, so two servers
// starting at once don't both migrate.
const migrationLock = 7_204_118_301

// Migrate applies the migrations not yet applied, each in a transaction,
// in the order of their numbers. It returns how many it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return 0, err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLock)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return 0, err
	}
	applied := map[int]bool{}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return 0, err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return 0, err
	}
	sort.Strings(files)
	n := 0
	for _, f := range files {
		name := strings.TrimPrefix(f, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return n, fmt.Errorf("migration %s: the name must start with its number", name)
		}
		if applied[version] {
			continue
		}
		sql, err := migrations.ReadFile(f)
		if err != nil {
			return n, err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return n, err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return n, fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", version, name); err != nil {
			tx.Rollback(ctx)
			return n, err
		}
		if err := tx.Commit(ctx); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
