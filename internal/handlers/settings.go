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

type driveFolderDTO struct {
	FolderID       string  `json:"folder_id"`
	ParentFolderID *string `json:"parent_folder_id"`
	Name           string  `json:"name"`
	Path           string  `json:"path"`
	Depth          int     `json:"depth"`
}

type driveFolderListDTO struct {
	SyncEnabled     bool             `json:"sync_enabled"`
	RootFolderID    string           `json:"root_folder_id"`
	Folders         []driveFolderDTO `json:"folders"`
	LastSyncedAt    *time.Time       `json:"last_synced_at"`
	CanUploadToRoot bool             `json:"can_upload_to_root"`
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

func (h SettingsHandler) ListDriveFolders(w http.ResponseWriter, r *http.Request) {
	_ = authctx.MustPrincipal(r.Context())
	if h.Service.DriveFolders == nil {
		WriteServiceError(w, service.ValidationError("drive folder service is not configured"))
		return
	}
	cache, err := h.Service.DriveFolders.GetCache(r.Context())
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDriveFolderListDTO(h.Service.DriveSyncEnabled, cache))
}

func (h SettingsHandler) RefreshDriveFolders(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	if h.Service.DriveFolders == nil {
		WriteServiceError(w, service.ValidationError("drive folder service is not configured"))
		return
	}
	cache, err := h.Service.DriveFolders.RefreshCache(r.Context(), actor)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDriveFolderListDTO(h.Service.DriveSyncEnabled, cache))
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

func toDriveFolderListDTO(syncEnabled bool, cache service.DriveFolderCache) driveFolderListDTO {
	items := make([]driveFolderDTO, len(cache.Folders))
	for i, item := range cache.Folders {
		items[i] = driveFolderDTO{
			FolderID:       item.FolderID,
			ParentFolderID: item.ParentFolderID,
			Name:           item.Name,
			Path:           item.Path,
			Depth:          item.Depth,
		}
	}
	return driveFolderListDTO{
		SyncEnabled:     syncEnabled,
		RootFolderID:    cache.RootFolderID,
		Folders:         items,
		LastSyncedAt:    cache.LastSyncedAt,
		CanUploadToRoot: cache.CanUploadToRoot,
	}
}
