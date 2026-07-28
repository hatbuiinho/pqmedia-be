package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type PostOverviewStats struct {
	TotalPosts       int
	PendingPosts     int
	ApprovedPosts    int
	RejectedPosts    int
	PublishedPosts   int
	UnpublishedPosts int
}

type MemberActivitySortBy string

const (
	MemberActivitySortDisplayName     MemberActivitySortBy = "display_name"
	MemberActivitySortPostsCreated    MemberActivitySortBy = "posts_created"
	MemberActivitySortPostsPending    MemberActivitySortBy = "posts_pending"
	MemberActivitySortPostsApproved   MemberActivitySortBy = "posts_approved"
	MemberActivitySortPostsRejected   MemberActivitySortBy = "posts_rejected"
	MemberActivitySortPostsPublished  MemberActivitySortBy = "posts_published"
	MemberActivitySortCommentsCreated MemberActivitySortBy = "comments_created"
	MemberActivitySortReactionsMade   MemberActivitySortBy = "reactions_created"
	MemberActivitySortLastActiveAt    MemberActivitySortBy = "last_active_at"
)

type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

type MemberActivityFilter struct {
	From    *time.Time
	To      *time.Time
	SortBy  MemberActivitySortBy
	SortDir SortDirection
	Limit   int
	Offset  int
}

type MemberActivityRow struct {
	User             User
	Profile          Profile
	PostsCreated     int
	PostsPending     int
	PostsApproved    int
	PostsRejected    int
	PostsPublished   int
	CommentsCreated  int
	ReactionsCreated int
	LastActiveAt     *time.Time
}

func (r *Repo) GetPostOverviewStats(ctx context.Context, from, to *time.Time) (PostOverviewStats, error) {
	args := make([]any, 0, 2)
	where := buildStatsTimeWhere("p.created_at", from, to, &args)

	var stats PostOverviewStats
	err := r.pool.QueryRow(ctx, `
		WITH filtered_posts AS (
			SELECT p.id, p.approval_status
			FROM posts p
			WHERE p.deleted_at IS NULL`+where+`
		),
		published_post_ids AS (
			SELECT DISTINCT pp.post_id
			FROM post_publications pp
			JOIN filtered_posts fp ON fp.id = pp.post_id
		)
		SELECT
			COUNT(*)::int AS total_posts,
			COUNT(*) FILTER (WHERE fp.approval_status = 'pending')::int AS pending_posts,
			COUNT(*) FILTER (WHERE fp.approval_status = 'approved')::int AS approved_posts,
			COUNT(*) FILTER (WHERE fp.approval_status = 'rejected')::int AS rejected_posts,
			COUNT(*) FILTER (WHERE ppi.post_id IS NOT NULL)::int AS published_posts,
			COUNT(*) FILTER (WHERE ppi.post_id IS NULL)::int AS unpublished_posts
		FROM filtered_posts fp
		LEFT JOIN published_post_ids ppi ON ppi.post_id = fp.id
	`, args...).Scan(
		&stats.TotalPosts,
		&stats.PendingPosts,
		&stats.ApprovedPosts,
		&stats.RejectedPosts,
		&stats.PublishedPosts,
		&stats.UnpublishedPosts,
	)
	if err != nil {
		return PostOverviewStats{}, fmt.Errorf("get post overview stats: %w", err)
	}
	return stats, nil
}

