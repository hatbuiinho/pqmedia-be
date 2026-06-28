package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Publication struct {
	ID                uuid.UUID
	PostID            uuid.UUID
	Platform          string
	ExternalURL       *string
	PublishedAt       time.Time
	PublishedByUserID uuid.UUID
	PublishedByName   string
	PublishedByAvatar *string
	Note              *string
}

func (r *Repo) UpsertPublication(ctx context.Context, postID uuid.UUID, platform string, externalURL, note *string, publishedAt time.Time, publishedByUserID uuid.UUID) (Publication, error) {
	var p Publication
	err := r.pool.QueryRow(ctx, `
		WITH upserted AS (
			INSERT INTO post_publications
				(post_id, platform, external_url, published_at, published_by_user_id, note)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (post_id, platform) DO UPDATE
			SET external_url = EXCLUDED.external_url,
			    published_at = EXCLUDED.published_at,
			    published_by_user_id = EXCLUDED.published_by_user_id,
			    note = EXCLUDED.note
			RETURNING id, post_id, platform, external_url, published_at, published_by_user_id, note
		)
		SELECT u.id, u.post_id, u.platform, u.external_url, u.published_at, u.published_by_user_id,
		       p.full_name, p.avatar_object_key, u.note
		FROM upserted u
		JOIN user_profiles p ON p.user_id = u.published_by_user_id
	`, postID, platform, externalURL, publishedAt, publishedByUserID, note).
		Scan(
			&p.ID,
			&p.PostID,
			&p.Platform,
			&p.ExternalURL,
			&p.PublishedAt,
			&p.PublishedByUserID,
			&p.PublishedByName,
			&p.PublishedByAvatar,
			&p.Note,
		)
	if err != nil {
		return Publication{}, fmt.Errorf("upsert publication: %w", err)
	}
	return p, nil
}

func (r *Repo) DeletePublication(ctx context.Context, postID uuid.UUID, platform string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM post_publications WHERE post_id = $1 AND platform = $2`, postID, platform)
	if err != nil {
		return fmt.Errorf("delete publication: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) ListPublicationsByPosts(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]Publication, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID][]Publication{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT pp.id, pp.post_id, pp.platform, pp.external_url, pp.published_at, pp.published_by_user_id,
		       p.full_name, p.avatar_object_key, pp.note
		FROM post_publications pp
		JOIN user_profiles p ON p.user_id = pp.published_by_user_id
		WHERE pp.post_id = ANY($1)
		ORDER BY post_id, platform
	`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]Publication, len(postIDs))
	for rows.Next() {
		var p Publication
		if err := rows.Scan(
			&p.ID,
			&p.PostID,
			&p.Platform,
			&p.ExternalURL,
			&p.PublishedAt,
			&p.PublishedByUserID,
			&p.PublishedByName,
			&p.PublishedByAvatar,
			&p.Note,
		); err != nil {
			return nil, fmt.Errorf("scan publication: %w", err)
		}
		out[p.PostID] = append(out[p.PostID], p)
	}
	return out, rows.Err()
}
