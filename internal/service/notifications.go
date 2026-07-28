package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"pqmedia/be/internal/push"
	"pqmedia/be/internal/repository"
)

// Trigger is the small interface notification-emitting services depend on.
// Comments/Reactions call Trigger to enqueue + push without coupling to the
// full NotificationService API.
type Trigger interface {
	OnPostComment(ctx context.Context, post repository.Post, comment repository.Comment, actor Principal)
	OnReaction(ctx context.Context, target repository.ReactionTargetType, targetID uuid.UUID, emoji string, actor Principal)
}

type PostTrigger interface {
	OnPostCreated(ctx context.Context, post repository.Post, actor Principal)
	OnPostApproved(ctx context.Context, post repository.Post, actor Principal)
	OnPostRejected(ctx context.Context, post repository.Post, actor Principal)
	OnPostResubmitted(ctx context.Context, post repository.Post, actor Principal)
}

type Notification struct {
	repository.Notification
	Actor *PostAuthor
}

type NotificationService struct {
	Repo   *repository.Repo
	Sender *push.Sender
	Logger *slog.Logger
}

func (s *NotificationService) List(ctx context.Context, viewer Principal, unreadOnly bool, limit int) ([]Notification, error) {
	rows, err := s.Repo.ListNotifications(ctx, viewer.User.ID, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, len(rows))
	for i, n := range rows {
		out[i] = Notification{Notification: n}
		if n.ActorUserID != nil {
			actor, err := s.Repo.GetUserByID(ctx, *n.ActorUserID)
			if err == nil {
				profile, _ := s.Repo.GetProfile(ctx, *n.ActorUserID)
				view := PostAuthor{
					ID:         actor.ID,
					FullName:   profile.FullName,
					DharmaName: profile.DharmaName,
					BirthYear:  profile.BirthYear,
					CTN:        profile.CTN,
					Phone:      profile.Phone,
				}
				out[i].Actor = &view
			}
		}
	}
	return out, nil
}

func (s *NotificationService) UnreadCount(ctx context.Context, viewer Principal) (int, error) {
	return s.Repo.CountUnreadNotifications(ctx, viewer.User.ID)
}

func (s *NotificationService) MarkRead(ctx context.Context, viewer Principal, id uuid.UUID) error {
	return s.Repo.MarkNotificationRead(ctx, viewer.User.ID, id)
}

func (s *NotificationService) MarkAllRead(ctx context.Context, viewer Principal) error {
	return s.Repo.MarkAllNotificationsRead(ctx, viewer.User.ID)
}

func (s *NotificationService) UpsertSubscription(ctx context.Context, viewer Principal, params repository.UpsertSubscriptionParams) error {
	params.UserID = viewer.User.ID
	if params.Endpoint == "" || params.P256DH == "" || params.Auth == "" {
		return ValidationError("endpoint, p256dh, auth are required")
	}
	_, err := s.Repo.UpsertPushSubscription(ctx, params)
	return err
}

func (s *NotificationService) DisableSubscription(ctx context.Context, viewer Principal, endpoint string) error {
	if endpoint == "" {
		return ValidationError("endpoint is required")
	}
	return s.Repo.DisablePushSubscription(ctx, viewer.User.ID, endpoint)
}

// ----- Trigger implementation -----

func (s *NotificationService) OnPostComment(ctx context.Context, post repository.Post, comment repository.Comment, actor Principal) {
	if post.AuthorUserID == actor.User.ID {
		return
	}
	postIDCopy := post.ID
	commentIDCopy := comment.ID
	actorIDCopy := actor.User.ID
	title := fmt.Sprintf("%s đã bình luận", preferredProfileName(actor.Profile))
	body := truncate(comment.Content, 140)
	route := fmt.Sprintf("/posts/%s", post.ID)

	payload := map[string]any{
		"post_id":    post.ID.String(),
		"comment_id": comment.ID.String(),
	}
	raw, _ := json.Marshal(payload)
	created, err := s.Repo.CreateNotification(ctx, repository.CreateNotificationParams{
		RecipientUserID: post.AuthorUserID,
		ActorUserID:     &actorIDCopy,
		Kind:            "post_comment",
		PostID:          &postIDCopy,
		CommentID:       &commentIDCopy,
		Title:           title,
		Body:            body,
		RouteURL:        &route,
		Payload:         raw,
	})
	if err != nil {
		s.logger().Error("notification create", slog.String("err", err.Error()))
		return
	}
	s.push(ctx, post.AuthorUserID, push.Payload{
		Title: created.Title,
		Body:  created.Body,
		Tag:   "post-" + post.ID.String(),
		URL:   route,
		Data:  payload,
	})
}

func (s *NotificationService) OnReaction(ctx context.Context, target repository.ReactionTargetType, targetID uuid.UUID, emoji string, actor Principal) {
	recipient, postID, kind, err := s.reactionRecipient(ctx, target, targetID)
	if err != nil {
		s.logger().Error("notification recipient", slog.String("err", err.Error()))
		return
	}
	if recipient == actor.User.ID {
		return
	}
	actorIDCopy := actor.User.ID
	title := fmt.Sprintf("%s đã thả %s", preferredProfileName(actor.Profile), emoji)
	route := fmt.Sprintf("/posts/%s", postID)
	payload := map[string]any{
		"post_id":   postID.String(),
		"target_id": targetID.String(),
		"emoji":     emoji,
	}
	raw, _ := json.Marshal(payload)

	created, err := s.Repo.CreateNotification(ctx, repository.CreateNotificationParams{
		RecipientUserID: recipient,
		ActorUserID:     &actorIDCopy,
		Kind:            kind,
		PostID:          &postID,
		Title:           title,
		Body:            "",
		RouteURL:        &route,
		Payload:         raw,
	})
	if err != nil {
		s.logger().Error("notification create", slog.String("err", err.Error()))
		return
	}
	s.push(ctx, recipient, push.Payload{
		Title: created.Title,
		Body:  created.Body,
		Tag:   "post-" + postID.String(),
		URL:   route,
		Data:  payload,
	})
}

