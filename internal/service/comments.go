package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

const maxCommentContent = 2000

type Comment struct {
	repository.Comment
	Author    PostAuthor
	Reactions []ReactionSummary
}

type CommentService struct {
	Repo         *repository.Repo
	Storage      *storage.MinIO
	Notification Trigger
}

func (s *CommentService) List(ctx context.Context, viewer Principal, postID uuid.UUID) ([]Comment, error) {
	if _, err := s.Repo.GetPost(ctx, postID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rows, err := s.Repo.ListCommentsByPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	commentIDs := make([]uuid.UUID, len(rows))
	for i, c := range rows {
		commentIDs[i] = c.Comment.ID
	}
	summaries, err := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, repository.ReactionTargetComment, commentIDs)
	if err != nil {
		return nil, err
	}

	out := make([]Comment, len(rows))
	for i, c := range rows {
		out[i] = Comment{
			Comment:   c.Comment,
			Author:    authorView(c.Author, c.Profile, s.Storage),
			Reactions: toReactionSummaries(summaries[c.Comment.ID]),
		}
	}
	return out, nil
}

func (s *CommentService) Create(ctx context.Context, viewer Principal, postID uuid.UUID, content string) (Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Comment{}, ValidationError("content is required")
	}
	if len(content) > maxCommentContent {
		return Comment{}, ValidationError("content too long")
	}
	post, err := s.Repo.GetPost(ctx, postID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, err
	}
	created, err := s.Repo.CreateComment(ctx, postID, viewer.User.ID, content)
	if err != nil {
		return Comment{}, err
	}
	if s.Notification != nil {
		s.Notification.OnPostComment(ctx, post, created, viewer)
	}
	return Comment{
		Comment:   created,
		Author:    authorView(viewer.User, viewer.Profile, s.Storage),
		Reactions: []ReactionSummary{},
	}, nil
}

func (s *CommentService) Delete(ctx context.Context, viewer Principal, commentID uuid.UUID) error {
	existing, err := s.Repo.GetComment(ctx, commentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.AuthorUserID != viewer.User.ID && !viewer.User.IsAdmin {
		return ErrForbidden
	}
	return s.Repo.DeleteComment(ctx, commentID)
}

func authorView(u repository.User, p repository.Profile, store *storage.MinIO) PostAuthor {
	avatar := ""
	if p.AvatarObjectKey != nil {
		avatar = store.BuildPublicURL(*p.AvatarObjectKey)
	}
	return PostAuthor{ID: u.ID, FullName: p.FullName, AvatarURL: avatar}
}

func toReactionSummaries(aggs []repository.ReactionAggregate) []ReactionSummary {
	out := make([]ReactionSummary, len(aggs))
	for i, a := range aggs {
		out[i] = ReactionSummary{Emoji: a.Emoji, Count: a.Count, ReactedByMe: a.ReactedByMe}
	}
	return out
}
