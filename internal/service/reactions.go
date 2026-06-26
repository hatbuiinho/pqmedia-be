package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"pqmedia/be/internal/repository"
)

type ReactionService struct {
	Repo         *repository.Repo
	Notification Trigger
}

// Toggle inserts or removes a reaction. Returns the resulting active flag.
func (s *ReactionService) Toggle(ctx context.Context, viewer Principal, target repository.ReactionTargetType, targetID uuid.UUID, emoji string) (bool, error) {
	if !target.Valid() {
		return false, ValidationError("target_type must be post or comment")
	}
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return false, ValidationError("emoji is required")
	}
	if !s.targetExists(ctx, target, targetID) {
		return false, ErrNotFound
	}
	active, err := s.Repo.ToggleReaction(ctx, viewer.User.ID, target, targetID, emoji)
	if err != nil {
		return false, err
	}
	if active && s.Notification != nil {
		s.Notification.OnReaction(ctx, target, targetID, emoji, viewer)
	}
	return active, nil
}

// Summaries returns the same shape used inline in Post/Comment, but on demand.
func (s *ReactionService) Summaries(ctx context.Context, viewer Principal, target repository.ReactionTargetType, targetID uuid.UUID) ([]ReactionSummary, error) {
	if !target.Valid() {
		return nil, ValidationError("target_type must be post or comment")
	}
	if !s.targetExists(ctx, target, targetID) {
		return nil, ErrNotFound
	}
	aggs, err := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, target, []uuid.UUID{targetID})
	if err != nil {
		return nil, err
	}
	return toReactionSummaries(aggs[targetID]), nil
}

func (s *ReactionService) targetExists(ctx context.Context, target repository.ReactionTargetType, id uuid.UUID) bool {
	switch target {
	case repository.ReactionTargetPost:
		_, err := s.Repo.GetPost(ctx, id)
		return err == nil
	case repository.ReactionTargetComment:
		_, err := s.Repo.GetComment(ctx, id)
		return err == nil
	default:
		return false
	}
}
