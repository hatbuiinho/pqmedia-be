package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type CommentHandler struct {
	Service *service.CommentService
}

type createCommentRequest struct {
	Content string `json:"content"`
}

type commentListResponse struct {
	Items []CommentDTO `json:"items"`
}

func (h CommentHandler) ListByPost(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	postID, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	items, err := h.Service.List(r.Context(), viewer, postID)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	dtos := make([]CommentDTO, len(items))
	for i, c := range items {
		dtos[i] = ToComment(c)
	}
	httpx.WriteJSON(w, http.StatusOK, commentListResponse{Items: dtos})
}

func (h CommentHandler) Create(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	postID, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	var body createCommentRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	comment, err := h.Service.Create(r.Context(), viewer, postID, body.Content)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToComment(comment))
}

func (h CommentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	commentID, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_comment_id", err.Error())
		return
	}
	if err := h.Service.Delete(r.Context(), viewer, commentID); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