func (s *NotificationService) OnPostCreated(ctx context.Context, post repository.Post, actor Principal) {
	title := fmt.Sprintf("%s đã đăng bài mới", preferredProfileName(actor.Profile))
	s.notifyReviewers(ctx, actor.User.ID, title, notificationBodyFromPost(post), "post_created", post, actor)
}

func (s *NotificationService) OnPostApproved(ctx context.Context, post repository.Post, actor Principal) {
	s.notifyPostAuthor(ctx, post, actor, "post_approved", "Bài viết của bạn đã được duyệt", notificationBodyFromPost(post))
}

func (s *NotificationService) OnPostRejected(ctx context.Context, post repository.Post, actor Principal) {
	s.notifyPostAuthor(ctx, post, actor, "post_rejected", "Bài viết của bạn không được duyệt", notificationBodyFromPost(post))
}

func (s *NotificationService) OnPostResubmitted(ctx context.Context, post repository.Post, actor Principal) {
	title := fmt.Sprintf("%s đã gửi duyệt lại bài viết", preferredProfileName(actor.Profile))
	s.notifyReviewers(ctx, actor.User.ID, title, notificationBodyFromPost(post), "post_resubmitted", post, actor)
}

func (s *NotificationService) push(ctx context.Context, userID uuid.UUID, payload push.Payload) {
	if s.Sender == nil {
		return
	}
	s.Sender.SendToUser(ctx, userID, payload)
}

func (s *NotificationService) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *NotificationService) notifyReviewers(
	ctx context.Context,
	actorUserID uuid.UUID,
	title string,
	body string,
	kind string,
	post repository.Post,
	actor Principal,
) {
	recipientIDs, err := s.Repo.ListActiveReviewerUserIDs(ctx)
	if err != nil {
		s.logger().Error("notification reviewers", slog.String("err", err.Error()))
		return
	}
	for _, recipientUserID := range recipientIDs {
		if recipientUserID == actorUserID {
			continue
		}
		s.notifyOne(ctx, recipientUserID, &actorUserID, kind, post, title, body, actor)
	}
}

func (s *NotificationService) notifyPostAuthor(
	ctx context.Context,
	post repository.Post,
	actor Principal,
	kind string,
	title string,
	body string,
) {
	if post.AuthorUserID == actor.User.ID {
		return
	}
	actorUserID := actor.User.ID
	s.notifyOne(ctx, post.AuthorUserID, &actorUserID, kind, post, title, body, actor)
}

func (s *NotificationService) notifyOne(
	ctx context.Context,
	recipientUserID uuid.UUID,
	actorUserID *uuid.UUID,
	kind string,
	post repository.Post,
	title string,
	body string,
	actor Principal,
) {
	postIDCopy := post.ID
	route := fmt.Sprintf("/posts/%s", post.ID)
	payload := map[string]any{
		"post_id": post.ID.String(),
		"kind":    kind,
	}
	raw, _ := json.Marshal(payload)
	created, err := s.Repo.CreateNotification(ctx, repository.CreateNotificationParams{
		RecipientUserID: recipientUserID,
		ActorUserID:     actorUserID,
		Kind:            kind,
		PostID:          &postIDCopy,
		Title:           title,
		Body:            body,
		RouteURL:        &route,
		Payload:         raw,
	})
	if err != nil {
		s.logger().Error("notification create", slog.String("err", err.Error()))
		return
	}
	s.push(ctx, recipientUserID, push.Payload{
		Title: created.Title,
		Body:  created.Body,
		Tag:   "post-" + post.ID.String() + "-" + kind,
		URL:   route,
		Data: map[string]any{
			"post_id": post.ID.String(),
			"kind":    kind,
			"actor":   actor.User.ID.String(),
		},
	})
}

func (s *NotificationService) reactionRecipient(ctx context.Context, target repository.ReactionTargetType, targetID uuid.UUID) (uuid.UUID, uuid.UUID, string, error) {
	switch target {
	case repository.ReactionTargetPost:
		post, err := s.Repo.GetPost(ctx, targetID)
		if err != nil {
			return uuid.Nil, uuid.Nil, "", err
		}
		return post.AuthorUserID, post.ID, "post_reaction", nil
	case repository.ReactionTargetComment:
		comment, err := s.Repo.GetComment(ctx, targetID)
		if err != nil {
			return uuid.Nil, uuid.Nil, "", err
		}
		return comment.AuthorUserID, comment.PostID, "comment_reaction", nil
	default:
		return uuid.Nil, uuid.Nil, "", fmt.Errorf("unknown target type %q", target)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func notificationBodyFromPost(post repository.Post) string {
	content := strings.TrimSpace(post.Content)
	if content == "" {
		return "Mở bài viết để xem chi tiết."
	}
	return truncate(content, 140)
}
