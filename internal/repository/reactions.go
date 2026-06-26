package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// ToggleReaction inserts the reaction if absent, or deletes it if present (idempotent).
// Returns true if the reaction is now present after the call.
func (r *Repo) ToggleReaction(ctx context.Context, userID uuid.UUID, target ReactionTargetType, targetID uuid.UUID, emoji string) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM reactions
		WHERE target_type = $1 AND target_id = $2 AND user_id = $3 AND emoji = $4
	`, target, targetID, userID, emoji).Scan(&id)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
			INSERT INTO reactions (target_type, target_id, user_id, emoji)
			VALUES ($1, $2, $3, $4)
		`, target, targetID, userID, emoji); err != nil {
			return false, fmt.Errorf("insert reaction: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit insert: %w", err)
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("query reaction: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM reactions WHERE id = $1`, id); err != nil {
		return false, fmt.Errorf("delete reaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit delete: %w", err)
	}
	return false, nil
}

// ReactionSummariesByTargets returns aggregates grouped by target_id.
// `reacted_by_me` is true when viewerID has reacted with that emoji.
func (r *Repo) ReactionSummariesByTargets(ctx context.Context, viewerID uuid.UUID, target ReactionTargetType, targetIDs []uuid.UUID) (map[uuid.UUID][]ReactionAggregate, error) {
	if len(targetIDs) == 0 {
		return map[uuid.UUID][]ReactionAggregate{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT target_id, emoji, COUNT(*)::int,
		       bool_or(user_id = $1) AS reacted_by_me
		FROM reactions
		WHERE target_type = $2 AND target_id = ANY($3)
		GROUP BY target_id, emoji
		ORDER BY target_id, COUNT(*) DESC, emoji
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
