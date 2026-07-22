package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID                    uuid.UUID
	Email                 string
	PasswordHash          string
	IsAdmin               bool
	CanManagePublications bool
	IsActive              bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Profile struct {
	UserID          uuid.UUID
	FullName        string
	DharmaName      *string
	BirthYear       *int16
	Phone           *string
	CTN             *string
	AvatarBucket    *string
	AvatarObjectKey *string
	UpdatedAt       time.Time
}

type UserWithProfile struct {
	User    User
	Profile Profile
}

type CreateUserParams struct {
	Email                 string
	PasswordHash          string
	IsAdmin               bool
	CanManagePublications bool
	IsActive              bool
	FullName              string
	DharmaName            *string
	BirthYear             *int16
	Phone                 *string
	CTN                   *string
}

type UpdateUserParams struct {
	FullName              string
	DharmaName            *string
	BirthYear             *int16
	Phone                 *string
	CTN                   *string
	IsAdmin               bool
	CanManagePublications bool
	IsActive              bool
}

type ImportUserParams struct {
	ExistingUserID        *uuid.UUID
	Email                 string
	PasswordHash          *string
	FullName              string
	DharmaName            *string
	BirthYear             *int16
	Phone                 *string
	CTN                   *string
	IsAdmin               bool
	CanManagePublications bool
	IsActive              bool
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (User, error) {
	const q = `
		SELECT id, email, password_hash, is_admin, can_manage_publications, is_active, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	return scanUser(r.pool.QueryRow(ctx, q, email))
}

func (r *Repo) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	const q = `
		SELECT id, email, password_hash, is_admin, can_manage_publications, is_active, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.pool.QueryRow(ctx, q, id))
}

func (r *Repo) GetProfile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	const q = `
		SELECT user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
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
		INSERT INTO users (email, password_hash, is_admin, can_manage_publications, is_active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, password_hash, is_admin, can_manage_publications, is_active, created_at, updated_at
	`, params.Email, params.PasswordHash, params.IsAdmin, params.CanManagePublications, params.IsActive))
	if err != nil {
		return UserWithProfile{}, err
	}

	profile, err := scanProfile(tx.QueryRow(ctx, `
		INSERT INTO user_profiles (user_id, full_name, dharma_name, birth_year, phone, ctn)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
	`, user.ID, params.FullName, params.DharmaName, params.BirthYear, params.Phone, params.CTN))
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
		WHERE (
			$1 = ''
			OR u.email ILIKE '%' || $1 || '%'
			OR p.full_name ILIKE '%' || $1 || '%'
			OR COALESCE(p.dharma_name, '') ILIKE '%' || $1 || '%'
			OR COALESCE(p.ctn, '') ILIKE '%' || $1 || '%'
		)
	`

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+baseFrom, q).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.email, u.password_hash, u.is_admin, u.can_manage_publications, u.is_active, u.created_at, u.updated_at,
		       p.user_id, p.full_name, p.dharma_name, p.birth_year, p.phone, p.ctn, p.avatar_bucket, p.avatar_object_key, p.updated_at
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
			&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CanManagePublications, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
			&p.UserID, &p.FullName, &p.DharmaName, &p.BirthYear, &p.Phone, &p.CTN, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, UserWithProfile{User: u, Profile: p})
	}
	return out, total, rows.Err()
}

func (r *Repo) ListUsersByEmails(ctx context.Context, emails []string) ([]UserWithProfile, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.email, u.password_hash, u.is_admin, u.can_manage_publications, u.is_active, u.created_at, u.updated_at,
		       p.user_id, p.full_name, p.dharma_name, p.birth_year, p.phone, p.ctn, p.avatar_bucket, p.avatar_object_key, p.updated_at
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		WHERE u.email = ANY($1)
	`, emails)
	if err != nil {
		return nil, fmt.Errorf("list users by email: %w", err)
	}
	defer rows.Close()

	out := make([]UserWithProfile, 0, len(emails))
	for rows.Next() {
		var u User
		var p Profile
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CanManagePublications, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
			&p.UserID, &p.FullName, &p.DharmaName, &p.BirthYear, &p.Phone, &p.CTN, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user by email: %w", err)
		}
		out = append(out, UserWithProfile{User: u, Profile: p})
	}
	return out, rows.Err()
}

