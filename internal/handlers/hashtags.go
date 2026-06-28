package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type HashtagHandler struct {
	Service *service.HashtagService
}

type hashtagDTO struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	PostCount            int    `json:"post_count"`
	UnpublishedPostCount int    `json:"unpublished_post_count"`
}

type hashtagListResponse struct {
	Items []hashtagDTO `json:"items"`
}

type createHashtagRequest struct {
	Name string `json:"name"`
}

type updateHashtagRequest struct {
	Name string `json:"name"`
}

func (h HashtagHandler) List(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Service.List(r.Context(), viewer, r.URL.Query().Get("q"), limit)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	out := make([]hashtagDTO, len(items))
	for i, item := range items {
		out[i] = toHashtagDTO(item)
	}
	httpx.WriteJSON(w, http.StatusOK, hashtagListResponse{Items: out})
}

func (h HashtagHandler) Create(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	var body createHashtagRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	item, err := h.Service.Create(r.Context(), viewer, body.Name)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toHashtagDTO(item))
}

func (h HashtagHandler) Update(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	currentName := chi.URLParam(r, "hashtagName")
	var body updateHashtagRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	item, err := h.Service.Update(r.Context(), viewer, currentName, body.Name)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toHashtagDTO(item))
}

func (h HashtagHandler) Delete(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	if err := h.Service.Delete(r.Context(), viewer, chi.URLParam(r, "hashtagName")); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toHashtagDTO(item service.Hashtag) hashtagDTO {
	return hashtagDTO{
		ID:                   item.ID,
		Name:                 item.Name,
		PostCount:            item.PostCount,
		UnpublishedPostCount: item.UnpublishedPostCount,
	}
}
