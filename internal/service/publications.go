package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

type PublicationService struct {
	Repo    *repository.Repo
	Storage *storage.MinIO
}

type UpsertPublicationInput struct {
	ExternalURL *string
	PublishedAt *time.Time
	Note        *string
}

func (s *PublicationService) Upsert(ctx context.Context, viewer Principal, postID uuid.UUID, platform string, input UpsertPublicationInput) (Publication, error) {
	if !repository.IsValidPlatform(platform) {
		return Publication{}, ValidationError("unsupported platform")
	}
	if _, err := s.Repo.GetPost(ctx, postID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Publication{}, ErrNotFound
		}
		return Publication{}, err
	}
	publishedAt := time.Now()
	if input.PublishedAt != nil {
		publishedAt = *input.PublishedAt
	}
	created, err := s.Repo.UpsertPublication(ctx, postID, platform, input.ExternalURL, input.Note, publishedAt, viewer.User.ID)
	if err != nil {
		return Publication{}, err
	}
	return toPublication(created, s.Storage.BuildPublicURL), nil
}

func (s *PublicationService) Delete(ctx context.Context, _ Principal, postID uuid.UUID, platform string) error {
	if !repository.IsValidPlatform(platform) {
		return ValidationError("unsupported platform")
	}
	if err := s.Repo.DeletePublication(ctx, postID, platform); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *PublicationService) ListForPost(ctx context.Context, postID uuid.UUID) ([]Publication, error) {
	if _, err := s.Repo.GetPost(ctx, postID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	grouped, err := s.Repo.ListPublicationsByPosts(ctx, []uuid.UUID{postID})
	if err != nil {
		return nil, err
	}
	return toPublications(grouped[postID], s.Storage.BuildPublicURL), nil
}

func toPublications(in []repository.Publication, buildPublicURL func(string) string) []Publication {
	out := make([]Publication, len(in))
	for i, p := range in {
		out[i] = toPublication(p, buildPublicURL)
	}
	return out
}

func toPublication(p repository.Publication, buildPublicURL func(string) string) Publication {
	avatarURL := ""
	if p.PublishedByAvatar != nil {
		avatarURL = buildPublicURL(*p.PublishedByAvatar)
	}
	return Publication{
		ID:          p.ID,
		PostID:      p.PostID,
		Platform:    p.Platform,
		ExternalURL: p.ExternalURL,
		PublishedAt: p.PublishedAt,
		PublishedBy: PostAuthor{
			ID:        p.PublishedByUserID,
			FullName:  p.PublishedByName,
			AvatarURL: avatarURL,
		},
		Note: p.Note,
	}
}
