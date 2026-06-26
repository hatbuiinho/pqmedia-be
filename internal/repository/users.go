package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	IsAdmin      bool
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Profile struct {
	UserID          uuid.UUID
	FullName        string
	Phone           *string
	AvatarBucket    *string
	AvatarObjectKey *string
	UpdatedAt       time.Time
}

type UserWithProfile struct {
	User    User
	Profile Profile
}

type CreateUserParams struct {
	Email        string
	PasswordHash string
	IsAdmin      bool
	FullName     string
	Phone        *string
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (User, error) {
	const q = `
		SELECT id, email, password_hash, is_admin, is_active, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	return scanUser(r.pool.QueryRow(ctx, q, email))
}

func (r *Repo) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	const q = `
		SELECT id, email, password_hash, is_admin, is_active, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.pool.QueryRow(ctx, q, id))
}

func (r *Repo) GetProfile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	const q = `
		SELECT user_id, full_name, phone, avatar_bucket, avatar_object_key, updated_at
		FROM user_profiles
		WHERE user_id = $1
	`
	return scanProfile(r.pool.QueryRow(ctx, q, userID))
}

func (r *Repo) CreateUserWithProfile(ctx context.Context, params CreateUserParams) (UserWithProfile, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserWithProfile{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, is_admin)
		VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, is_admin, is_active, created_at, updated_at
	`, params.Email, params.PasswordHash, params.IsAdmin))
	if err != nil {
		return UserWithProfile{}, err
	}

	profile, err := scanProfile(tx.QueryRow(ctx, `
		INSERT INTO user_profiles (user_id, full_name, phone)
		VALUES ($1, $2, $3)
		RETURNING user_id, full_name, phone, avatar_bucket, avatar_object_key, updated_at
	`, user.ID, params.FullName, params.Phone))
	if err != nil {
		return UserWithProfile{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return UserWithProfile{}, fmt.Errorf("commit tx: %w", err)
	}
	return UserWithProfile{User: user, Profile: profile}, nil
}

// ListUsers returns users matching q (full_name ILIKE or email ILIKE) with the total count.
func (r *Repo) ListUsers(ctx context.Context, q string, limit, offset int) ([]UserWithProfile, int, error) {
	const baseFrom = `
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		WHERE ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR p.full_name ILIKE '%' || $1 || '%')
	`

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+baseFrom, q).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.email, u.password_hash, u.is_admin, u.is_active, u.created_at, u.updated_at,
		       p.user_id, p.full_name, p.phone, p.avatar_bucket, p.avatar_object_key, p.updated_at
	`+baseFrom+`
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3
	`, q, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	out := make([]UserWithProfile, 0, limit)
	for rows.Next() {
		var u User
		var p Profile
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
			&p.UserID, &p.FullName, &p.Phone, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, UserWithProfile{User: u, Profile: p})
	}
	return out, total, rows.Err()
}

func (r *Repo) UpdateUserActive(ctx context.Context, id uuid.UUID, isActive bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET is_active = $2, updated_at = now() WHERE id = $1`, id, isActive)
	if err != nil {
		return fmt.Errorf("update user active: %w", err)
	}
	return nil
}

func (r *Repo) UpdateProfile(ctx context.Context, userID uuid.UUID, fullName string, phone *string) (Profile, error) {
	return scanProfile(r.pool.QueryRow(ctx, `
		UPDATE user_profiles
		SET full_name = $2, phone = $3, updated_at = now()
		WHERE user_id = $1
		RETURNING user_id, full_name, phone, avatar_bucket, avatar_object_key, updated_at
	`, userID, fullName, phone))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if isNoRows(err) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

func scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	if err := row.Scan(&p.UserID, &p.FullName, &p.Phone, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt); err != nil {
		if isNoRows(err) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("scan profile: %w", err)
	}
	return p, nil
}
