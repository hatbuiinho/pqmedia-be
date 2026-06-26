package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type UserHandler struct {
	Service *service.UserService
}

type createUserRequest struct {
	Email    string  `json:"email"`
	Password string  `json:"password"`
	FullName string  `json:"full_name"`
	Phone    *string `json:"phone"`
	IsAdmin  bool    `json:"is_admin"`
}

type updateProfileRequest struct {
	FullName string  `json:"full_name"`
	Phone    *string `json:"phone"`
}

type listResponse struct {
	Items []PrincipalDTO `json:"items"`
	Page  PageMetaDTO    `json:"page"`
}

func (h UserHandler) List(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	items, page, err := h.Service.ListUsers(r.Context(), actor, q, limit, offset)
	if err != nil {
		WriteServiceError(w, err)
		return
	}

	dtos := make([]PrincipalDTO, 0, len(items))
	for _, p := range items {
		dtos = append(dtos, ToPrincipal(p, nil))
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Items: dtos, Page: ToPageMeta(page)})
}

func (h UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body createUserRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := h.Service.CreateUser(r.Context(), actor, service.CreateUserInput{
		Email:    body.Email,
		Password: body.Password,
		FullName: body.FullName,
		Phone:    body.Phone,
		IsAdmin:  body.IsAdmin,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToPrincipal(created, nil))
}

func (h UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	var body updateProfileRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdateProfile(r.Context(), actor, userID, body.FullName, body.Phone)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(updated, nil))
}

func (h UserHandler) UpdateOwnProfile(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body updateProfileRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdateProfile(r.Context(), actor, actor.User.ID, body.FullName, body.Phone)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(updated, nil))
}
