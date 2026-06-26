package handlers

import (
	"net/http"
	"time"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type AuthHandler struct {
	Service *service.UserService
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenPairResponse struct {
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	AccessExpiresAt time.Time `json:"access_expires_at"`
}

type loginResponse struct {
	tokenPairResponse
	Principal PrincipalDTO `json:"principal"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.Service.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, loginResponse{
		tokenPairResponse: tokenPairResponse{
			AccessToken:     result.Tokens.AccessToken,
			RefreshToken:    result.Tokens.RefreshToken,
			AccessExpiresAt: result.Tokens.AccessExpiresAt,
		},
		Principal: ToPrincipal(result.Principal, nil),
	})
}

func (h AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	tokens, err := h.Service.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tokenPairResponse{
		AccessToken:     tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
		AccessExpiresAt: tokens.AccessExpiresAt,
	})
}

// Logout is stateless: with JWT we cannot revoke server-side, so the client
// clears its tokens. Endpoint exists for symmetry and future audit logging.
func (h AuthHandler) Logout(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal := authctx.MustPrincipal(r.Context())
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(principal, nil))
}
