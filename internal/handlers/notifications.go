package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/service"
)

type NotificationHandler struct {
	Service  *service.NotificationService
	VAPIDKey string
}

type notificationDTO struct {
	ID        string         `json:"id"`
	Actor     *PostAuthorDTO `json:"actor"`
	Kind      string         `json:"kind"`
	PostID    *string        `json:"post_id"`
	CommentID *string        `json:"comment_id"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	RouteURL  *string        `json:"route_url"`
	ReadAt    *time.Time     `json:"read_at"`
	CreatedAt time.Time      `json:"created_at"`
}

type notificationListResponse struct {
	Items []notificationDTO `json:"items"`
}

func (h NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	unreadOnly := r.URL.Query().Get("unread") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Service.List(r.Context(), viewer, unreadOnly, limit)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]notificationDTO, len(items))
	for i, n := range items {
		dtos[i] = toNotificationDTO(n)
	}
	httpx.WriteJSON(w, http.StatusOK, notificationListResponse{Items: dtos})
}

func (h NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := h.Service.MarkRead(r.Context(), viewer, id); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	if err := h.Service.MarkAllRead(r.Context(), viewer); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- Push subscriptions -----

type pushSubscriptionRequest struct {
	Endpoint    string  `json:"endpoint"`
	P256DH      string  `json:"p256dh"`
	Auth        string  `json:"auth"`
	UserAgent   *string `json:"user_agent"`
	DeviceLabel *string `json:"device_label"`
}

type vapidResponse struct {
	PublicKey string `json:"public_key"`
}

func (h NotificationHandler) VAPIDPublicKey(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, vapidResponse{PublicKey: h.VAPIDKey})
}

func (h NotificationHandler) UpsertSubscription(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	var body pushSubscriptionRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.Service.UpsertSubscription(r.Context(), viewer, repository.UpsertSubscriptionParams{
		Endpoint:    body.Endpoint,
		P256DH:      body.P256DH,
		Auth:        body.Auth,
		UserAgent:   body.UserAgent,
		DeviceLabel: body.DeviceLabel,
	}); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deleteSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
}

func (h NotificationHandler) DisableSubscription(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	var body deleteSubscriptionRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.Service.DisableSubscription(r.Context(), viewer, body.Endpoint); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toNotificationDTO(n service.Notification) notificationDTO {
	var postID *string
	if n.PostID != nil {
		v := n.PostID.String()
		postID = &v
	}
	var commentID *string
	if n.CommentID != nil {
		v := n.CommentID.String()
		commentID = &v
	}
	var actor *PostAuthorDTO
	if n.Actor != nil {
		actor = &PostAuthorDTO{
			ID:       n.Actor.ID.String(),
			FullName: n.Actor.FullName,
		}
	}
	return notificationDTO{
		ID:        n.ID.String(),
		Actor:     actor,
		Kind:      n.Kind,
		PostID:    postID,
		CommentID: commentID,
		Title:     n.Title,
		Body:      n.Body,
		RouteURL:  n.RouteURL,
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}
