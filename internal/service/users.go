package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"pqmedia/be/internal/auth"
	"pqmedia/be/internal/repository"
)

// Principal is the authenticated user view returned to handlers.
type Principal struct {
	User    repository.User
	Profile repository.Profile
}

type TokenPair struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
}

type LoginResult struct {
	Principal Principal
	Tokens    TokenPair
}

type CreateUserInput struct {
	Email    string
	Password string
	FullName string
	Phone    *string
	IsAdmin  bool
}

type UpdateUserInput struct {
	FullName string
	Phone    *string
	IsAdmin  bool
	IsActive bool
}

type ResetUserPasswordInput struct {
	Password string
}

type Page struct {
	Limit  int
	Offset int
	Count  int
	Total  int
}

type UserService struct {
	Repo            *repository.Repo
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Now             func() time.Time
}

func (s *UserService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *UserService) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return LoginResult{}, ValidationError("email and password are required")
	}
	user, err := s.Repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return LoginResult{}, ErrUnauthorized
		}
		return LoginResult{}, err
	}
	if !user.IsActive {
		return LoginResult{}, ErrUnauthorized
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	profile, err := s.Repo.GetProfile(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	tokens, err := s.issueTokens(user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Principal: Principal{User: user, Profile: profile},
		Tokens:    tokens,
	}, nil
}

func (s *UserService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	claims, err := auth.Parse(s.JWTSecret, refreshToken, auth.RefreshToken)
	if err != nil {
		return TokenPair{}, ErrUnauthorized
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return TokenPair{}, ErrUnauthorized
	}
	user, err := s.Repo.GetUserByID(ctx, userID)
	if err != nil {
		return TokenPair{}, ErrUnauthorized
	}
	if !user.IsActive {
		return TokenPair{}, ErrUnauthorized
	}
	return s.issueTokens(user.ID)
}

func (s *UserService) PrincipalFromAccessToken(ctx context.Context, accessToken string) (Principal, error) {
	claims, err := auth.Parse(s.JWTSecret, accessToken, auth.AccessToken)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	user, err := s.Repo.GetUserByID(ctx, userID)
	if err != nil || !user.IsActive {
		return Principal{}, ErrUnauthorized
	}
	profile, err := s.Repo.GetProfile(ctx, user.ID)
	if err != nil {
		return Principal{}, err
	}
	return Principal{User: user, Profile: profile}, nil
}

func (s *UserService) CreateUser(ctx context.Context, actor Principal, input CreateUserInput) (Principal, error) {
	if !actor.User.IsAdmin {
		return Principal{}, ErrForbidden
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		return Principal{}, ValidationError("invalid email")
	}
	if len(input.Password) < 8 {
		return Principal{}, ValidationError("password must be at least 8 characters")
	}
	if strings.TrimSpace(input.FullName) == "" {
		return Principal{}, ValidationError("full_name is required")
	}

	if existing, err := s.Repo.GetUserByEmail(ctx, email); err == nil {
		_ = existing
		return Principal{}, ErrConflict
	} else if !errors.Is(err, repository.ErrNotFound) {
		return Principal{}, err
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return Principal{}, err
	}
	created, err := s.Repo.CreateUserWithProfile(ctx, repository.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		IsAdmin:      input.IsAdmin,
		FullName:     strings.TrimSpace(input.FullName),
		Phone:        input.Phone,
	})
	if err != nil {
		return Principal{}, err
	}
	return Principal{User: created.User, Profile: created.Profile}, nil
}

func (s *UserService) ListUsers(ctx context.Context, actor Principal, q string, limit, offset int) ([]Principal, Page, error) {
	if !actor.User.IsAdmin {
		return nil, Page{}, ErrForbidden
	}
	limit, offset = clampPagination(limit, offset)
	users, total, err := s.Repo.ListUsers(ctx, strings.TrimSpace(q), limit, offset)
	if err != nil {
		return nil, Page{}, err
	}
	out := make([]Principal, 0, len(users))
	for _, u := range users {
		out = append(out, Principal{User: u.User, Profile: u.Profile})
	}
	return out, Page{Limit: limit, Offset: offset, Count: len(out), Total: total}, nil
}

func (s *UserService) UpdateProfile(ctx context.Context, actor Principal, userID uuid.UUID, fullName string, phone *string) (Principal, error) {
	if actor.User.ID != userID && !actor.User.IsAdmin {
		return Principal{}, ErrForbidden
	}
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return Principal{}, ValidationError("full_name is required")
	}
	profile, err := s.Repo.UpdateProfile(ctx, userID, fullName, phone)
	if err != nil {
		return Principal{}, err
	}
	user, err := s.Repo.GetUserByID(ctx, userID)
	if err != nil {
		return Principal{}, err
	}
	return Principal{User: user, Profile: profile}, nil
}

func (s *UserService) UpdateUser(ctx context.Context, actor Principal, userID uuid.UUID, input UpdateUserInput) (Principal, error) {
	if !actor.User.IsAdmin {
		return Principal{}, ErrForbidden
	}

	existing, err := s.Repo.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Principal{}, ErrNotFound
		}
		return Principal{}, err
	}

	fullName := strings.TrimSpace(input.FullName)
	if fullName == "" {
		return Principal{}, ValidationError("full_name is required")
	}

	removesActiveAdmin := existing.IsAdmin && existing.IsActive && (!input.IsAdmin || !input.IsActive)
	if removesActiveAdmin {
		activeAdmins, err := s.Repo.CountActiveAdmins(ctx)
		if err != nil {
			return Principal{}, err
		}
		if activeAdmins <= 1 {
			return Principal{}, ValidationError("must keep at least one active admin")
		}
	}

	updated, err := s.Repo.UpdateUserWithProfile(ctx, userID, repository.UpdateUserParams{
		FullName: fullName,
		Phone:    input.Phone,
		IsAdmin:  input.IsAdmin,
		IsActive: input.IsActive,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return Principal{}, ErrNotFound
		default:
			return Principal{}, err
		}
	}
	return Principal{User: updated.User, Profile: updated.Profile}, nil
}

func (s *UserService) ResetUserPassword(ctx context.Context, actor Principal, userID uuid.UUID, input ResetUserPasswordInput) error {
	if !actor.User.IsAdmin {
		return ErrForbidden
	}
	if len(input.Password) < 8 {
		return ValidationError("password must be at least 8 characters")
	}
	if _, err := s.Repo.GetUserByID(ctx, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return err
	}
	if err := s.Repo.UpdateUserPasswordHash(ctx, userID, hash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *UserService) issueTokens(userID uuid.UUID) (TokenPair, error) {
	now := s.now()
	access, accessExp, err := auth.Issue(s.JWTSecret, userID.String(), auth.AccessToken, s.AccessTokenTTL, now)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, _, err := auth.Issue(s.JWTSecret, userID.String(), auth.RefreshToken, s.RefreshTokenTTL, now)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:     access,
		RefreshToken:    refresh,
		AccessExpiresAt: accessExp,
	}, nil
}

func clampPagination(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