func (r *Repo) ListMemberActivity(ctx context.Context, filter MemberActivityFilter) ([]MemberActivityRow, int, error) {
	orderBy, err := memberActivityOrderBy(filter.SortBy, filter.SortDir)
	if err != nil {
		return nil, 0, err
	}

	args := make([]any, 0, 8)
	postRange := buildStatsTimeWhere("p.created_at", filter.From, filter.To, &args)
	commentRange := buildStatsTimeWhere("c.created_at", filter.From, filter.To, &args)
	reactionRange := buildStatsTimeWhere("r.updated_at", filter.From, filter.To, &args)

	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
	`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count member activity rows: %w", err)
	}

	args = append(args, filter.Limit, filter.Offset)
	limitArg := fmt.Sprintf("$%d", len(args)-1)
	offsetArg := fmt.Sprintf("$%d", len(args))

	rows, err := r.pool.Query(ctx, `
		WITH post_stats AS (
			SELECT
				p.author_user_id AS user_id,
				COUNT(*)::int AS posts_created,
				COUNT(*) FILTER (WHERE p.approval_status = 'pending')::int AS posts_pending,
				COUNT(*) FILTER (WHERE p.approval_status = 'approved')::int AS posts_approved,
				COUNT(*) FILTER (WHERE p.approval_status = 'rejected')::int AS posts_rejected,
				COUNT(*) FILTER (
					WHERE EXISTS (
						SELECT 1
						FROM post_publications pp
						WHERE pp.post_id = p.id
					)
				)::int AS posts_published,
				MAX(p.created_at) AS last_post_at
			FROM posts p
			WHERE p.deleted_at IS NULL`+postRange+`
			GROUP BY p.author_user_id
		),
		comment_stats AS (
			SELECT
				c.author_user_id AS user_id,
				COUNT(*)::int AS comments_created,
				MAX(c.created_at) AS last_comment_at
			FROM post_comments c
			WHERE 1 = 1`+commentRange+`
			GROUP BY c.author_user_id
		),
		reaction_stats AS (
			SELECT
				r.user_id,
				COUNT(*)::int AS reactions_created,
				MAX(r.updated_at) AS last_reaction_at
			FROM reactions r
			WHERE 1 = 1`+reactionRange+`
			GROUP BY r.user_id
		),
		member_rows AS (
			SELECT
				u.id,
				u.email,
				u.password_hash,
				u.is_admin,
				u.can_manage_publications,
				u.can_review_posts,
				u.is_active,
				u.created_at AS user_created_at,
				u.updated_at AS user_updated_at,
				p.user_id,
				p.full_name,
				p.dharma_name,
				p.birth_year,
				p.phone,
				p.ctn,
				p.avatar_bucket,
				p.avatar_object_key,
				p.updated_at AS profile_updated_at,
				COALESCE(NULLIF(BTRIM(p.dharma_name), ''), NULLIF(BTRIM(p.full_name), ''), u.email) AS display_name,
				COALESCE(ps.posts_created, 0) AS posts_created,
				COALESCE(ps.posts_pending, 0) AS posts_pending,
				COALESCE(ps.posts_approved, 0) AS posts_approved,
				COALESCE(ps.posts_rejected, 0) AS posts_rejected,
				COALESCE(ps.posts_published, 0) AS posts_published,
				COALESCE(cs.comments_created, 0) AS comments_created,
				COALESCE(rs.reactions_created, 0) AS reactions_created,
				NULLIF(
					GREATEST(
						COALESCE(ps.last_post_at, '-infinity'::timestamptz),
						COALESCE(cs.last_comment_at, '-infinity'::timestamptz),
						COALESCE(rs.last_reaction_at, '-infinity'::timestamptz)
					),
					'-infinity'::timestamptz
				) AS last_active_at
			FROM users u
			JOIN user_profiles p ON p.user_id = u.id
			LEFT JOIN post_stats ps ON ps.user_id = u.id
			LEFT JOIN comment_stats cs ON cs.user_id = u.id
			LEFT JOIN reaction_stats rs ON rs.user_id = u.id
		)
		SELECT
			id, email, password_hash, is_admin, can_manage_publications, can_review_posts, is_active, user_created_at, user_updated_at,
			user_id, full_name, dharma_name, birth_year, phone, ctn, avatar_bucket, avatar_object_key, profile_updated_at,
			posts_created, posts_pending, posts_approved, posts_rejected, posts_published, comments_created, reactions_created, last_active_at
		FROM member_rows
		ORDER BY `+orderBy+`
		LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list member activity: %w", err)
	}
	defer rows.Close()

	out := make([]MemberActivityRow, 0, filter.Limit)
	for rows.Next() {
		var row MemberActivityRow
		if err := rows.Scan(
			&row.User.ID,
			&row.User.Email,
			&row.User.PasswordHash,
			&row.User.IsAdmin,
			&row.User.CanManagePublications,
			&row.User.CanReviewPosts,
			&row.User.IsActive,
			&row.User.CreatedAt,
			&row.User.UpdatedAt,
			&row.Profile.UserID,
			&row.Profile.FullName,
			&row.Profile.DharmaName,
			&row.Profile.BirthYear,
			&row.Profile.Phone,
			&row.Profile.CTN,
			&row.Profile.AvatarBucket,
			&row.Profile.AvatarObjectKey,
			&row.Profile.UpdatedAt,
			&row.PostsCreated,
			&row.PostsPending,
			&row.PostsApproved,
			&row.PostsRejected,
			&row.PostsPublished,
			&row.CommentsCreated,
			&row.ReactionsCreated,
			&row.LastActiveAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan member activity: %w", err)
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

func buildStatsTimeWhere(column string, from, to *time.Time, args *[]any) string {
	parts := make([]string, 0, 2)
	if from != nil {
		*args = append(*args, *from)
		parts = append(parts, column+" >= "+fmt.Sprintf("$%d", len(*args)))
	}
	if to != nil {
		*args = append(*args, *to)
		parts = append(parts, column+" < "+fmt.Sprintf("$%d", len(*args)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " AND " + strings.Join(parts, " AND ")
}

func memberActivityOrderBy(sortBy MemberActivitySortBy, sortDir SortDirection) (string, error) {
	column := "posts_created"
	switch sortBy {
	case "", MemberActivitySortPostsCreated:
		column = "posts_created"
	case MemberActivitySortDisplayName:
		column = "display_name"
	case MemberActivitySortPostsPending:
		column = "posts_pending"
	case MemberActivitySortPostsApproved:
		column = "posts_approved"
	case MemberActivitySortPostsRejected:
		column = "posts_rejected"
	case MemberActivitySortPostsPublished:
		column = "posts_published"
	case MemberActivitySortCommentsCreated:
		column = "comments_created"
	case MemberActivitySortReactionsMade:
		column = "reactions_created"
	case MemberActivitySortLastActiveAt:
		column = "last_active_at"
	default:
		return "", fmt.Errorf("invalid member activity sort field: %s", sortBy)
	}

	direction := "DESC"
	if sortDir == SortAsc {
		direction = "ASC"
	}

	if column == "display_name" {
		return fmt.Sprintf("%s %s, id ASC", column, direction), nil
	}
	if column == "last_active_at" {
		nulls := "NULLS LAST"
		if direction == "ASC" {
			nulls = "NULLS FIRST"
		}
		return fmt.Sprintf("%s %s %s, display_name ASC, id ASC", column, direction, nulls), nil
	}
	return fmt.Sprintf("%s %s, display_name ASC, id ASC", column, direction), nil
}
