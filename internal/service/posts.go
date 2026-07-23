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
	ID         uuid.UUID
	FullName   string
	DharmaName *string
	AvatarURL  string
}

type PostAttachment struct {
	repository.PostAttachment
	URL       string
	DriveSync *repository.AttachmentDriveSync
}

type Post struct {
	repository.Post
	Author       PostAuthor
	ApprovedBy   *PostAuthor
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
	Content             string
	DriveTargetFolderID *string
	Attachments         []repository.PostAttachmentInput
	Hashtags            []string
}

type UpdatePostInput struct {
	Content             *string
	DriveTargetFolderID *string
	Attachments         *[]repository.PostAttachmentInput
	Hashtags            *[]string
}

type PostService struct {
	Repo         *repository.Repo
	Storage      *storage.MinIO
	DriveSync    *DriveSyncService
	DriveFolders *DriveFolderService
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
	driveSyncs, err := s.Repo.ListAttachmentDriveSyncsByAttachments(ctx, attachmentIDsFromMap(attachments))
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
	approvedBy, err := s.loadPostApprovers(ctx, posts)
	if err != nil {
		return nil, Page{}, err
	}

	out := make([]Post, len(posts))
	for i, p := range posts {
		composed := s.composePost(p, users[i], profiles[i], approvedBy[p.ID], attachments[p.ID], driveSyncs, commentCounts[p.ID], hashtags[p.ID])
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
	driveSyncs, err := s.Repo.ListAttachmentDriveSyncsByAttachments(ctx, attachmentIDsFromMap(attachments))
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
	approvedBy, err := s.loadPostApprovers(ctx, []repository.Post{post})
	if err != nil {
		return Post{}, err
	}
	composed := s.composePost(post, author, profile, approvedBy[post.ID], attachments[post.ID], driveSyncs, counts[post.ID], hashtags[post.ID])
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
	driveTargetFolderID, err := s.normalizeDriveTargetFolderID(ctx, input.DriveTargetFolderID, input.Attachments)
	if err != nil {
		return Post{}, err
	}
	post, atts, err := s.Repo.CreatePost(ctx, repository.CreatePostParams{
		AuthorUserID:        viewer.User.ID,
		Content:             content,
		DriveTargetFolderID: driveTargetFolderID,
		Attachments:         input.Attachments,
		Hashtags:            input.Hashtags,
	})
	if err != nil {
		return Post{}, err
	}
	if s.DriveSync != nil {
		if err := s.DriveSync.QueueAttachments(ctx, atts); err != nil {
			return Post{}, err
		}
	}
	author, _ := s.Repo.GetUserByID(ctx, post.AuthorUserID)
	profile, _ := s.Repo.GetProfile(ctx, post.AuthorUserID)
	driveSyncs, _ := s.Repo.ListAttachmentDriveSyncsByAttachments(ctx, attachmentIDs(atts))
	approvedBy, _ := s.loadPostApprovers(ctx, []repository.Post{post})
	return s.composePost(post, author, profile, approvedBy[post.ID], atts, driveSyncs, 0, input.Hashtags), nil
}

func (s *PostService) Update(ctx context.Context, viewer Principal, id uuid.UUID, input UpdatePostInput) (Post, error) {
	existing, err := s.Repo.GetPost(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	if existing.AuthorUserID != viewer.User.ID && !viewer.User.IsAdmin {
		return Post{}, ErrForbidden
	}

	content := existing.Content
	if input.Content != nil {
		content = strings.TrimSpace(*input.Content)
		if len(content) > maxPostContent {
			return Post{}, ValidationError("content too long")
		}
	}
	existingAttachmentMap, err := s.Repo.ListAttachmentsByPosts(ctx, []uuid.UUID{existing.ID})
	if err != nil {
		return Post{}, err
	}
	finalAttachments := attachmentsToInputs(existingAttachmentMap[existing.ID])
	var attachments *[]repository.PostAttachmentInput
	if input.Attachments != nil {
		attachments = input.Attachments
		finalAttachments = *input.Attachments
	}
	driveTargetFolderID := existing.DriveTargetFolderID
	if input.DriveTargetFolderID != nil || input.Attachments != nil {
		driveTargetFolderID, err = s.normalizeDriveTargetFolderID(ctx, input.DriveTargetFolderID, finalAttachments)
		if err != nil {
			return Post{}, err
		}
	}
	updated, atts, err := s.Repo.UpdatePost(ctx, id, content, driveTargetFolderID, attachments, input.Hashtags)
	if err != nil {
		return Post{}, err
	}
	if s.DriveSync != nil {
		if err := s.DriveSync.QueueAttachments(ctx, atts); err != nil {
			return Post{}, err
		}
	}
	author, _ := s.Repo.GetUserByID(ctx, updated.AuthorUserID)
	profile, _ := s.Repo.GetProfile(ctx, updated.AuthorUserID)
	counts, _ := s.Repo.CountCommentsByPosts(ctx, []uuid.UUID{updated.ID})
	hashtags, _ := s.Repo.ListHashtagsByPosts(ctx, []uuid.UUID{updated.ID})
	reactions, _ := s.Repo.ReactionSummariesByTargets(ctx, viewer.User.ID, repository.ReactionTargetPost, []uuid.UUID{updated.ID})
	publications, _ := s.Repo.ListPublicationsByPosts(ctx, []uuid.UUID{updated.ID})
	driveSyncs, _ := s.Repo.ListAttachmentDriveSyncsByAttachments(ctx, attachmentIDs(atts))
	approvedBy, _ := s.loadPostApprovers(ctx, []repository.Post{updated})
	composed := s.composePost(updated, author, profile, approvedBy[updated.ID], atts, driveSyncs, counts[updated.ID], hashtags[updated.ID])
	composed.Reactions = toReactionSummaries(reactions[updated.ID])
	composed.Publications = toPublications(publications[updated.ID], s.Storage.BuildPublicURL)
	return composed, nil
}

func (s *PostService) UpdateApproval(ctx context.Context, viewer Principal, id uuid.UUID, isApproved bool) (Post, error) {
	if !viewer.User.IsAdmin {
		return Post{}, ErrForbidden
	}

	post, err := s.Repo.UpdatePostApproval(ctx, id, isApproved, &viewer.User.ID)
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
	driveSyncs, err := s.Repo.ListAttachmentDriveSyncsByAttachments(ctx, attachmentIDsFromMap(attachments))
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
	approvedBy, err := s.loadPostApprovers(ctx, []repository.Post{post})
	if err != nil {
		return Post{}, err
	}
	composed := s.composePost(post, author, profile, approvedBy[post.ID], attachments[post.ID], driveSyncs, counts[post.ID], hashtags[post.ID])
	composed.Reactions = toReactionSummaries(reactions[post.ID])
	composed.Publications = toPublications(publications[post.ID], s.Storage.BuildPublicURL)
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

func (s *PostService) normalizeDriveTargetFolderID(ctx context.Context, requested *string, attachments []repository.PostAttachmentInput) (*string, error) {
	if s.DriveSync == nil || !s.DriveSync.Enabled || s.DriveFolders == nil {
		return nil, nil
	}
	return s.DriveFolders.ResolveTargetFolderIDForPost(ctx, requested, attachments)
}

func attachmentsToInputs(items []repository.PostAttachment) []repository.PostAttachmentInput {
	out := make([]repository.PostAttachmentInput, len(items))
	for i, item := range items {
		out[i] = repository.PostAttachmentInput{
			Kind:        item.Kind,
			FileName:    item.FileName,
			ContentType: item.ContentType,
			Bucket:      item.Bucket,
			ObjectKey:   item.ObjectKey,
			SizeBytes:   item.SizeBytes,
			Width:       item.Width,
			Height:      item.Height,
			DurationMs:  item.DurationMs,
			SortOrder:   item.SortOrder,
		}
	}
	return out
}

func (s *PostService) composePost(
	post repository.Post,
	author repository.User,
	profile repository.Profile,
	approvedBy *PostAuthor,
	attachments []repository.PostAttachment,
	driveSyncs map[uuid.UUID]repository.AttachmentDriveSync,
	commentCount int,
	hashtags []string,
) Post {
	if hashtags == nil {
		hashtags = []string{}
	}
	enriched := make([]PostAttachment, len(attachments))
	for i, a := range attachments {
		var driveSync *repository.AttachmentDriveSync
		if sync, ok := driveSyncs[a.ID]; ok {
			syncCopy := sync
			driveSync = &syncCopy
		}
		enriched[i] = PostAttachment{PostAttachment: a, URL: s.attachmentURL(a), DriveSync: driveSync}
	}
	return Post{
		Post:         post,
		Author:       s.authorView(author, profile),
		ApprovedBy:   approvedBy,
		Attachments:  enriched,
		Hashtags:     hashtags,
		CommentCount: commentCount,
		Reactions:    []ReactionSummary{},
		Publications: []Publication{},
	}
}

func attachmentIDsFromMap(items map[uuid.UUID][]repository.PostAttachment) []uuid.UUID {
	var out []uuid.UUID
	for _, attachments := range items {
		for _, attachment := range attachments {
			out = append(out, attachment.ID)
		}
	}
	return out
}

func attachmentIDs(items []repository.PostAttachment) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func (s *PostService) authorView(u repository.User, p repository.Profile) PostAuthor {
	avatar := ""
	if p.AvatarObjectKey != nil {
		avatar = s.Storage.BuildPublicURL(*p.AvatarObjectKey)
	}
	return PostAuthor{ID: u.ID, FullName: p.FullName, DharmaName: p.DharmaName, AvatarURL: avatar}
}

func (s *PostService) loadPostApprovers(ctx context.Context, posts []repository.Post) (map[uuid.UUID]*PostAuthor, error) {
	approverIDs := make([]uuid.UUID, 0, len(posts))
	seen := make(map[uuid.UUID]struct{}, len(posts))
	for _, post := range posts {
		if post.ApprovedByUserID == nil {
			continue
		}
		if _, ok := seen[*post.ApprovedByUserID]; ok {
			continue
		}
		seen[*post.ApprovedByUserID] = struct{}{}
		approverIDs = append(approverIDs, *post.ApprovedByUserID)
	}
	if len(approverIDs) == 0 {
		return map[uuid.UUID]*PostAuthor{}, nil
	}

	users, err := s.Repo.ListUsersByIDs(ctx, approverIDs)
	if err != nil {
		return nil, err
	}
	profiles, err := s.Repo.ListProfilesByUserIDs(ctx, approverIDs)
	if err != nil {
		return nil, err
	}

	userByID := make(map[uuid.UUID]repository.User, len(users))
	for _, user := range users {
		userByID[user.ID] = user
	}
	profileByID := make(map[uuid.UUID]repository.Profile, len(profiles))
	for _, profile := range profiles {
		profileByID[profile.UserID] = profile
	}

	out := make(map[uuid.UUID]*PostAuthor, len(posts))
	for _, post := range posts {
		if post.ApprovedByUserID == nil {
			continue
		}
		user, okUser := userByID[*post.ApprovedByUserID]
		profile, okProfile := profileByID[*post.ApprovedByUserID]
		if !okUser || !okProfile {
			continue
		}
		author := s.authorView(user, profile)
		out[post.ID] = &author
	}
	return out, nil
}

func (s *PostService) attachmentURL(a repository.PostAttachment) string {
	return s.Storage.BuildPublicURL(a.ObjectKey)
}
