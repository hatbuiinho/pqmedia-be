package service

import (
	"context"
	"strings"
	"time"

	"pqmedia/be/internal/repository"
)

const DriveRootFolderSettingKey = "google_drive_root_folder_id"

type DriveSettings struct {
	SyncEnabled      bool
	RootFolderID     string
	OAuthReady       bool
	Connected        bool
	ConnectedEmail   *string
	ConnectedAt      *time.Time
	LastConnectError *string
}

type UpdateDriveSettingsInput struct {
	RootFolderID string
}

type SettingsService struct {
	Repo                     *repository.Repo
	DriveOAuth               *GoogleDriveOAuthService
	DriveSyncEnabled         bool
	DefaultDriveRootFolderID string
}

func (s *SettingsService) GetDriveSettings(ctx context.Context, actor Principal) (DriveSettings, error) {
	if !actor.User.IsAdmin {
		return DriveSettings{}, ErrForbidden
	}
	rootFolderID, err := s.resolveDriveRootFolderID(ctx)
	if err != nil {
		return DriveSettings{}, err
	}
	connection, err := s.DriveOAuth.GetConnectionStatus(ctx)
	if err != nil {
		return DriveSettings{}, err
	}
	return DriveSettings{
		SyncEnabled:      s.DriveSyncEnabled,
		RootFolderID:     rootFolderID,
		OAuthReady:       connection.OAuthReady,
		Connected:        connection.Connected,
		ConnectedEmail:   connection.ConnectedEmail,
		ConnectedAt:      connection.ConnectedAt,
		LastConnectError: connection.LastError,
	}, nil
}

func (s *SettingsService) UpdateDriveSettings(ctx context.Context, actor Principal, input UpdateDriveSettingsInput) (DriveSettings, error) {
	if !actor.User.IsAdmin {
		return DriveSettings{}, ErrForbidden
	}
	rootFolderID := strings.TrimSpace(input.RootFolderID)
	if err := s.Repo.UpsertSystemSetting(ctx, DriveRootFolderSettingKey, rootFolderID); err != nil {
		return DriveSettings{}, err
	}
	_ = s.Repo.RequeueAttachmentDriveSyncs(ctx, []repository.AttachmentDriveSyncStatus{
		repository.AttachmentDriveSyncBlocked,
		repository.AttachmentDriveSyncFailed,
	})
	return s.GetDriveSettings(ctx, actor)
}

func (s *SettingsService) resolveDriveRootFolderID(ctx context.Context) (string, error) {
	items, err := s.Repo.ListSystemSettingsByKeys(ctx, []string{DriveRootFolderSettingKey})
	if err != nil {
		return "", err
	}
	if value := strings.TrimSpace(items[DriveRootFolderSettingKey]); value != "" {
		return value, nil
	}
	return strings.TrimSpace(s.DefaultDriveRootFolderID), nil
}
