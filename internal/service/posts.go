package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

const maxPostContent = 10000

// PostAuthor is a tiny projection of a User+Profile pair, embedded in feed responses.
type PostAuthor struct {
	ID        uuid.UUID
	FullName  string
	AvatarURL string
}

type PostAttachment struct {
	repository.PostAttachment
	URL string
}

type Post struct {
	repository.Post
	Author       PostAuthor
	Attachments  []PostAttachment
	Hashtags     []string
	CommentCount int
	// Reactions + Publications populated by Phase 4/5 services; left empty here.
	Reactions    []ReactionSummary
	Publications []Publication
}

// ReactionSummary / Publication declared here as placeholders so the Post DTO
// is stable across phases. Phase 4/5 services will populate the actual data.
type ReactionSummary struct {
	Emoji       string
	Count       int
	ReactedByMe bool
}

type Publication struct {
	ID          uuid.UUID
	PostID      uuid.UUID
	Platform    string
	ExternalURL *string
	PublishedAt time.Time
	PublishedBy PostAuthor
	Note        *string
}

type CreatePostInput struct {
	Content     string
	Attachments []repository.PostAttachmentInput
	Hashtags    []string
}

type UpdatePostInput struct {
	Content     *string
	Attachments *[]repository.PostAttachmentInput
	Hashtags    *[]string
}

type PostService struct {
	Repo    *repository.Repo
	Storage *storage.MinIO
}

func (s *PostService) ListFeed(ctx context.Context, viewer Principal, filter repository.FeedFilter) ([]Post, Page, error) {
	filter.Limit, filter.Offset = clampPagination(filter.Limit, filter.Offset)

	posts, users, profiles, total, err := s.Repo.ListFeed(ctx, filter)
	if err != nil {
		return nil, Page{}, err
	}
	if len(posts) == 0 {
		return []Post{}, Page{Limit: filter.Limit, Offset: filter.Offset, Total: total}, nil
	}

	postIDs := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}
	attachments, err := s.Repo.ListAttachmentsByPosts(ctx, postIDs)
	if err != nil {
		return nil, Page{}, err
	}
	commentCounts, err := s.Repo.CountCommentsByPosts(ctx, postIDs)
	if err != nil {
		return nil, Page{}, err
	}
	reactions, err := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, repository.ReactionTargetPost, postIDs)
	if err != nil {
		return nil, Page{}, err
	}
	publications, err := s.Repo.ListPublicationsByPosts(ctx, postIDs)
	if err != nil {
		return nil, Page{}, err
	}
	hashtags, err := s.Repo.ListHashtagsByPosts(ctx, postIDs)
	if err != nil {
		return nil, Page{}, err
	}

	out := make([]Post, len(posts))
	for i, p := range posts {
		composed := s.composePost(p, users[i], profiles[i], attachments[p.ID], commentCounts[p.ID], hashtags[p.ID])
		composed.Reactions = toReactionSummaries(reactions[p.ID])
		composed.Publications = toPublications(publications[p.ID], s.Storage.BuildPublicURL)
		out[i] = composed
	}
	return out, Page{Limit: filter.Limit, Offset: filter.Offset, Count: len(out), Total: total}, nil
}

func (s *PostService) GetPost(ctx context.Context, viewer Principal, id uuid.UUID) (Post, error) {
	post, err := s.Repo.GetPost(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	author, err := s.Repo.GetUserByID(ctx, post.AuthorUserID)
	if err != nil {
		return Post{}, err
	}
	profile, err := s.Repo.GetProfile(ctx, post.AuthorUserID)
	if err != nil {
		return Post{}, err
	}
	attachments, err := s.Repo.ListAttachmentsByPosts(ctx, []uuid.UUID{post.ID})
	if err != nil {
		return Post{}, err
	}
	counts, err := s.Repo.CountCommentsByPosts(ctx, []uuid.UUID{post.ID})
	if err != nil {
		return Post{}, err
	}
	reactions, err := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, repository.ReactionTargetPost, []uuid.UUID{post.ID})
	if err != nil {
		return Post{}, err
	}
	publications, err := s.Repo.ListPublicationsByPosts(ctx, []uuid.UUID{post.ID})
	if err != nil {
		return Post{}, err
	}
	hashtags, err := s.Repo.ListHashtagsByPosts(ctx, []uuid.UUID{post.ID})
	if err != nil {
		return Post{}, err
	}
	composed := s.composePost(post, author, profile, attachments[post.ID], counts[post.ID], hashtags[post.ID])
	composed.Reactions = toReactionSummaries(reactions[post.ID])
	composed.Publications = toPublications(publications[post.ID], s.Storage.BuildPublicURL)
	return composed, nil
}

