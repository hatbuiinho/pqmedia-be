package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"pqmedia/be/internal/repository"
)

type DriveFolder struct {
	FolderID       string
	ParentFolderID *string
	Name           string
	Path           string
	Depth          int
}

type DriveFolderCache struct {
	RootFolderID    string
	Folders         []DriveFolder
	LastSyncedAt    *time.Time
	CanUploadToRoot bool
}

type DriveFolderService struct {
	Repo                *repository.Repo
	DriveOAuth          *GoogleDriveOAuthService
	Logger              *slog.Logger
	DefaultRootFolderID string
}

func (s *DriveFolderService) ResolveRootFolderID(ctx context.Context) (string, string, error) {
	items, err := s.Repo.ListSystemSettingsByKeys(ctx, []string{DriveRootFolderSettingKey})
	if err != nil {
		return "", "", err
	}
	if value := strings.TrimSpace(items[DriveRootFolderSettingKey]); value != "" {
		return value, "system_settings", nil
	}
	if value := strings.TrimSpace(s.DefaultRootFolderID); value != "" {
		return value, "env_default", nil
	}
	return "", "empty", nil
}

func (s *DriveFolderService) GetCache(ctx context.Context) (DriveFolderCache, error) {
	rootFolderID, _, err := s.ResolveRootFolderID(ctx)
	if err != nil {
		return DriveFolderCache{}, err
	}
	items, lastSyncedAt, err := s.Repo.ListGoogleDriveFoldersByRoot(ctx, rootFolderID)
	if err != nil {
		return DriveFolderCache{}, err
	}
	folders := make([]DriveFolder, len(items))
	for i, item := range items {
		folders[i] = DriveFolder{
			FolderID:       item.FolderID,
			ParentFolderID: item.ParentFolderID,
			Name:           item.Name,
			Path:           item.Path,
			Depth:          item.Depth,
		}
	}
	return DriveFolderCache{
		RootFolderID:    rootFolderID,
		Folders:         folders,
		LastSyncedAt:    lastSyncedAt,
		CanUploadToRoot: rootFolderID != "" && len(folders) == 0,
	}, nil
}

func (s *DriveFolderService) RefreshCache(ctx context.Context, actor Principal) (DriveFolderCache, error) {
	if !actor.User.IsAdmin {
		return DriveFolderCache{}, ErrForbidden
	}
	rootFolderID, _, err := s.ResolveRootFolderID(ctx)
	if err != nil {
		return DriveFolderCache{}, err
	}
	if rootFolderID == "" {
		return DriveFolderCache{}, ValidationError("chưa cấu hình folder gốc Google Drive")
	}
	folders, err := s.DriveOAuth.ListFolders(ctx, rootFolderID)
	if err != nil {
		return DriveFolderCache{}, err
	}
	repoItems := make([]repository.GoogleDriveFolder, len(folders))
	for i, folder := range folders {
		repoItems[i] = repository.GoogleDriveFolder{
			RootFolderID:   rootFolderID,
			FolderID:       folder.ID,
			ParentFolderID: folder.ParentID,
			Name:           folder.Name,
			Path:           folder.Path,
			Depth:          folder.Depth,
			SortOrder:      i,
		}
	}
	if err := s.Repo.ReplaceGoogleDriveFolders(ctx, rootFolderID, repoItems); err != nil {
		return DriveFolderCache{}, err
	}
	return s.GetCache(ctx)
}

func (s *DriveFolderService) ResolveTargetFolderIDForPost(ctx context.Context, selectedFolderID *string, attachments []repository.PostAttachmentInput) (*string, error) {
	if !containsVideoAttachmentInputs(attachments) {
		return nil, nil
	}

	cache, err := s.GetCache(ctx)
	if err != nil {
		return nil, err
	}
	if cache.RootFolderID == "" {
		return nil, ValidationError("chưa cấu hình folder gốc Google Drive")
	}
	if len(cache.Folders) == 0 {
		target := cache.RootFolderID
		return &target, nil
	}

	selected := strings.TrimSpace(derefString(selectedFolderID))
	if selected == "" {
		return nil, ValidationError("vui lòng chọn thư mục Drive cho bài viết có video")
	}
	exists, err := s.Repo.GoogleDriveFolderExists(ctx, cache.RootFolderID, selected)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ValidationError("thư mục Drive đã chọn không còn hợp lệ, hãy tải lại danh sách")
	}
	return &selected, nil
}

func (s *DriveFolderService) ResolveUploadFolderID(ctx context.Context, postTargetFolderID *string) (string, string, error) {
	target := strings.TrimSpace(derefString(postTargetFolderID))
	if target != "" {
		return target, "post_target_folder", nil
	}

	cache, err := s.GetCache(ctx)
	if err != nil {
		return "", "", err
	}
	if cache.RootFolderID == "" {
		return "", "", fmt.Errorf("google drive root folder is not configured")
	}
	if len(cache.Folders) == 0 {
		return cache.RootFolderID, "root_folder_fallback", nil
	}
	return "", "", fmt.Errorf("drive target folder is required for video upload")
}

func containsVideoAttachmentInputs(items []repository.PostAttachmentInput) bool {
	for _, item := range items {
		if item.Kind == repository.AttachmentVideo {
			return true
		}
	}
	return false
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
