package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Platform struct {
	Key       string
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreatePlatformParams struct {
	Key       string
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
}

type UpdatePlatformParams struct {
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
}

func (r *Repo) ListPlatforms(ctx context.Context, includeInactive bool) ([]Platform, error) {
	query := `
		SELECT key, label, icon, tone, sort_order, is_active, created_at, updated_at
		FROM platforms
	`
	if !includeInactive {
		query += ` WHERE is_active = TRUE`
	}
	query += ` ORDER BY sort_order ASC, label ASC, key ASC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list platforms: %w", err)
	}
	defer rows.Close()

	out := make([]Platform, 0)
	for rows.Next() {
		var p Platform
		if err := rows.Scan(
			&p.Key,
			&p.Label,
			&p.Icon,
			&p.Tone,
			&p.SortOrder,
			&p.IsActive,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan platform: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) GetPlatform(ctx context.Context, key string) (Platform, error) {
	var p Platform
	err := r.pool.QueryRow(ctx, `
		SELECT key, label, icon, tone, sort_order, is_active, created_at, updated_at
		FROM platforms
		WHERE key = $1
	`, key).Scan(
		&p.Key,
		&p.Label,
		&p.Icon,
		&p.Tone,
		&p.SortOrder,
		&p.IsActive,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if isNoRows(err) {
		return Platform{}, ErrNotFound
	}
	if err != nil {
		return Platform{}, fmt.Errorf("get platform: %w", err)
	}
	return p, nil
}

func (r *Repo) CreatePlatform(ctx context.Context, params CreatePlatformParams) (Platform, error) {
	var p Platform
	err := r.pool.QueryRow(ctx, `
		INSERT INTO platforms (key, label, icon, tone, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING key, label, icon, tone, sort_order, is_active, created_at, updated_at
	`, params.Key, params.Label, params.Icon, params.Tone, params.SortOrder, params.IsActive).Scan(
		&p.Key,
		&p.Label,
		&p.Icon,
		&p.Tone,
		&p.SortOrder,
		&p.IsActive,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return Platform{}, fmt.Errorf("create platform: %w", err)
	}
	return p, nil
}

func (r *Repo) UpdatePlatform(ctx context.Context, key string, params UpdatePlatformParams) (Platform, error) {
	var p Platform
	err := r.pool.QueryRow(ctx, `
		UPDATE platforms
		SET label = $2,
		    icon = $3,
		    tone = $4,
		    sort_order = $5,
		    is_active = $6,
		    updated_at = now()
		WHERE key = $1
		RETURNING key, label, icon, tone, sort_order, is_active, created_at, updated_at
	`, key, params.Label, params.Icon, params.Tone, params.SortOrder, params.IsActive).Scan(
		&p.Key,
		&p.Label,
		&p.Icon,
		&p.Tone,
		&p.SortOrder,
		&p.IsActive,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if isNoRows(err) {
		return Platform{}, ErrNotFound
	}
	if err != nil {
		return Platform{}, fmt.Errorf("update platform: %w", err)
	}
	return p, nil
}

func (r *Repo) DeletePlatform(ctx context.Context, key string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM platforms WHERE key = $1`, key)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrConflict
		}
		return fmt.Errorf("delete platform: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func NormalizePlatformKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
