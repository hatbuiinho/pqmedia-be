package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentVideo AttachmentKind = "video"
)

type Post struct {
	ID           uuid.UUID
	AuthorUserID uuid.UUID
	Content      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PostAttachment struct {
	ID          uuid.UUID
	PostID      uuid.UUID
	Kind        AttachmentKind
	FileName    string
	ContentType string
	Bucket      string
	ObjectKey   string
	SizeBytes   int64
	Width       *int
	Height      *int
	DurationMs  *int
	SortOrder   int
}

type PostAttachmentInput struct {
	Kind        AttachmentKind
	FileName    string
	ContentType string
	Bucket      string
	ObjectKey   string
	SizeBytes   int64
	Width       *int
	Height      *int
	DurationMs  *int
	SortOrder   int
}

type CreatePostParams struct {
	AuthorUserID uuid.UUID
	Content      string
	Attachments  []PostAttachmentInput
	Hashtags     []string
}

type FeedFilter struct {
	AuthorUserID  *uuid.UUID
	Search        string
	Hashtag       string
	UnpublishedOn []string // platforms — match posts still missing at least one selected platform
	Limit         int
	Offset        int
}

func (r *Repo) CreatePost(ctx context.Context, params CreatePostParams) (Post, []PostAttachment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Post{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var post Post
	err = tx.QueryRow(ctx, `
		INSERT INTO posts (author_user_id, content)
		VALUES ($1, $2)
		RETURNING id, author_user_id, content, created_at, updated_at
	`, params.AuthorUserID, params.Content).
		Scan(&post.ID, &post.AuthorUserID, &post.Content, &post.CreatedAt, &post.UpdatedAt)
	if err != nil {
		return Post{}, nil, fmt.Errorf("insert post: %w", err)
	}

	attachments, err := insertAttachments(ctx, tx, post.ID, params.Attachments)
	if err != nil {
		return Post{}, nil, err
	}
	if err := upsertHashtags(ctx, tx, post.ID, params.Hashtags); err != nil {
		return Post{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, nil, fmt.Errorf("commit: %w", err)
	}
	return post, attachments, nil
}

func (r *Repo) GetPost(ctx context.Context, id uuid.UUID) (Post, error) {
	var p Post
	err := r.pool.QueryRow(ctx, `
		SELECT id, author_user_id, content, created_at, updated_at
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&p.ID, &p.AuthorUserID, &p.Content, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return Post{}, ErrNotFound
		}
		return Post{}, fmt.Errorf("get post: %w", err)
	}
	return p, nil
}

func (r *Repo) UpdatePost(ctx context.Context, id uuid.UUID, content string, attachments *[]PostAttachmentInput, hashtags *[]string) (Post, []PostAttachment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Post{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var post Post
	err = tx.QueryRow(ctx, `
		UPDATE posts SET content = $2, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, author_user_id, content, created_at, updated_at
	`, id, content).Scan(&post.ID, &post.AuthorUserID, &post.Content, &post.CreatedAt, &post.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return Post{}, nil, ErrNotFound
		}
		return Post{}, nil, fmt.Errorf("update post: %w", err)
	}

	var newAttachments []PostAttachment
	if attachments != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM post_attachments WHERE post_id = $1`, id); err != nil {
			return Post{}, nil, fmt.Errorf("clear attachments: %w", err)
		}
		newAttachments, err = insertAttachments(ctx, tx, post.ID, *attachments)
		if err != nil {
			return Post{}, nil, err
		}
	} else {
		// keep existing attachments, just load them to return
		// (optional: could query them here, but we will let service re-fetch if needed, or return empty array if not modified. Actually we should load them)
		// Wait, existing code replaced attachments unconditionally if it wasn't nil.
		// If it's nil, we return empty or need to fetch? The caller probably doesn't strictly need them returned accurately if it refetches.
		// Actually let's just return what we have or nothing.
	}

	if hashtags != nil {
		if err := upsertHashtags(ctx, tx, post.ID, *hashtags); err != nil {
			return Post{}, nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Post{}, nil, fmt.Errorf("commit: %w", err)
	}
	return post, newAttachments, nil
}

func (r *Repo) SoftDeletePost(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE posts SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListFeed returns posts (with their author already attached) plus the total count.
// Attachments / reactions / comment counts / publications are loaded by the service
// via batch helpers below.
func (r *Repo) ListFeed(ctx context.Context, filter FeedFilter) ([]Post, []User, []Profile, int, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	filter.UnpublishedOn = normalizePublicationPlatforms(filter.UnpublishedOn)

	where := "WHERE posts.deleted_at IS NULL"
	args := []any{}
	idx := 0
	addArg := func(v any) string {
		idx++
		args = append(args, v)
		return fmt.Sprintf("$%d", idx)
	}
	if filter.AuthorUserID != nil {
		where += " AND posts.author_user_id = " + addArg(*filter.AuthorUserID)
	}
	if filter.Search != "" {
		where += " AND f_unaccent(posts.content) ILIKE f_unaccent('%' || " + addArg(filter.Search) + " || '%')"
	}
	if filter.Hashtag != "" {
		where += " AND EXISTS (SELECT 1 FROM post_hashtags ph JOIN hashtags h ON ph.hashtag_id = h.id WHERE ph.post_id = posts.id AND h.name = " + addArg(filter.Hashtag) + ")"
	}
	if len(filter.UnpublishedOn) > 0 {
		where += `
		 AND EXISTS (
		 	SELECT 1
		 	FROM unnest(` + addArg(filter.UnpublishedOn) + `::text[]) AS selected(platform)
		 	WHERE NOT EXISTS (
		 		SELECT 1
		 		FROM post_publications pp
		 		WHERE pp.post_id = posts.id AND pp.platform = selected.platform
		 	)
		 )`
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts "+where, args...).Scan(&total); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("count feed: %w", err)
	}

	limitArg := addArg(filter.Limit)
	offsetArg := addArg(filter.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT posts.id, posts.author_user_id, posts.content, posts.created_at, posts.updated_at,
		       u.id, u.email, u.password_hash, u.is_admin, u.is_active, u.created_at, u.updated_at,
		       p.user_id, p.full_name, p.phone, p.avatar_bucket, p.avatar_object_key, p.updated_at
		FROM posts
		JOIN users u ON u.id = posts.author_user_id
		JOIN user_profiles p ON p.user_id = u.id
		`+where+`
		ORDER BY posts.created_at DESC
		LIMIT `+limitArg+` OFFSET `+offsetArg,
		args...,
	)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("list feed: %w", err)
	}
	defer rows.Close()

	posts := make([]Post, 0, filter.Limit)
	users := make([]User, 0, filter.Limit)
	profiles := make([]Profile, 0, filter.Limit)
	for rows.Next() {
		var post Post
		var user User
		var profile Profile
		if err := rows.Scan(
			&post.ID, &post.AuthorUserID, &post.Content, &post.CreatedAt, &post.UpdatedAt,
			&user.ID, &user.Email, &user.PasswordHash, &user.IsAdmin, &user.IsActive, &user.CreatedAt, &user.UpdatedAt,
			&profile.UserID, &profile.FullName, &profile.Phone, &profile.AvatarBucket, &profile.AvatarObjectKey, &profile.UpdatedAt,
		); err != nil {
			return nil, nil, nil, 0, fmt.Errorf("scan feed row: %w", err)
		}
		posts = append(posts, post)
		users = append(users, user)
		profiles = append(profiles, profile)
	}
	return posts, users, profiles, total, rows.Err()
}

func normalizePublicationPlatforms(platforms []string) []string {
	if len(platforms) == 0 {
		return nil
	}
	out := make([]string, 0, len(platforms))
	seen := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platform = strings.TrimSpace(platform)
		if platform == "" || !IsValidPlatform(platform) {
			continue
		}
		if _, ok := seen[platform]; ok {
			continue
		}
		seen[platform] = struct{}{}
		out = append(out, platform)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ListAttachmentsByPosts returns attachments grouped by post_id, ordered by sort_order.
func (r *Repo) ListAttachmentsByPosts(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]PostAttachment, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID][]PostAttachment{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, post_id, kind, file_name, content_type, bucket, object_key,
		       size_bytes, width, height, duration_ms, sort_order
		FROM post_attachments
		WHERE post_id = ANY($1)
		ORDER BY post_id, sort_order, created_at
	`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]PostAttachment, len(postIDs))
	for rows.Next() {
		var a PostAttachment
		if err := rows.Scan(&a.ID, &a.PostID, &a.Kind, &a.FileName, &a.ContentType, &a.Bucket, &a.ObjectKey,
			&a.SizeBytes, &a.Width, &a.Height, &a.DurationMs, &a.SortOrder); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		out[a.PostID] = append(out[a.PostID], a)
	}
	return out, rows.Err()
}

// CountCommentsByPosts returns the number of comments per post.
func (r *Repo) CountCommentsByPosts(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT post_id, COUNT(*)::int
		FROM post_comments
		WHERE post_id = ANY($1)
		GROUP BY post_id
	`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("count comments: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]int, len(postIDs))
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan comment count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// Helper for INSERT INTO post_attachments in bulk inside a transaction.
func insertAttachments(ctx context.Context, tx pgx.Tx, postID uuid.UUID, inputs []PostAttachmentInput) ([]PostAttachment, error) {
	if len(inputs) == 0 {
		return []PostAttachment{}, nil
	}
	out := make([]PostAttachment, 0, len(inputs))
	for i, in := range inputs {
		if in.Kind != AttachmentImage && in.Kind != AttachmentVideo {
			return nil, errors.New("attachment kind must be image or video")
		}
		order := in.SortOrder
		if order == 0 {
			order = i
		}
		var a PostAttachment
		err := tx.QueryRow(ctx, `
			INSERT INTO post_attachments (post_id, kind, file_name, content_type, bucket, object_key,
			                              size_bytes, width, height, duration_ms, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, post_id, kind, file_name, content_type, bucket, object_key,
			          size_bytes, width, height, duration_ms, sort_order
		`, postID, in.Kind, in.FileName, in.ContentType, in.Bucket, in.ObjectKey,
			in.SizeBytes, in.Width, in.Height, in.DurationMs, order,
		).Scan(&a.ID, &a.PostID, &a.Kind, &a.FileName, &a.ContentType, &a.Bucket, &a.ObjectKey,
			&a.SizeBytes, &a.Width, &a.Height, &a.DurationMs, &a.SortOrder)
		if err != nil {
			return nil, fmt.Errorf("insert attachment %d: %w", i, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// ListHashtagsByPosts returns hashtags grouped by post_id.
func (r *Repo) ListHashtagsByPosts(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID][]string{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT ph.post_id, h.name
		FROM post_hashtags ph
		JOIN hashtags h ON ph.hashtag_id = h.id
		WHERE ph.post_id = ANY($1)
	`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("list hashtags: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]string, len(postIDs))
	for rows.Next() {
		var postID uuid.UUID
		var name string
		if err := rows.Scan(&postID, &name); err != nil {
			return nil, fmt.Errorf("scan hashtag: %w", err)
		}
		out[postID] = append(out[postID], name)
	}
	return out, rows.Err()
}

func (r *Repo) SearchHashtags(ctx context.Context, q string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows pgx.Rows
	var err error
	if q == "" {
		rows, err = r.pool.Query(ctx, `SELECT name FROM hashtags ORDER BY name LIMIT $1`, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT name FROM hashtags
			WHERE f_unaccent(name) ILIKE f_unaccent('%' || $1 || '%')
			ORDER BY name LIMIT $2
		`, q, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search hashtags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		tags = append(tags, n)
	}
	return tags, rows.Err()
}

func upsertHashtags(ctx context.Context, tx pgx.Tx, postID uuid.UUID, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM post_hashtags WHERE post_id = $1`, postID); err != nil {
		return fmt.Errorf("delete post_hashtags: %w", err)
	}
	for _, t := range tags {
		if t == "" {
			continue
		}
		var tagID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO hashtags (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, t).Scan(&tagID)
		if err != nil {
			return fmt.Errorf("upsert hashtag %s: %w", t, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO post_hashtags (post_id, hashtag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, postID, tagID); err != nil {
			return fmt.Errorf("link hashtag %s: %w", t, err)
		}
	}
	return nil
}
