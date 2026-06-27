package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/service"
)

type ReactionHandler struct {
	Service *service.ReactionService
}

type toggleReactionRequest struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Emoji      string `json:"emoji"`
	Delta      int    `json:"delta"`
}

type toggleReactionResponse struct {
	Active    bool                 `json:"active"`
	Summaries []ReactionSummaryDTO `json:"summaries"`
}

func (h ReactionHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	var body toggleReactionRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	targetID, err := uuid.Parse(body.TargetID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target_id", err.Error())
		return
	}
	target := repository.ReactionTargetType(body.TargetType)
	delta := body.Delta
	if delta <= 0 {
		delta = 1
	}
	active, err := h.Service.Toggle(r.Context(), viewer, target, targetID, body.Emoji, delta)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	summaries, err := h.Service.Summaries(r.Context(), viewer, target, targetID)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]ReactionSummaryDTO, len(summaries))
	for i, s := range summaries {
		dtos[i] = ReactionSummaryDTO{Emoji: s.Emoji, Count: s.Count, ReactedByMe: s.ReactedByMe}
	}
	httpx.WriteJSON(w, http.StatusOK, toggleReactionResponse{Active: active, Summaries: dtos})
}

type reactionListResponse struct {
	Summaries []ReactionSummaryDTO `json:"summaries"`
}

func (h ReactionHandler) List(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	target := repository.ReactionTargetType(r.URL.Query().Get("target_type"))
	rawID := r.URL.Query().Get("target_id")
	targetID, err := uuid.Parse(rawID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target_id", err.Error())
		return
	}
	summaries, err := h.Service.Summaries(r.Context(), viewer, target, targetID)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]ReactionSummaryDTO, len(summaries))
	for i, s := range summaries {
		dtos[i] = ReactionSummaryDTO{Emoji: s.Emoji, Count: s.Count, ReactedByMe: s.ReactedByMe}
	}
	httpx.WriteJSON(w, http.StatusOK, reactionListResponse{Summaries: dtos})
}

// service.NewError ensures we don't shadow the var in this file.
var _ = service.NewError

type reactionDetailsResponse struct {
	Details []ReactionDetailDTO `json:"details"`
}

func (h ReactionHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())

	targetType := r.URL.Query().Get("target_type")
	targetIDStr := r.URL.Query().Get("target_id")
	if targetType == "" || targetIDStr == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "missing target_type or target_id")
		return
	}
	targetID, err := uuid.Parse(targetIDStr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target_id", "invalid target_id")
		return
	}
	target := repository.ReactionTargetType(targetType)

	details, err := h.Service.Details(r.Context(), viewer, target, targetID)
	if err != nil {
		WriteServiceError(w, err)
		return
	}

	dtos := make([]ReactionDetailDTO, len(details))
	for i, d := range details {
		dtos[i] = ReactionDetailDTO{
			Emoji:     d.Emoji,
			Count:     d.Count,
			UserID:    d.UserID.String(),
			FullName:  d.FullName,
			AvatarURL: d.AvatarURL,
			CreatedAt: d.CreatedAt,
		}
	}

	httpx.WriteJSON(w, http.StatusOK, reactionDetailsResponse{Details: dtos})
}
