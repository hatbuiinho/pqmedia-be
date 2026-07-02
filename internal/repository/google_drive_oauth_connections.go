package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const GoogleDriveOAuthProvider = "google_drive"

type GoogleDriveOAuthConnection struct {
	Provider              string
	GoogleEmail           string
	EncryptedRefreshToken string
	Scope                 string
	TokenType             *string
	ConnectedByUserID     *uuid.UUID
	ConnectedAt           time.Time
	LastRefreshAt         *time.Time
	LastError             *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type UpsertGoogleDriveOAuthConnectionParams struct {
	GoogleEmail           string
	EncryptedRefreshToken string
	Scope                 string
	TokenType             *string
	ConnectedByUserID     uuid.UUID
	ConnectedAt           time.Time
}

func (r *Repo) GetGoogleDriveOAuthConnection(ctx context.Context) (GoogleDriveOAuthConnection, error) {
	var item GoogleDriveOAuthConnection
	err := r.pool.QueryRow(ctx, `
		SELECT provider, google_email, encrypted_refresh_token, scope, token_type,
		       connected_by_user_id, connected_at, last_refresh_at, last_error, created_at, updated_at
		FROM google_drive_oauth_connections
		WHERE provider = $1
	`, GoogleDriveOAuthProvider).Scan(
		&item.Provider,
		&item.GoogleEmail,
		&item.EncryptedRefreshToken,
		&item.Scope,
		&item.TokenType,
		&item.ConnectedByUserID,
		&item.ConnectedAt,
		&item.LastRefreshAt,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return GoogleDriveOAuthConnection{}, ErrNotFound
		}
		return GoogleDriveOAuthConnection{}, fmt.Errorf("get google drive oauth connection: %w", err)
	}
	return item, nil
}

func (r *Repo) UpsertGoogleDriveOAuthConnection(ctx context.Context, params UpsertGoogleDriveOAuthConnectionParams) error {
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO google_drive_oauth_connections (
			provider,
			google_email,
			encrypted_refresh_token,
			scope,
			token_type,
			connected_by_user_id,
			connected_at,
			last_refresh_at,
			last_error,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), NULL, now())
		ON CONFLICT (provider) DO UPDATE
		SET google_email = EXCLUDED.google_email,
		    encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
		    scope = EXCLUDED.scope,
		    token_type = EXCLUDED.token_type,
		    connected_by_user_id = EXCLUDED.connected_by_user_id,
		    connected_at = EXCLUDED.connected_at,
		    last_refresh_at = now(),
		    last_error = NULL,
		    updated_at = now()
	`, GoogleDriveOAuthProvider,
		params.GoogleEmail,
		params.EncryptedRefreshToken,
		params.Scope,
		params.TokenType,
		params.ConnectedByUserID,
		params.ConnectedAt,
	); err != nil {
		return fmt.Errorf("upsert google drive oauth connection: %w", err)
	}
	return nil
}

func (r *Repo) ClearGoogleDriveOAuthConnection(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM google_drive_oauth_connections
		WHERE provider = $1
	`, GoogleDriveOAuthProvider); err != nil {
		return fmt.Errorf("clear google drive oauth connection: %w", err)
	}
	return nil
}

func (r *Repo) MarkGoogleDriveOAuthConnectionRefreshed(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE google_drive_oauth_connections
		SET last_refresh_at = now(),
		    last_error = NULL,
		    updated_at = now()
		WHERE provider = $1
	`, GoogleDriveOAuthProvider); err != nil {
		return fmt.Errorf("mark google drive oauth connection refreshed: %w", err)
	}
	return nil
}

func (r *Repo) MarkGoogleDriveOAuthConnectionError(ctx context.Context, message string) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE google_drive_oauth_connections
		SET last_error = $2,
		    updated_at = now()
		WHERE provider = $1
	`, GoogleDriveOAuthProvider, message); err != nil {
		return fmt.Errorf("mark google drive oauth connection error: %w", err)
	}
	return nil
}
