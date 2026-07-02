package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"pqmedia/be/internal/config"
)

type GoogleDrive struct {
	sharedDriveID string
}

type DriveUploadResult struct {
	FileID         string
	FolderID       *string
	WebViewLink    *string
	WebContentLink *string
}

func NewGoogleDrive(cfg config.GoogleDriveConfig) *GoogleDrive {
	return &GoogleDrive{
		sharedDriveID: strings.TrimSpace(cfg.SharedDriveID),
	}
}

func (g *GoogleDrive) UploadVideo(
	ctx context.Context,
	tokenSource oauth2.TokenSource,
	folderID string,
	fileName string,
	contentType string,
	reader io.Reader,
) (DriveUploadResult, error) {
	if strings.TrimSpace(fileName) == "" {
		return DriveUploadResult{}, fmt.Errorf("file name is required")
	}
	if tokenSource == nil {
		return DriveUploadResult{}, fmt.Errorf("google drive token source is required")
	}

	service, err := drive.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return DriveUploadResult{}, fmt.Errorf("init google drive service: %w", err)
	}

	file := &drive.File{
		Name:     fileName,
		MimeType: contentType,
	}
	targetFolderID := strings.TrimSpace(folderID)
	if targetFolderID != "" {
		file.Parents = []string{targetFolderID}
	}

	call := service.Files.Create(file).
		SupportsAllDrives(true).
		Fields("id,parents,webViewLink,webContentLink").
		Media(reader)

	created, err := call.Context(ctx).Do()
	if err != nil {
		return DriveUploadResult{}, fmt.Errorf("upload to google drive: %w", err)
	}

	var uploadedFolderID *string
	if len(created.Parents) > 0 {
		uploadedFolderID = &created.Parents[0]
	}
	return DriveUploadResult{
		FileID:         created.Id,
		FolderID:       uploadedFolderID,
		WebViewLink:    stringPtrOrNil(created.WebViewLink),
		WebContentLink: stringPtrOrNil(created.WebContentLink),
	}, nil
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
