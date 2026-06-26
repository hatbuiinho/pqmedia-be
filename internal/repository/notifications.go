package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID              uuid.UUID
	RecipientUserID uuid.UUID
	ActorUserID     *uuid.UUID
	Kind            string
	PostID          *uuid.UUID
	CommentID       *uuid.UUID
	Title           string
	Body            string
	RouteURL        *string
	Payload         json.RawMessage
	ReadAt          *time.Time
	CreatedAt       time.Time
}

type CreateNotificationParams struct {
	RecipientUserID uuid.UUID
	ActorUserID     *uuid.UUID
	Kind            string
	PostID          *uuid.UUID
	CommentID       *uuid.UUID
	Title           string
	Body            string
	RouteURL        *string
	Payload         json.RawMessage
}

func (r *Repo) CreateNotification(ctx context.Context, params CreateNotificationParams) (Notification, error) {
	payload := params.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	var n Notification
	err := r.pool.QueryRow(ctx, `
		INSERT INTO notifications
			(recipient_user_id, actor_user_id, kind, post_id, comment_id, title, body, route_url, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, recipient_user_id, actor_user_id, kind, post_id, comment_id,
		          title, body, route_url, payload, read_at, created_at
	`, params.RecipientUserID, params.ActorUserID, params.Kind, params.PostID, params.CommentID,
		params.Title, params.Body, params.RouteURL, payload).
		Scan(&n.ID, &n.RecipientUserID, &n.ActorUserID, &n.Kind, &n.PostID, &n.CommentID,
			&n.Title, &n.Body, &n.RouteURL, &n.Payload, &n.ReadAt, &n.CreatedAt)
	if err != nil {
		return Notification{}, fmt.Errorf("insert notification: %w", err)
	}
	return n, nil
}

func (r *Repo) ListNotifications(ctx context.Context, recipientUserID uuid.UUID, unreadOnly bool, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, recipient_user_id, actor_user_id, kind, post_id, comment_id,
	            title, body, route_url, payload, read_at, created_at
	      FROM notifications
	      WHERE recipient_user_id = $1`
	if unreadOnly {
		q += " AND read_at IS NULL"
	}
	q += " ORDER BY created_at DESC LIMIT $2"

	rows, err := r.pool.Query(ctx, q, recipientUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	out := make([]Notification, 0, limit)
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.RecipientUserID, &n.ActorUserID, &n.Kind, &n.PostID, &n.CommentID,
			&n.Title, &n.Body, &n.RouteURL, &n.Payload, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repo) MarkNotificationRead(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE id = $1 AND recipient_user_id = $2 AND read_at IS NULL
	`, id, userID)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE recipient_user_id = $1 AND read_at IS NULL
	`, userID)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	return nil
}

// ----------------- Push subscriptions -----------------

type PushSubscription struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Endpoint    string
	P256DH      string
	Auth        string
	UserAgent   *string
	DeviceLabel *string
	Enabled     bool
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

type UpsertSubscriptionParams struct {
	UserID      uuid.UUID
	Endpoint    string
	P256DH      string
	Auth        string
	UserAgent   *string
	DeviceLabel *string
}

func (r *Repo) UpsertPushSubscription(ctx context.Context, params UpsertSubscriptionParams) (PushSubscription, error) {
	var s PushSubscription
	err := r.pool.QueryRow(ctx, `
		INSERT INTO web_push_subscriptions
			(user_id, endpoint, p256dh, auth, user_agent, device_label, enabled, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, now())
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    p256dh = EXCLUDED.p256dh,
		    auth = EXCLUDED.auth,
		    user_agent = EXCLUDED.user_agent,
		    device_label = EXCLUDED.device_label,
		    enabled = TRUE,
		    updated_at = now(),
		    last_seen_at = now(),
		    disabled_at = NULL
		RETURNING id, user_id, endpoint, p256dh, auth, user_agent, device_label,
		          enabled, created_at, last_seen_at
	`, params.UserID, params.Endpoint, params.P256DH, params.Auth, params.UserAgent, params.DeviceLabel).
		Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256DH, &s.Auth, &s.UserAgent, &s.DeviceLabel,
			&s.Enabled, &s.CreatedAt, &s.LastSeenAt)
	if err != nil {
		return PushSubscription{}, fmt.Errorf("upsert subscription: %w", err)
	}
	return s, nil
}

func (r *Repo) DisablePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE web_push_subscriptions
		SET enabled = FALSE, disabled_at = now(), updated_at = now()
		WHERE user_id = $1 AND endpoint = $2
	`, userID, endpoint)
	if err != nil {
		return fmt.Errorf("disable subscription: %w", err)
	}
	return nil
}

func (r *Repo) DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM web_push_subscriptions WHERE endpoint = $1`, endpoint)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}

func (r *Repo) ActivePushSubscriptionsForUser(ctx context.Context, userID uuid.UUID) ([]PushSubscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, endpoint, p256dh, auth, user_agent, device_label,
		       enabled, created_at, last_seen_at
		FROM web_push_subscriptions
		WHERE user_id = $1 AND enabled = TRUE
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	out := make([]PushSubscription, 0, 2)
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256DH, &s.Auth, &s.UserAgent, &s.DeviceLabel,
			&s.Enabled, &s.CreatedAt, &s.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
