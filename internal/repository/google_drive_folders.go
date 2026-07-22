package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type GoogleDriveFolder struct {
	RootFolderID   string
	FolderID       string
	ParentFolderID *string
	Name           string
	Path           string
	Depth          int
	SortOrder      int
	LastSyncedAt   time.Time
}

func (r *Repo) ReplaceGoogleDriveFolders(ctx context.Context, rootFolderID string, folders []GoogleDriveFolder) error {
	rootFolderID = strings.TrimSpace(rootFolderID)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin replace google drive folders tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM google_drive_folders WHERE root_folder_id = $1`, rootFolderID); err != nil {
		return fmt.Errorf("delete google drive folders: %w", err)
	}

	syncedAt := time.Now()
	for _, folder := range folders {
		if _, err := tx.Exec(ctx, `
			INSERT INTO google_drive_folders (
				root_folder_id, folder_id, parent_folder_id, name, path, depth, sort_order, last_synced_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		`, rootFolderID, folder.FolderID, folder.ParentFolderID, folder.Name, folder.Path, folder.Depth, folder.SortOrder, syncedAt); err != nil {
			return fmt.Errorf("insert google drive folder %s: %w", folder.FolderID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit replace google drive folders tx: %w", err)
	}
	return nil
}

func (r *Repo) ListGoogleDriveFoldersByRoot(ctx context.Context, rootFolderID string) ([]GoogleDriveFolder, *time.Time, error) {
	rootFolderID = strings.TrimSpace(rootFolderID)
	if rootFolderID == "" {
		return []GoogleDriveFolder{}, nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT root_folder_id, folder_id, parent_folder_id, name, path, depth, sort_order, last_synced_at
		FROM google_drive_folders
		WHERE root_folder_id = $1
		ORDER BY depth, sort_order, path, folder_id
	`, rootFolderID)
	if err != nil {
		return nil, nil, fmt.Errorf("list google drive folders: %w", err)
	}
	defer rows.Close()

	items := make([]GoogleDriveFolder, 0, 32)
	var lastSyncedAt *time.Time
	for rows.Next() {
		var item GoogleDriveFolder
		if err := rows.Scan(
			&item.RootFolderID,
			&item.FolderID,
			&item.ParentFolderID,
			&item.Name,
			&item.Path,
			&item.Depth,
			&item.SortOrder,
			&item.LastSyncedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan google drive folder: %w", err)
		}
		if lastSyncedAt == nil || item.LastSyncedAt.After(*lastSyncedAt) {
			value := item.LastSyncedAt
			lastSyncedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate google drive folders: %w", err)
	}
	return items, lastSyncedAt, nil
}

func (r *Repo) GoogleDriveFolderExists(ctx context.Context, rootFolderID, folderID string) (bool, error) {
	rootFolderID = strings.TrimSpace(rootFolderID)
	folderID = strings.TrimSpace(folderID)
	if rootFolderID == "" || folderID == "" {
		return false, nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM google_drive_folders
			WHERE root_folder_id = $1 AND folder_id = $2
		)
	`, rootFolderID, folderID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check google drive folder exists: %w", err)
	}
	return exists, nil
}
