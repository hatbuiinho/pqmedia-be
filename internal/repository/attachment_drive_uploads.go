package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AttachmentDriveSyncJob struct {
	Sync       AttachmentDriveSync
	Attachment PostAttachment
}

func (r *Repo) QueueAttachmentDriveSyncs(ctx context.Context, attachments []PostAttachment) error {
	if len(attachments) == 0 {
		return nil
	}
	for _, attachment := range attachments {
		if attachment.Kind != AttachmentVideo {
			continue
		}
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO attachment_drive_uploads (
				attachment_id,
				status,
				error_message,
				attempt_count,
				next_attempt_at,
				last_attempt_at,
				uploaded_at,
				updated_at
			)
			VALUES ($1, $2, NULL, 0, now(), NULL, NULL, now())
			ON CONFLICT (attachment_id) DO NOTHING
		`, attachment.ID, AttachmentDriveSyncPending); err != nil {
			return fmt.Errorf("queue attachment drive sync %s: %w", attachment.ID, err)
		}
	}
	return nil
}

func (r *Repo) ListAttachmentDriveSyncsByAttachments(ctx context.Context, attachmentIDs []uuid.UUID) (map[uuid.UUID]AttachmentDriveSync, error) {
	if len(attachmentIDs) == 0 {
		return map[uuid.UUID]AttachmentDriveSync{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT attachment_id, status, drive_file_id, drive_folder_id, web_view_link, web_content_link,
		       error_message, attempt_count, next_attempt_at, last_attempt_at, uploaded_at, created_at, updated_at
		FROM attachment_drive_uploads
		WHERE attachment_id = ANY($1)
	`, attachmentIDs)
	if err != nil {
		return nil, fmt.Errorf("list attachment drive syncs: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]AttachmentDriveSync, len(attachmentIDs))
	for rows.Next() {
		var sync AttachmentDriveSync
		if err := rows.Scan(
			&sync.AttachmentID,
			&sync.Status,
			&sync.DriveFileID,
			&sync.DriveFolderID,
			&sync.WebViewLink,
			&sync.WebContentLink,
			&sync.ErrorMessage,
			&sync.AttemptCount,
			&sync.NextAttemptAt,
			&sync.LastAttemptAt,
			&sync.UploadedAt,
			&sync.CreatedAt,
			&sync.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan attachment drive sync: %w", err)
		}
		out[sync.AttachmentID] = sync
	}
	return out, rows.Err()
}

func (r *Repo) ClaimAttachmentDriveSyncBatch(ctx context.Context, limit int) ([]AttachmentDriveSyncJob, error) {
	if limit <= 0 {
		limit = 1
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin claim attachment drive sync tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		WITH picked AS (
			SELECT ads.attachment_id
			FROM attachment_drive_uploads ads
			JOIN post_attachments pa ON pa.id = ads.attachment_id
			WHERE pa.kind = $1
			  AND ads.status IN ($2, $3)
			  AND ads.next_attempt_at <= now()
			ORDER BY ads.next_attempt_at, ads.created_at
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		)
		UPDATE attachment_drive_uploads ads
		SET status = $5,
		    attempt_count = ads.attempt_count + 1,
		    last_attempt_at = now(),
		    error_message = NULL,
		    updated_at = now()
		FROM picked, post_attachments pa
		WHERE ads.attachment_id = picked.attachment_id
		  AND pa.id = ads.attachment_id
		RETURNING
			ads.attachment_id, ads.status, ads.drive_file_id, ads.drive_folder_id, ads.web_view_link,
			ads.web_content_link, ads.error_message, ads.attempt_count, ads.next_attempt_at,
			ads.last_attempt_at, ads.uploaded_at, ads.created_at, ads.updated_at,
			pa.id, pa.post_id, pa.kind, pa.file_name, pa.content_type, pa.bucket, pa.object_key,
			pa.size_bytes, pa.width, pa.height, pa.duration_ms, pa.sort_order
	`, AttachmentVideo, AttachmentDriveSyncPending, AttachmentDriveSyncFailed, limit, AttachmentDriveSyncUploading)
	if err != nil {
		return nil, fmt.Errorf("claim attachment drive sync batch: %w", err)
	}
	defer rows.Close()

	jobs := make([]AttachmentDriveSyncJob, 0, limit)
	for rows.Next() {
		var job AttachmentDriveSyncJob
		if err := rows.Scan(
			&job.Sync.AttachmentID,
			&job.Sync.Status,
			&job.Sync.DriveFileID,
			&job.Sync.DriveFolderID,
			&job.Sync.WebViewLink,
			&job.Sync.WebContentLink,
			&job.Sync.ErrorMessage,
			&job.Sync.AttemptCount,
			&job.Sync.NextAttemptAt,
			&job.Sync.LastAttemptAt,
			&job.Sync.UploadedAt,
			&job.Sync.CreatedAt,
			&job.Sync.UpdatedAt,
			&job.Attachment.ID,
			&job.Attachment.PostID,
			&job.Attachment.Kind,
			&job.Attachment.FileName,
			&job.Attachment.ContentType,
			&job.Attachment.Bucket,
			&job.Attachment.ObjectKey,
			&job.Attachment.SizeBytes,
			&job.Attachment.Width,
			&job.Attachment.Height,
			&job.Attachment.DurationMs,
			&job.Attachment.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan claimed attachment drive sync job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim attachment drive sync tx: %w", err)
	}
	return jobs, nil
}

func (r *Repo) MarkAttachmentDriveSyncUploaded(
	ctx context.Context,
	attachmentID uuid.UUID,
	driveFileID string,
	driveFolderID *string,
	webViewLink *string,
	webContentLink *string,
) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE attachment_drive_uploads
		SET status = $2,
		    drive_file_id = $3,
		    drive_folder_id = $4,
		    web_view_link = $5,
		    web_content_link = $6,
		    error_message = NULL,
		    uploaded_at = now(),
		    next_attempt_at = now(),
		    updated_at = now()
		WHERE attachment_id = $1
	`, attachmentID, AttachmentDriveSyncUploaded, driveFileID, driveFolderID, webViewLink, webContentLink); err != nil {
		return fmt.Errorf("mark attachment drive sync uploaded %s: %w", attachmentID, err)
	}
	return nil
}

func (r *Repo) MarkAttachmentDriveSyncFailed(ctx context.Context, attachmentID uuid.UUID, message string, nextAttemptAt time.Time) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE attachment_drive_uploads
		SET status = $2,
		    error_message = $3,
		    next_attempt_at = $4,
		    updated_at = now()
		WHERE attachment_id = $1
	`, attachmentID, AttachmentDriveSyncFailed, message, nextAttemptAt); err != nil {
		return fmt.Errorf("mark attachment drive sync failed %s: %w", attachmentID, err)
	}
	return nil
}

func (r *Repo) MarkAttachmentDriveSyncBlocked(ctx context.Context, attachmentID uuid.UUID, message string) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE attachment_drive_uploads
		SET status = $2,
		    error_message = $3,
		    next_attempt_at = now(),
		    updated_at = now()
		WHERE attachment_id = $1
	`, attachmentID, AttachmentDriveSyncBlocked, message); err != nil {
		return fmt.Errorf("mark attachment drive sync blocked %s: %w", attachmentID, err)
	}
	return nil
}

func (r *Repo) RequeueAttachmentDriveSyncs(ctx context.Context, statuses []AttachmentDriveSyncStatus) error {
	if len(statuses) == 0 {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `
		UPDATE attachment_drive_uploads
		SET status = $2,
		    error_message = NULL,
		    next_attempt_at = now(),
		    updated_at = now()
		WHERE status = ANY($1)
	`, statuses, AttachmentDriveSyncPending); err != nil {
		return fmt.Errorf("requeue attachment drive syncs: %w", err)
	}
	return nil
}
