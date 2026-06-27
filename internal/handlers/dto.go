// Package handlers wires HTTP requests/responses around services.
// Each module has its own file; dto.go owns response shapes shared by multiple handlers.
package handlers

import (
	"errors"
	"net/http"
	"time"

	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/service"
)

// User mirrors the shared TS contract User.
type UserDTO struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type ProfileDTO struct {
	UserID    string  `json:"user_id"`
	FullName  string  `json:"full_name"`
	Phone     *string `json:"phone"`
	AvatarURL *string `json:"avatar_url"`
}

type PrincipalDTO struct {
	User    UserDTO    `json:"user"`
	Profile ProfileDTO `json:"profile"`
}

type PageMetaDTO struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
	Total  int `json:"total"`
}

func ToUser(u repository.User) UserDTO {
	return UserDTO{
		ID:        u.ID.String(),
		Email:     u.Email,
		IsAdmin:   u.IsAdmin,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
	}
}

func ToProfile(p repository.Profile, avatarURL *string) ProfileDTO {
	return ProfileDTO{
		UserID:    p.UserID.String(),
		FullName:  p.FullName,
		Phone:     p.Phone,
		AvatarURL: avatarURL,
	}
}

func ToPrincipal(p service.Principal, avatarURL *string) PrincipalDTO {
	return PrincipalDTO{
		User:    ToUser(p.User),
		Profile: ToProfile(p.Profile, avatarURL),
	}
}

func ToPageMeta(p service.Page) PageMetaDTO {
	return PageMetaDTO{Limit: p.Limit, Offset: p.Offset, Count: p.Count, Total: p.Total}
}

// ---------- Posts ----------

type PostAuthorDTO struct {
	ID        string  `json:"id"`
	FullName  string  `json:"full_name"`
	AvatarURL *string `json:"avatar_url"`
}

type PostAttachmentDTO struct {
	ID          string  `json:"id"`
	PostID      string  `json:"post_id"`
	Kind        string  `json:"kind"`
	FileName    string  `json:"file_name"`
	ContentType string  `json:"content_type"`
	Bucket      string  `json:"bucket"`
	ObjectKey   string  `json:"object_key"`
	URL         string  `json:"url"`
	SizeBytes   int64   `json:"size_bytes"`
	Width       *int    `json:"width"`
	Height      *int    `json:"height"`
	DurationMs  *int    `json:"duration_ms"`
	SortOrder   int     `json:"sort_order"`
	_           *string // reserved
}

type ReactionSummaryDTO struct {
	Emoji       string `json:"emoji"`
	Count       int    `json:"count"`
	ReactedByMe bool   `json:"reacted_by_me"`
}

type PublicationDTO struct {
	ID          string  `json:"id"`
	Platform    string  `json:"platform"`
	ExternalURL *string `json:"external_url"`
	Note        *string `json:"note"`
}

type PostDTO struct {
	ID           string               `json:"id"`
	Author       PostAuthorDTO        `json:"author"`
	Content      string               `json:"content"`
	Attachments  []PostAttachmentDTO  `json:"attachments"`
	Hashtags     []string             `json:"hashtags"`
	CommentCount int                  `json:"comment_count"`
	Reactions    []ReactionSummaryDTO `json:"reactions"`
	Publications []PublicationDTO     `json:"publications"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// ---------- Comments ----------

type CommentDTO struct {
	ID        string               `json:"id"`
	PostID    string               `json:"post_id"`
	Author    PostAuthorDTO        `json:"author"`
	Content   string               `json:"content"`
	Reactions []ReactionSummaryDTO `json:"reactions"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
}

func ToComment(c service.Comment) CommentDTO {
	reactions := make([]ReactionSummaryDTO, len(c.Reactions))
	for i, r := range c.Reactions {
		reactions[i] = ReactionSummaryDTO{Emoji: r.Emoji, Count: r.Count, ReactedByMe: r.ReactedByMe}
	}
	var avatar *string
	if c.Author.AvatarURL != "" {
		a := c.Author.AvatarURL
		avatar = &a
	}
	return CommentDTO{
		ID:     c.ID.String(),
		PostID: c.PostID.String(),
		Author: PostAuthorDTO{
			ID:        c.Author.ID.String(),
			FullName:  c.Author.FullName,
			AvatarURL: avatar,
		},
		Content:   c.Content,
		Reactions: reactions,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func ToPost(p service.Post) PostDTO {
	attachments := make([]PostAttachmentDTO, len(p.Attachments))
	for i, a := range p.Attachments {
		attachments[i] = PostAttachmentDTO{
			ID:          a.ID.String(),
			PostID:      a.PostID.String(),
			Kind:        string(a.Kind),
			FileName:    a.FileName,
			ContentType: a.ContentType,
			Bucket:      a.Bucket,
			ObjectKey:   a.ObjectKey,
			URL:         a.URL,
			SizeBytes:   a.SizeBytes,
			Width:       a.Width,
			Height:      a.Height,
			DurationMs:  a.DurationMs,
			SortOrder:   a.SortOrder,
		}
	}
	reactions := make([]ReactionSummaryDTO, len(p.Reactions))
	for i, r := range p.Reactions {
		reactions[i] = ReactionSummaryDTO{Emoji: r.Emoji, Count: r.Count, ReactedByMe: r.ReactedByMe}
	}
	publications := make([]PublicationDTO, len(p.Publications))
	for i, pub := range p.Publications {
		publications[i] = PublicationDTO{
			ID:          pub.ID.String(),
			Platform:    pub.Platform,
			ExternalURL: pub.ExternalURL,
			Note:        pub.Note,
		}
	}
	var avatar *string
	if p.Author.AvatarURL != "" {
		a := p.Author.AvatarURL
		avatar = &a
	}
	return PostDTO{
		ID: p.ID.String(),
		Author: PostAuthorDTO{
			ID:        p.Author.ID.String(),
			FullName:  p.Author.FullName,
			AvatarURL: avatar,
		},
		Content:      p.Content,
		Attachments:  attachments,
		Hashtags:     p.Hashtags,
		CommentCount: p.CommentCount,
		Reactions:    reactions,
		Publications: publications,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

// WriteServiceError maps a service.Error (or unknown error) to a JSON HTTP response.
func WriteServiceError(w http.ResponseWriter, err error) {
	var se service.Error
	if errors.As(err, &se) {
		httpx.WriteError(w, se.Status, se.Code, se.Message)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
}

type ReactionDetailDTO struct {
	Emoji     string    `json:"emoji"`
	Count     int       `json:"count"`
	UserID    string    `json:"user_id"`
	FullName  string    `json:"full_name"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
