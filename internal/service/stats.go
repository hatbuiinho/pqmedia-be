package service

import (
	"context"
	"time"

	"pqmedia/be/internal/repository"
)

type StatsRange struct {
	From *time.Time
	To   *time.Time
}

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
	MemberActivitySortDisplayName      MemberActivitySortBy = "display_name"
	MemberActivitySortPostsCreated     MemberActivitySortBy = "posts_created"
	MemberActivitySortPostsPending     MemberActivitySortBy = "posts_pending"
	MemberActivitySortPostsApproved    MemberActivitySortBy = "posts_approved"
	MemberActivitySortPostsRejected    MemberActivitySortBy = "posts_rejected"
	MemberActivitySortPostsPublished   MemberActivitySortBy = "posts_published"
	MemberActivitySortCommentsCreated  MemberActivitySortBy = "comments_created"
	MemberActivitySortReactionsCreated MemberActivitySortBy = "reactions_created"
	MemberActivitySortLastActiveAt     MemberActivitySortBy = "last_active_at"
)

type SortDirection string

const (
	SortDirectionAsc  SortDirection = "asc"
	SortDirectionDesc SortDirection = "desc"
)

type MemberActivityFilter struct {
	Range   StatsRange
	SortBy  MemberActivitySortBy
	SortDir SortDirection
	Limit   int
	Offset  int
}

type MemberActivityRow struct {
	Principal        Principal
	PostsCreated     int
	PostsPending     int
	PostsApproved    int
	PostsRejected    int
	PostsPublished   int
	CommentsCreated  int
	ReactionsCreated int
	LastActiveAt     *time.Time
}

type StatsService struct {
	Repo *repository.Repo
}

func (s *StatsService) GetPostOverview(ctx context.Context, actor Principal, statsRange StatsRange) (PostOverviewStats, error) {
	if !actor.User.IsAdmin {
		return PostOverviewStats{}, ErrForbidden
	}
	if err := validateStatsRange(statsRange); err != nil {
		return PostOverviewStats{}, err
	}
	stats, err := s.Repo.GetPostOverviewStats(ctx, statsRange.From, statsRange.To)
	if err != nil {
		return PostOverviewStats{}, err
	}
	return PostOverviewStats{
		TotalPosts:       stats.TotalPosts,
		PendingPosts:     stats.PendingPosts,
		ApprovedPosts:    stats.ApprovedPosts,
		RejectedPosts:    stats.RejectedPosts,
		PublishedPosts:   stats.PublishedPosts,
		UnpublishedPosts: stats.UnpublishedPosts,
	}, nil
}

func (s *StatsService) ListMemberActivity(ctx context.Context, actor Principal, filter MemberActivityFilter) ([]MemberActivityRow, Page, error) {
	if !actor.User.IsAdmin {
		return nil, Page{}, ErrForbidden
	}
	if err := validateStatsRange(filter.Range); err != nil {
		return nil, Page{}, err
	}

	limit, offset := clampPagination(filter.Limit, filter.Offset)
	rows, total, err := s.Repo.ListMemberActivity(ctx, repository.MemberActivityFilter{
		From:    filter.Range.From,
		To:      filter.Range.To,
		SortBy:  normalizeMemberActivitySortBy(filter.SortBy),
		SortDir: normalizeSortDirection(filter.SortDir),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return nil, Page{}, err
	}

	out := make([]MemberActivityRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, MemberActivityRow{
			Principal: Principal{
				User:    row.User,
				Profile: row.Profile,
			},
			PostsCreated:     row.PostsCreated,
			PostsPending:     row.PostsPending,
			PostsApproved:    row.PostsApproved,
			PostsRejected:    row.PostsRejected,
			PostsPublished:   row.PostsPublished,
			CommentsCreated:  row.CommentsCreated,
			ReactionsCreated: row.ReactionsCreated,
			LastActiveAt:     row.LastActiveAt,
		})
	}

	return out, Page{
		Limit:  limit,
		Offset: offset,
		Count:  len(out),
		Total:  total,
	}, nil
}

func validateStatsRange(statsRange StatsRange) error {
	if statsRange.From != nil && statsRange.To != nil && !statsRange.From.Before(*statsRange.To) {
		return ValidationError("khoảng thời gian không hợp lệ")
	}
	return nil
}

func normalizeMemberActivitySortBy(sortBy MemberActivitySortBy) repository.MemberActivitySortBy {
	switch sortBy {
	case MemberActivitySortDisplayName:
		return repository.MemberActivitySortDisplayName
	case MemberActivitySortPostsPending:
		return repository.MemberActivitySortPostsPending
	case MemberActivitySortPostsApproved:
		return repository.MemberActivitySortPostsApproved
	case MemberActivitySortPostsRejected:
		return repository.MemberActivitySortPostsRejected
	case MemberActivitySortPostsPublished:
		return repository.MemberActivitySortPostsPublished
	case MemberActivitySortCommentsCreated:
		return repository.MemberActivitySortCommentsCreated
	case MemberActivitySortReactionsCreated:
		return repository.MemberActivitySortReactionsMade
	case MemberActivitySortLastActiveAt:
		return repository.MemberActivitySortLastActiveAt
	case "", MemberActivitySortPostsCreated:
		return repository.MemberActivitySortPostsCreated
	default:
		return repository.MemberActivitySortPostsCreated
	}
}

func normalizeSortDirection(sortDir SortDirection) repository.SortDirection {
	if sortDir == SortDirectionAsc {
		return repository.SortAsc
	}
	return repository.SortDesc
}
