package service

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"pqmedia/be/internal/repository"
)

var platformKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Platform struct {
	Key       string
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
}

type CreatePlatformInput struct {
	Key       string
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
}

type UpdatePlatformInput struct {
	Label     string
	Icon      string
	Tone      string
	SortOrder int
	IsActive  bool
}

type PlatformService struct {
	Repo *repository.Repo
}

func (s *PlatformService) ListPlatforms(ctx context.Context, includeInactive bool) ([]Platform, error) {
	items, err := s.Repo.ListPlatforms(ctx, includeInactive)
	if err != nil {
		return nil, err
	}
	out := make([]Platform, len(items))
	for i, item := range items {
		out[i] = toPlatform(item)
	}
	return out, nil
}

func (s *PlatformService) CreatePlatform(ctx context.Context, actor Principal, input CreatePlatformInput) (Platform, error) {
	if !actor.User.IsAdmin {
		return Platform{}, ErrForbidden
	}
	key, params, err := validateCreatePlatformInput(input)
	if err != nil {
		return Platform{}, err
	}
	if _, err := s.Repo.GetPlatform(ctx, key); err == nil {
		return Platform{}, NewError(http.StatusConflict, "platform_exists", "nền tảng này đã tồn tại")
	} else if !errors.Is(err, repository.ErrNotFound) {
		return Platform{}, err
	}
	created, err := s.Repo.CreatePlatform(ctx, params)
	if err != nil {
		return Platform{}, err
	}
	return toPlatform(created), nil
}

func (s *PlatformService) UpdatePlatform(ctx context.Context, actor Principal, key string, input UpdatePlatformInput) (Platform, error) {
	if !actor.User.IsAdmin {
		return Platform{}, ErrForbidden
	}
	key = repository.NormalizePlatformKey(key)
	if key == "" {
		return Platform{}, ValidationError("platform key is required")
	}
	params, err := validateUpdatePlatformInput(input)
	if err != nil {
		return Platform{}, err
	}
	updated, err := s.Repo.UpdatePlatform(ctx, key, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Platform{}, ErrNotFound
		}
		return Platform{}, err
	}
	return toPlatform(updated), nil
}

func (s *PlatformService) DeletePlatform(ctx context.Context, actor Principal, key string) error {
	if !actor.User.IsAdmin {
		return ErrForbidden
	}
	key = repository.NormalizePlatformKey(key)
	if key == "" {
		return ValidationError("platform key is required")
	}
	if err := s.Repo.DeletePlatform(ctx, key); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return ErrNotFound
		case errors.Is(err, repository.ErrConflict):
			return NewError(http.StatusConflict, "platform_in_use", "nền tảng này đã được dùng trong trạng thái đăng bài, không thể xóa")
		default:
			return err
		}
	}
	return nil
}

func validateCreatePlatformInput(input CreatePlatformInput) (string, repository.CreatePlatformParams, error) {
	key := repository.NormalizePlatformKey(input.Key)
	if key == "" {
		return "", repository.CreatePlatformParams{}, ValidationError("platform key is required")
	}
	if !platformKeyPattern.MatchString(key) {
		return "", repository.CreatePlatformParams{}, ValidationError("platform key must use lowercase letters, numbers, dash or underscore")
	}
	params, err := validateUpdatePlatformInput(UpdatePlatformInput{
		Label:     input.Label,
		Icon:      input.Icon,
		Tone:      input.Tone,
		SortOrder: input.SortOrder,
		IsActive:  input.IsActive,
	})
	if err != nil {
		return "", repository.CreatePlatformParams{}, err
	}
	return key, repository.CreatePlatformParams{
		Key:       key,
		Label:     params.Label,
		Icon:      params.Icon,
		Tone:      params.Tone,
		SortOrder: params.SortOrder,
		IsActive:  params.IsActive,
	}, nil
}

func validateUpdatePlatformInput(input UpdatePlatformInput) (repository.UpdatePlatformParams, error) {
	label := strings.TrimSpace(input.Label)
	icon := strings.TrimSpace(input.Icon)
	tone := strings.TrimSpace(input.Tone)
	if label == "" {
		return repository.UpdatePlatformParams{}, ValidationError("label is required")
	}
	if icon == "" {
		return repository.UpdatePlatformParams{}, ValidationError("icon is required")
	}
	if tone == "" {
		return repository.UpdatePlatformParams{}, ValidationError("tone is required")
	}
	return repository.UpdatePlatformParams{
		Label:     label,
		Icon:      icon,
		Tone:      tone,
		SortOrder: input.SortOrder,
		IsActive:  input.IsActive,
	}, nil
}

func toPlatform(p repository.Platform) Platform {
	return Platform{
		Key:       p.Key,
		Label:     p.Label,
		Icon:      p.Icon,
		Tone:      p.Tone,
		SortOrder: p.SortOrder,
		IsActive:  p.IsActive,
	}
}