func (r *Repo) ListUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, email, password_hash, is_admin, can_manage_publications, is_active, created_at, updated_at
		FROM users
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("list users by ids: %w", err)
	}
	defer rows.Close()

	out := make([]User, 0, len(ids))
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CanManagePublications, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user by id: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *Repo) ListProfilesByUserIDs(ctx context.Context, ids []uuid.UUID) ([]Profile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
		FROM user_profiles
		WHERE user_id = ANY($1)
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("list profiles by user ids: %w", err)
	}
	defer rows.Close()

	out := make([]Profile, 0, len(ids))
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UserID, &p.FullName, &p.DharmaName, &p.BirthYear, &p.Phone, &p.CTN, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan profile by user id: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) UpdateUserActive(ctx context.Context, id uuid.UUID, isActive bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET is_active = $2, updated_at = now() WHERE id = $1`, id, isActive)
	if err != nil {
		return fmt.Errorf("update user active: %w", err)
	}
	return nil
}

func (r *Repo) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM users
		WHERE is_admin = TRUE AND is_active = TRUE
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}
	return count, nil
}

func (r *Repo) UpdateUserAdmin(ctx context.Context, id uuid.UUID, isAdmin bool) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET is_admin = $2, updated_at = now() WHERE id = $1`, id, isAdmin)
	if err != nil {
		return fmt.Errorf("update user admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) UpdateUserPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE users
		SET password_hash = $2, updated_at = now()
		WHERE id = $1
	`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) UpdateUserWithProfile(ctx context.Context, id uuid.UUID, params UpdateUserParams) (UserWithProfile, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserWithProfile{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET is_admin = $2, can_manage_publications = $3, is_active = $4, updated_at = now()
		WHERE id = $1
		RETURNING id, email, password_hash, is_admin, can_manage_publications, is_active, created_at, updated_at
	`, id, params.IsAdmin, params.CanManagePublications, params.IsActive))
	if err != nil {
		return UserWithProfile{}, err
	}

	profile, err := scanProfile(tx.QueryRow(ctx, `
		UPDATE user_profiles
		SET full_name = $2, dharma_name = $3, birth_year = $4, phone = $5, ctn = $6, updated_at = now()
		WHERE user_id = $1
		RETURNING user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
	`, id, params.FullName, params.DharmaName, params.BirthYear, params.Phone, params.CTN))
	if err != nil {
		return UserWithProfile{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return UserWithProfile{}, fmt.Errorf("commit tx: %w", err)
	}
	return UserWithProfile{User: user, Profile: profile}, nil
}

func (r *Repo) ImportUsers(ctx context.Context, ops []ImportUserParams) error {
	if len(ops) == 0 {
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin import users tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, op := range ops {
		if op.ExistingUserID == nil {
			if op.PasswordHash == nil || *op.PasswordHash == "" {
				return fmt.Errorf("import user %s: missing password hash", op.Email)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO users (email, password_hash, is_admin, can_manage_publications, is_active)
				VALUES ($1, $2, $3, $4, $5)
			`, op.Email, *op.PasswordHash, op.IsAdmin, op.CanManagePublications, op.IsActive); err != nil {
				if isUniqueViolation(err) {
					return ErrConflict
				}
				return fmt.Errorf("create imported user: %w", err)
			}
			var createdUserID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, op.Email).Scan(&createdUserID); err != nil {
				return fmt.Errorf("load created user id: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_profiles (user_id, full_name, dharma_name, birth_year, phone, ctn)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, createdUserID, op.FullName, op.DharmaName, op.BirthYear, op.Phone, op.CTN); err != nil {
				return fmt.Errorf("create imported profile: %w", err)
			}
			continue
		}

		tag, err := tx.Exec(ctx, `
			UPDATE users
			SET is_admin = $2,
			    can_manage_publications = $3,
			    is_active = $4,
			    updated_at = now()
			WHERE id = $1
		`, *op.ExistingUserID, op.IsAdmin, op.CanManagePublications, op.IsActive)
		if err != nil {
			return fmt.Errorf("update imported user: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		tag, err = tx.Exec(ctx, `
			UPDATE user_profiles
			SET full_name = $2,
			    dharma_name = $3,
			    birth_year = $4,
			    phone = $5,
			    ctn = $6,
			    updated_at = now()
			WHERE user_id = $1
		`, *op.ExistingUserID, op.FullName, op.DharmaName, op.BirthYear, op.Phone, op.CTN)
		if err != nil {
			return fmt.Errorf("update imported profile: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		if op.PasswordHash != nil && *op.PasswordHash != "" {
			tag, err = tx.Exec(ctx, `
				UPDATE users
				SET password_hash = $2,
				    updated_at = now()
				WHERE id = $1
			`, *op.ExistingUserID, *op.PasswordHash)
			if err != nil {
				return fmt.Errorf("update imported password: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit import users tx: %w", err)
	}
	return nil
}

func (r *Repo) UpdateProfile(ctx context.Context, userID uuid.UUID, fullName string, dharmaName *string, birthYear *int16, phone *string, ctn *string) (Profile, error) {
	return scanProfile(r.pool.QueryRow(ctx, `
		UPDATE user_profiles
		SET full_name = $2, dharma_name = $3, birth_year = $4, phone = $5, ctn = $6, updated_at = now()
		WHERE user_id = $1
		RETURNING user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
	`, userID, fullName, dharmaName, birthYear, phone, ctn))
}

func (r *Repo) UpdateProfileAvatar(ctx context.Context, userID uuid.UUID, avatarBucket *string, avatarObjectKey *string) (Profile, error) {
	return scanProfile(r.pool.QueryRow(ctx, `
		UPDATE user_profiles
		SET avatar_bucket = $2, avatar_object_key = $3, updated_at = now()
		WHERE user_id = $1
		RETURNING user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, updated_at
	`, userID, avatarBucket, avatarObjectKey))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.CanManagePublications, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if isNoRows(err) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

func scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	if err := row.Scan(&p.UserID, &p.FullName, &p.DharmaName, &p.BirthYear, &p.Phone, &p.CTN, &p.AvatarBucket, &p.AvatarObjectKey, &p.UpdatedAt); err != nil {
		if isNoRows(err) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("scan profile: %w", err)
	}
	return p, nil
}
