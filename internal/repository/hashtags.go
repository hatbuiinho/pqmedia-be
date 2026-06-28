package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Hashtag struct {
	ID                   string
	Name                 string
	PostCount            int
	UnpublishedPostCount int
	CreatedAt            time.Time
}

func (r *Repo) ListHashtagSummaries(ctx context.Context, q string, limit int) ([]Hashtag, error) {
	query := `
		SELECT
			h.id::text,
			h.name,
			COUNT(DISTINCT p.id)::int AS post_count,
			COUNT(
				DISTINCT CASE
					WHEN p.id IS NOT NULL
						AND NOT EXISTS (
							SELECT 1
							FROM post_publications pp
							WHERE pp.post_id = p.id
						)
					THEN p.id
					ELSE NULL
				END
			)::int AS unpublished_post_count,
			h.created_at
		FROM hashtags h
		LEFT JOIN post_hashtags ph ON ph.hashtag_id = h.id
		LEFT JOIN posts p ON p.id = ph.post_id AND p.deleted_at IS NULL
	`
	args := []any{}
	if trimmed := strings.TrimSpace(q); trimmed != "" {
		query += ` WHERE f_unaccent(h.name) ILIKE f_unaccent('%' || $1 || '%')`
		args = append(args, trimmed)
	}
	query += `
		GROUP BY h.id, h.name, h.created_at
		ORDER BY post_count DESC, h.name ASC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list hashtag summaries: %w", err)
	}
	defer rows.Close()

	out := make([]Hashtag, 0, 32)
	for rows.Next() {
		var item Hashtag
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.PostCount,
			&item.UnpublishedPostCount,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan hashtag summary: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) CreateHashtag(ctx context.Context, name string) (Hashtag, error) {
	var item Hashtag
	err := r.pool.QueryRow(ctx, `
		INSERT INTO hashtags (name)
		VALUES ($1)
		RETURNING id::text, name, 0, 0, created_at
	`, name).Scan(&item.ID, &item.Name, &item.PostCount, &item.UnpublishedPostCount, &item.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Hashtag{}, ErrConflict
		}
		return Hashtag{}, fmt.Errorf("create hashtag: %w", err)
	}
	return item, nil
}

func (r *Repo) RenameHashtag(ctx context.Context, currentName, nextName string) (Hashtag, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Hashtag{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var item Hashtag
	err = tx.QueryRow(ctx, `
		UPDATE hashtags
		SET name = $2
		WHERE name = $1
		RETURNING id::text, name, created_at
	`, currentName, nextName).Scan(&item.ID, &item.Name, &item.CreatedAt)
	if isNoRows(err) {
		return Hashtag{}, ErrNotFound
	}
	if err != nil {
		if isUniqueViolation(err) {
			return Hashtag{}, ErrConflict
		}
		return Hashtag{}, fmt.Errorf("rename hashtag: %w", err)
	}

	if err := tx.QueryRow(ctx, `
		SELECT
			COUNT(DISTINCT p.id)::int AS post_count,
			COUNT(
				DISTINCT CASE
					WHEN NOT EXISTS (
						SELECT 1
						FROM post_publications pp
						WHERE pp.post_id = p.id
					)
					THEN p.id
					ELSE NULL
				END
			)::int AS unpublished_post_count
		FROM post_hashtags ph
		JOIN posts p ON p.id = ph.post_id AND p.deleted_at IS NULL
		WHERE ph.hashtag_id = $1::uuid
	`, item.ID).Scan(&item.PostCount, &item.UnpublishedPostCount); err != nil {
		return Hashtag{}, fmt.Errorf("count renamed hashtag posts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Hashtag{}, fmt.Errorf("commit: %w", err)
	}
	return item, nil
}

func (r *Repo) DeleteHashtag(ctx context.Context, name string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM hashtags WHERE name = $1`, name).Scan(&id)
	if isNoRows(err) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get hashtag: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM post_hashtags WHERE hashtag_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete post hashtag links: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM hashtags WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete hashtag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
