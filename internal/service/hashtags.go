package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"pqmedia/be/internal/repository"
)

var hashtagNamePattern = regexp.MustCompile(`^[\p{L}\d_]+$`)

type Hashtag struct {
	ID                   string
	Name                 string
	PostCount            int
	UnpublishedPostCount int
	CreatedAt            time.Time
}

type HashtagService struct {
	Repo *repository.Repo
}

func (s *HashtagService) List(ctx context.Context, _ Principal, q string, limit int) ([]Hashtag, error) {
	items, err := s.Repo.ListHashtagSummaries(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Hashtag, len(items))
	for i, item := range items {
		out[i] = toHashtag(item)
	}
	return out, nil
}

func (s *HashtagService) Create(ctx context.Context, actor Principal, rawName string) (Hashtag, error) {
	if !actor.User.IsAdmin {
		return Hashtag{}, ErrForbidden
	}
	name, err := normalizeHashtagName(rawName)
	if err != nil {
		return Hashtag{}, err
	}
	item, err := s.Repo.CreateHashtag(ctx, name)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Hashtag{}, ErrConflict
		}
		return Hashtag{}, err
	}
	return toHashtag(item), nil
}

func (s *HashtagService) Update(ctx context.Context, actor Principal, currentRawName, nextRawName string) (Hashtag, error) {
	if !actor.User.IsAdmin {
		return Hashtag{}, ErrForbidden
	}
	currentName, err := normalizeHashtagName(currentRawName)
	if err != nil {
		return Hashtag{}, err
	}
	nextName, err := normalizeHashtagName(nextRawName)
	if err != nil {
		return Hashtag{}, err
	}
	item, err := s.Repo.RenameHashtag(ctx, currentName, nextName)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return Hashtag{}, ErrNotFound
		case errors.Is(err, repository.ErrConflict):
			return Hashtag{}, ErrConflict
		default:
			return Hashtag{}, err
		}
	}
	return toHashtag(item), nil
}

func (s *HashtagService) Delete(ctx context.Context, actor Principal, rawName string) error {
	if !actor.User.IsAdmin {
		return ErrForbidden
	}
	name, err := normalizeHashtagName(rawName)
	if err != nil {
		return err
	}
	if err := s.Repo.DeleteHashtag(ctx, name); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func normalizeHashtagName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	name = strings.TrimPrefix(name, "#")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ValidationError("hashtag name is required")
	}
	if !hashtagNamePattern.MatchString(name) {
		return "", ValidationError("hashtag name must contain only letters, numbers or underscore")
	}
	if len(name) > 64 {
		return "", ValidationError("hashtag name too long")
	}
	return name, nil
}

func toHashtag(item repository.Hashtag) Hashtag {
	return Hashtag{
		ID:                   item.ID,
		Name:                 item.Name,
		PostCount:            item.PostCount,
		UnpublishedPostCount: item.UnpublishedPostCount,
		CreatedAt:            item.CreatedAt,
	}
}
