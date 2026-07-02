package handlers

import (
	"net/http"
	"time"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type SettingsHandler struct {
	Service *service.SettingsService
}

type driveSettingsDTO struct {
	SyncEnabled      bool       `json:"sync_enabled"`
	RootFolderID     string     `json:"root_folder_id"`
	OAuthReady       bool       `json:"oauth_ready"`
	Connected        bool       `json:"connected"`
	ConnectedEmail   *string    `json:"connected_email"`
	ConnectedAt      *time.Time `json:"connected_at"`
	LastConnectError *string    `json:"last_connect_error"`
}

type updateDriveSettingsRequest struct {
	RootFolderID string `json:"root_folder_id"`
}

func (h SettingsHandler) GetDriveSettings(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	settings, err := h.Service.GetDriveSettings(r.Context(), actor)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, driveSettingsDTO{
		SyncEnabled:      settings.SyncEnabled,
		RootFolderID:     settings.RootFolderID,
		OAuthReady:       settings.OAuthReady,
		Connected:        settings.Connected,
		ConnectedEmail:   settings.ConnectedEmail,
		ConnectedAt:      settings.ConnectedAt,
		LastConnectError: settings.LastConnectError,
	})
}

func (h SettingsHandler) UpdateDriveSettings(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body updateDriveSettingsRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	settings, err := h.Service.UpdateDriveSettings(r.Context(), actor, service.UpdateDriveSettingsInput{
		RootFolderID: body.RootFolderID,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, driveSettingsDTO{
		SyncEnabled:      settings.SyncEnabled,
		RootFolderID:     settings.RootFolderID,
		OAuthReady:       settings.OAuthReady,
		Connected:        settings.Connected,
		ConnectedEmail:   settings.ConnectedEmail,
		ConnectedAt:      settings.ConnectedAt,
		LastConnectError: settings.LastConnectError,
	})
}

func (h SettingsHandler) StartGoogleDriveOAuth(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	authURL, err := h.Service.DriveOAuth.StartConnect(r.Context(), actor)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

func (h SettingsHandler) GoogleDriveOAuthCallback(w http.ResponseWriter, r *http.Request) {
	redirectURL, err := h.Service.DriveOAuth.HandleCallback(
		r.Context(),
		r.URL.Query().Get("state"),
		r.URL.Query().Get("code"),
		r.URL.Query().Get("error"),
	)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h SettingsHandler) DisconnectGoogleDriveOAuth(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	if err := h.Service.DriveOAuth.Disconnect(r.Context(), actor); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