func (s *PostService) Create(ctx context.Context, viewer Principal, input CreatePostInput) (Post, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" && len(input.Attachments) == 0 {
		return Post{}, ValidationError("post must have content or attachments")
	}
	if len(content) > maxPostContent {
		return Post{}, ValidationError("content too long")
	}
	post, atts, err := s.Repo.CreatePost(ctx, repository.CreatePostParams{
		AuthorUserID: viewer.User.ID,
		Content:      content,
		Attachments:  input.Attachments,
		Hashtags:     input.Hashtags,
	})
	if err != nil {
		return Post{}, err
	}
	author, _ := s.Repo.GetUserByID(ctx, post.AuthorUserID)
	profile, _ := s.Repo.GetProfile(ctx, post.AuthorUserID)
	return s.composePost(post, author, profile, atts, 0, input.Hashtags), nil
}

func (s *PostService) Update(ctx context.Context, viewer Principal, id uuid.UUID, input UpdatePostInput) (Post, error) {
	existing, err := s.Repo.GetPost(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	if existing.AuthorUserID != viewer.User.ID {
		return Post{}, ErrForbidden
	}

	content := existing.Content
	if input.Content != nil {
		content = strings.TrimSpace(*input.Content)
		if len(content) > maxPostContent {
			return Post{}, ValidationError("content too long")
		}
	}
	var attachments *[]repository.PostAttachmentInput
	if input.Attachments != nil {
		attachments = input.Attachments
	}
	updated, atts, err := s.Repo.UpdatePost(ctx, id, content, attachments, input.Hashtags)
	if err != nil {
		return Post{}, err
	}
	author, _ := s.Repo.GetUserByID(ctx, updated.AuthorUserID)
	profile, _ := s.Repo.GetProfile(ctx, updated.AuthorUserID)
	counts, _ := s.Repo.CountCommentsByPosts(ctx, []uuid.UUID{updated.ID})
	hashtags, _ := s.Repo.ListHashtagsByPosts(ctx, []uuid.UUID{updated.ID})
	reactions, _ := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, repository.ReactionTargetPost, []uuid.UUID{updated.ID})
	publications, _ := s.Repo.ListPublicationsByPosts(ctx, []uuid.UUID{updated.ID})
	composed := s.composePost(updated, author, profile, atts, counts[updated.ID], hashtags[updated.ID])
	composed.Reactions = toReactionSummaries(reactions[updated.ID])
	composed.Publications = toPublications(publications[updated.ID], s.Storage.BuildPublicURL)
	return composed, nil
}

func (s *PostService) Delete(ctx context.Context, viewer Principal, id uuid.UUID) error {
	existing, err := s.Repo.GetPost(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.AuthorUserID != viewer.User.ID && !viewer.User.IsAdmin {
		return ErrForbidden
	}
	return s.Repo.SoftDeletePost(ctx, id)
}

func (s *PostService) composePost(
	post repository.Post,
	author repository.User,
	profile repository.Profile,
	attachments []repository.PostAttachment,
	commentCount int,
	hashtags []string,
) Post {
	if hashtags == nil {
		hashtags = []string{}
	}
	enriched := make([]PostAttachment, len(attachments))
	for i, a := range attachments {
		enriched[i] = PostAttachment{PostAttachment: a, URL: s.attachmentURL(a)}
	}
	return Post{
		Post:         post,
		Author:       s.authorView(author, profile),
		Attachments:  enriched,
		Hashtags:     hashtags,
		CommentCount: commentCount,
		Reactions:    []ReactionSummary{},
		Publications: []Publication{},
	}
}

func (s *PostService) authorView(u repository.User, p repository.Profile) PostAuthor {
	avatar := ""
	if p.AvatarObjectKey != nil {
		avatar = s.Storage.BuildPublicURL(*p.AvatarObjectKey)
	}
	return PostAuthor{ID: u.ID, FullName: p.FullName, AvatarURL: avatar}
}

func (s *PostService) attachmentURL(a repository.PostAttachment) string {
	return s.Storage.BuildPublicURL(a.ObjectKey)
}
