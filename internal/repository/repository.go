// Package repository encapsulates raw SQL access. Each entity has its own file;
// repository.go owns the shared Repo struct and common types.
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is the single entry point for all DB access. Methods are grouped by
// entity in sibling files (users.go, posts.go, etc.) for navigation.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Pool exposes the underlying pgxpool for advanced callers (transactions, custom
// queries). Prefer adding a method on Repo when possible.
func (r *Repo) Pool() *pgxpool.Pool {
	return r.pool
}

// ErrNotFound is returned when a query expects exactly one row but finds none.
var ErrNotFound = errors.New("repository: not found")

// ErrConflict is returned when a mutation violates a DB-level constraint.
var ErrConflict = errors.New("repository: conflict")

// isNoRows centralises the pgx error mapping so callers compare against ErrNotFound only.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
