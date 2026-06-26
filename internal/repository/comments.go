package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID           uuid.UUID
	PostID       uuid.UUID
	AuthorUserID uuid.UUID
	Content      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CommentWithAuthor struct {
	Comment Comment
	Author  User
	Profile Profile
}

func (r *Repo) CreateComment(ctx context.Context, postID, authorUserID uuid.UUID, content string) (Comment, error) {
	var c Comment
	err := r.pool.QueryRow(ctx, `
		INSERT INTO post_comments (post_id, author_user_id, content)
		VALUES ($1, $2, $3)
		RETURNING id, post_id, author_user_id, content, created_at, updated_at
	`, postID, authorUserID, content).
		Scan(&c.ID, &c.PostID, &c.AuthorUserID, &c.Content, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Comment{}, fmt.Errorf("insert comment: %w", err)
	}
	return c, nil
}

func (r *Repo) GetComment(ctx context.Context, id uuid.UUID) (Comment, error) {
	var c Comment
	err := r.pool.QueryRow(ctx, `
		SELECT id, post_id, author_user_id, content, created_at, updated_at
		FROM post_comments WHERE id = $1
	`, id).Scan(&c.ID, &c.PostID, &c.AuthorUserID, &c.Content, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return c, nil
}

func (r *Repo) DeleteComment(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM post_comments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListCommentsByPost loads comments + their authors in one query, ordered oldest-first.
func (r *Repo) ListCommentsByPost(ctx context.Context, postID uuid.UUID) ([]CommentWithAuthor, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.post_id, c.author_user_id, c.content, c.created_at, c.updated_at,
		       u.id, u.email, u.password_hash, u.is_admin, u.is_active, u.created_at, u.updated_at,
		       p.user_id, p.full_name, p.phone, p.avatar_bucket, p.avatar_object_key, p.updated_at
		FROM post_comments c
		JOIN users u ON u.id = c.author_user_id
		JOIN user_profiles p ON p.user_id = u.id
		WHERE c.post_id = $1
		ORDER BY c.created_at ASC
	`, postID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	out := make([]CommentWithAuthor, 0, 8)
	for rows.Next() {
		var c Comment
		var u User
		var p Profile
		if err := rows.Scan(
			&c.ID, &c.PostID, &c.AuthorUserID, &c.Content, &c.CreatedAt, &c.UpdatedAt,
			&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
			&p.UserID, &p.FullName, &p.Phone, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		out = append(out, CommentWithAuthor{Comment: c, Author: u, Profile: p})
	}
	return out, rows.Err()
}
