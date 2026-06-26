package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type PublicationHandler struct {
	Service *service.PublicationService
}

type upsertPublicationRequest struct {
	ExternalURL *string    `json:"external_url"`
	PublishedAt *time.Time `json:"published_at"`
	Note        *string    `json:"note"`
}

type publicationsListResponse struct {
	Items []PublicationDTO `json:"items"`
}

func (h PublicationHandler) List(w http.ResponseWriter, r *http.Request) {
	postID, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	items, err := h.Service.ListForPost(r.Context(), postID)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]PublicationDTO, len(items))
	for i, p := range items {
		dtos[i] = PublicationDTO{
			ID:          p.ID.String(),
			Platform:    p.Platform,
			ExternalURL: p.ExternalURL,
			Note:        p.Note,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, publicationsListResponse{Items: dtos})
}

func (h PublicationHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	postID, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	platform := chi.URLParam(r, "platform")
	var body upsertPublicationRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		// Empty body is OK — treat as toggle-on with defaults.
		body = upsertPublicationRequest{}
	}
	created, err := h.Service.Upsert(r.Context(), viewer, postID, platform, service.UpsertPublicationInput{
		ExternalURL: body.ExternalURL,
		PublishedAt: body.PublishedAt,
		Note:        body.Note,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PublicationDTO{
		ID:          created.ID.String(),
		Platform:    created.Platform,
		ExternalURL: created.ExternalURL,
		Note:        created.Note,
	})
}

func (h PublicationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	postID, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	platform := chi.URLParam(r, "platform")
	if err := h.Service.Delete(r.Context(), viewer, postID, platform); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
