package storage

import (
	"context"
	"fmt"
	"io"
	"sort"
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

type DriveUploadMetadata struct {
	AppProperties map[string]string
	Description   string
}

type DriveFolder struct {
	ID       string
	ParentID *string
	Name     string
	Path     string
	Depth    int
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
	metadata DriveUploadMetadata,
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
		Name:          fileName,
		MimeType:      contentType,
		AppProperties: sanitizeDriveAppProperties(metadata.AppProperties),
		Description:   strings.TrimSpace(metadata.Description),
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

func (g *GoogleDrive) ListFolders(ctx context.Context, tokenSource oauth2.TokenSource, rootFolderID string) ([]DriveFolder, error) {
	rootFolderID = strings.TrimSpace(rootFolderID)
	if rootFolderID == "" {
		return nil, fmt.Errorf("root folder id is required")
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("google drive token source is required")
	}

	service, err := drive.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("init google drive service: %w", err)
	}

	type queueItem struct {
		parentID   string
		parentPath string
		depth      int
	}

	queue := []queueItem{{parentID: rootFolderID, parentPath: "", depth: 0}}
	folders := make([]DriveFolder, 0, 32)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		children, err := g.listDirectChildFolders(ctx, service, current.parentID)
		if err != nil {
			return nil, err
		}
		sort.Slice(children, func(i, j int) bool {
			return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
		})

		for _, child := range children {
			child.Depth = current.depth + 1
			if current.parentPath == "" {
				child.Path = child.Name
			} else {
				child.Path = current.parentPath + " / " + child.Name
			}
			folders = append(folders, child)
			queue = append(queue, queueItem{
				parentID:   child.ID,
				parentPath: child.Path,
				depth:      child.Depth,
			})
		}
	}

	return folders, nil
}

func (g *GoogleDrive) listDirectChildFolders(ctx context.Context, service *drive.Service, parentFolderID string) ([]DriveFolder, error) {
	query := fmt.Sprintf(
		"trashed = false and mimeType = 'application/vnd.google-apps.folder' and '%s' in parents",
		parentFolderID,
	)
	call := service.Files.List().
		Q(query).
		Fields("nextPageToken,files(id,name,parents)").
		PageSize(1000).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true)
	if g.sharedDriveID != "" {
		call = call.DriveId(g.sharedDriveID).Corpora("drive")
	}

	folders := make([]DriveFolder, 0, 32)
	pageToken := ""
	for {
		pageCall := call
		if pageToken != "" {
			pageCall = pageCall.PageToken(pageToken)
		}
		resp, err := pageCall.Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("list google drive folders: %w", err)
		}
		for _, file := range resp.Files {
			id := strings.TrimSpace(file.Id)
			name := strings.TrimSpace(file.Name)
			if id == "" || name == "" {
				continue
			}
			var parentID *string
			if len(file.Parents) > 0 {
				parentID = stringPtrOrNil(file.Parents[0])
			}
			folders = append(folders, DriveFolder{
				ID:       id,
				ParentID: parentID,
				Name:     name,
			})
		}
		pageToken = strings.TrimSpace(resp.NextPageToken)
		if pageToken == "" {
			break
		}
	}

	return folders, nil
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func sanitizeDriveAppProperties(items map[string]string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
