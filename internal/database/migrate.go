package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationState reports whether a migration has been applied.
type MigrationState struct {
	Version string
	Applied bool
}

// EnsureMigrationTable creates the schema_migrations bookkeeping table if missing.
func EnsureMigrationTable(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

// ApplyMigrations runs every *.sql file in dir, in lexical order, that hasn't been
// recorded in schema_migrations. Each file is executed inside its own transaction.
func ApplyMigrations(ctx context.Context, db *pgxpool.Pool, dir string) error {
	if err := EnsureMigrationTable(ctx, db); err != nil {
		return err
	}
	files, err := migrationFiles(dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		version := versionFromFilename(file)
		applied, err := isApplied(ctx, db, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyOne(ctx, db, file, version); err != nil {
			return err
		}
	}
	return nil
}

// Status returns the applied flag for every migration file on disk.
func Status(ctx context.Context, db *pgxpool.Pool, dir string) ([]MigrationState, error) {
	if err := EnsureMigrationTable(ctx, db); err != nil {
		return nil, err
	}
	files, err := migrationFiles(dir)
	if err != nil {
		return nil, err
	}

	applied, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	states := make([]MigrationState, 0, len(files))
	for _, file := range files {
		v := versionFromFilename(file)
		states = append(states, MigrationState{Version: v, Applied: applied[v]})
	}
	return states, nil
}

func applyOne(ctx context.Context, db *pgxpool.Pool, file, version string) error {
	sqlBytes, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", file, err)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", file, err)
	}
	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("apply %s: %w", file, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("record %s: %w", file, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", file, err)
	}
	return nil
}

func loadAppliedVersions(ctx context.Context, db *pgxpool.Pool) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

func migrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migration dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func versionFromFilename(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".sql")
}

func isApplied(ctx context.Context, db *pgxpool.Pool, version string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return exists, nil
}

// ErrNoMigrations is returned when the migrations directory has no .sql files.
var ErrNoMigrations = errors.New("no migration files found")
