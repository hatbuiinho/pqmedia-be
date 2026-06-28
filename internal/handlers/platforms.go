package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type PlatformHandler struct {
	Service *service.PlatformService
}

type createPlatformRequest struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Icon      string `json:"icon"`
	Tone      string `json:"tone"`
	SortOrder int    `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
}

type updatePlatformRequest struct {
	Label     string `json:"label"`
	Icon      string `json:"icon"`
	Tone      string `json:"tone"`
	SortOrder int    `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
}

type platformsListResponse struct {
	Items []PlatformDTO `json:"items"`
}

func (h PlatformHandler) List(w http.ResponseWriter, r *http.Request) {
	includeInactive, _ := strconv.ParseBool(r.URL.Query().Get("include_inactive"))
	items, err := h.Service.ListPlatforms(r.Context(), includeInactive)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]PlatformDTO, len(items))
	for i, item := range items {
		dtos[i] = toPlatformDTO(item)
	}
	httpx.WriteJSON(w, http.StatusOK, platformsListResponse{Items: dtos})
}

func (h PlatformHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body createPlatformRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := h.Service.CreatePlatform(r.Context(), actor, service.CreatePlatformInput{
		Key:       body.Key,
		Label:     body.Label,
		Icon:      body.Icon,
		Tone:      body.Tone,
		SortOrder: body.SortOrder,
		IsActive:  body.IsActive,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toPlatformDTO(created))
}

func (h PlatformHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	key := chi.URLParam(r, "platformKey")
	var body updatePlatformRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdatePlatform(r.Context(), actor, key, service.UpdatePlatformInput{
		Label:     body.Label,
		Icon:      body.Icon,
		Tone:      body.Tone,
		SortOrder: body.SortOrder,
		IsActive:  body.IsActive,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPlatformDTO(updated))
}

func (h PlatformHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	key := chi.URLParam(r, "platformKey")
	if err := h.Service.DeletePlatform(r.Context(), actor, key); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
