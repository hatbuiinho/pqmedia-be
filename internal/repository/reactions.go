package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ReactionTargetType string

const (
	ReactionTargetPost    ReactionTargetType = "post"
	ReactionTargetComment ReactionTargetType = "comment"
)

func (t ReactionTargetType) Valid() bool {
	return t == ReactionTargetPost || t == ReactionTargetComment
}

type ReactionAggregate struct {
	TargetID    uuid.UUID
	Emoji       string
	Count       int
	ReactedByMe bool
}

// ToggleReaction now acts as Set/Increment Reaction.
// If emoji == "", it deletes the reaction for this user and target.
// Otherwise, it increments the count by delta if emoji is the same, or changes the emoji and resets count to delta.
func (r *Repo) ToggleReaction(ctx context.Context, userID uuid.UUID, target ReactionTargetType, targetID uuid.UUID, emoji string, delta int) (bool, error) {
	if emoji == "" {
		_, err := r.pool.Exec(ctx, `DELETE FROM reactions WHERE target_type = $1 AND target_id = $2 AND user_id = $3`, target, targetID, userID)
		return false, err
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO reactions (target_type, target_id, user_id, emoji, count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (target_type, target_id, user_id) DO UPDATE SET 
			count = CASE WHEN reactions.emoji = $4 THEN reactions.count + $5 ELSE $5 END,
			emoji = $4,
			updated_at = NOW()
	`, target, targetID, userID, emoji, delta)
	if err != nil {
		return false, fmt.Errorf("upsert reaction: %w", err)
	}
	return true, nil
}

// ReactionSummariesByTargets returns aggregates grouped by target_id.
// `reacted_by_me` is true when viewerID has reacted with that emoji.
func (r *Repo) ReactionSummariesByTargets(ctx context.Context, viewerID uuid.UUID, target ReactionTargetType, targetIDs []uuid.UUID) (map[uuid.UUID][]ReactionAggregate, error) {
	if len(targetIDs) == 0 {
		return map[uuid.UUID][]ReactionAggregate{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT target_id, emoji, SUM(count)::int,
		       bool_or(user_id = $1) AS reacted_by_me
		FROM reactions
		WHERE target_type = $2 AND target_id = ANY($3)
		GROUP BY target_id, emoji
		ORDER BY target_id, SUM(count)::int DESC, emoji
	`, viewerID, target, targetIDs)
	if err != nil {
		return nil, fmt.Errorf("list reactions: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]ReactionAggregate, len(targetIDs))
	for rows.Next() {
		var a ReactionAggregate
		if err := rows.Scan(&a.TargetID, &a.Emoji, &a.Count, &a.ReactedByMe); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		out[a.TargetID] = append(out[a.TargetID], a)
	}
	return out, rows.Err()
}

type ReactionDetail struct {
	Emoji           string
	Count           int
	UserID          uuid.UUID
	FullName        string
	AvatarBucket    *string
	AvatarObjectKey *string
	CreatedAt       time.Time
}

func (r *Repo) GetReactionDetails(ctx context.Context, target ReactionTargetType, targetID uuid.UUID) ([]ReactionDetail, error) {
	// We map TargetType string to ReactionTargetType here or rely on the caller checking it.
	rows, err := r.pool.Query(ctx, `
		SELECT r.emoji, r.count, r.user_id, p.full_name, p.avatar_bucket, p.avatar_object_key, r.created_at
		FROM reactions r
		JOIN user_profiles p ON r.user_id = p.user_id
		WHERE r.target_type = $1 AND r.target_id = $2
		ORDER BY r.created_at DESC
	`, target, targetID)
	if err != nil {
		return nil, fmt.Errorf("query reaction details: %w", err)
	}
	defer rows.Close()

	var details []ReactionDetail
	for rows.Next() {
		var d ReactionDetail
		if err := rows.Scan(&d.Emoji, &d.Count, &d.UserID, &d.FullName, &d.AvatarBucket, &d.AvatarObjectKey, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reaction detail: %w", err)
		}
		details = append(details, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reaction details: %w", err)
	}
	if details == nil {
		details = []ReactionDetail{}
	}
	return details, nil
}
