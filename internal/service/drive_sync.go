package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

type DriveSyncService struct {
	Repo                *repository.Repo
	Storage             *storage.MinIO
	Drive               *GoogleDriveOAuthService
	Logger              *slog.Logger
	Enabled             bool
	DefaultRootFolderID string
	PollEvery           time.Duration
	BatchSize           int
	BaseDelay           time.Duration
	MaxDelay            time.Duration
	triggerCh           chan struct{}
	running             atomic.Bool
}

func NewDriveSyncService(
	repo *repository.Repo,
	store *storage.MinIO,
	drive *GoogleDriveOAuthService,
	cfg config.GoogleDriveConfig,
	logger *slog.Logger,
) *DriveSyncService {
	return &DriveSyncService{
		Repo:                repo,
		Storage:             store,
		Drive:               drive,
		Logger:              logger,
		Enabled:             cfg.Enabled && drive != nil,
		DefaultRootFolderID: strings.TrimSpace(cfg.RootFolderID),
		PollEvery:           cfg.SyncPollInterval,
		BatchSize:           cfg.SyncBatchSize,
		BaseDelay:           cfg.FailureRetryBaseDelay,
		MaxDelay:            cfg.FailureRetryMaxDelay,
		triggerCh:           make(chan struct{}, 1),
	}
}

func (s *DriveSyncService) Start(ctx context.Context) {
	if !s.Enabled {
		return
	}
	ticker := time.NewTicker(s.PollEvery)
	defer ticker.Stop()

	s.Signal()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runBatch(ctx)
		case <-s.triggerCh:
			s.runBatch(ctx)
		}
	}
}

func (s *DriveSyncService) QueueAttachments(ctx context.Context, attachments []repository.PostAttachment) error {
	if !s.Enabled || len(attachments) == 0 {
		return nil
	}
	if err := s.Repo.QueueAttachmentDriveSyncs(ctx, attachments); err != nil {
		return err
	}
	s.Signal()
	return nil
}

func (s *DriveSyncService) Signal() {
	if !s.Enabled {
		return
	}
	select {
	case s.triggerCh <- struct{}{}:
	default:
	}
}

func (s *DriveSyncService) runBatch(ctx context.Context) {
	if !s.Enabled || !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	for {
		jobs, err := s.Repo.ClaimAttachmentDriveSyncBatch(ctx, s.BatchSize)
		if err != nil {
			s.Logger.Error("claim drive sync batch", slog.String("err", err.Error()))
			return
		}
		if len(jobs) == 0 {
			return
		}
		for _, job := range jobs {
			s.processJob(ctx, job)
		}
		if len(jobs) < s.BatchSize {
			return
		}
	}
}

func (s *DriveSyncService) processJob(parentCtx context.Context, job repository.AttachmentDriveSyncJob) {
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Minute)
	defer cancel()

	reader, err := s.Storage.OpenObject(ctx, job.Attachment.ObjectKey)
	if err != nil {
		s.failJob(ctx, job, fmt.Errorf("open minio object: %w", err))
		return
	}
	defer closeQuietly(reader)

	rootFolderID, rootFolderSource := s.resolveDriveRootFolderID(ctx)
	s.Logger.Info("drive sync upload starting",
		slog.String("attachment_id", job.Attachment.ID.String()),
		slog.String("post_id", job.Attachment.PostID.String()),
		slog.String("object_key", job.Attachment.ObjectKey),
		slog.String("root_folder_id", rootFolderID),
		slog.String("root_folder_source", rootFolderSource),
		slog.Bool("has_root_folder_id", rootFolderID != ""),
		slog.String("content_type", job.Attachment.ContentType),
	)
	result, err := s.Drive.UploadVideo(
		ctx,
		rootFolderID,
		s.driveFileName(job.Attachment),
		job.Attachment.ContentType,
		reader,
	)
	if err != nil {
		s.failJob(ctx, job, err)
		return
	}
	if err := s.Repo.MarkAttachmentDriveSyncUploaded(
		ctx,
		job.Attachment.ID,
		result.FileID,
		result.FolderID,
		result.WebViewLink,
		result.WebContentLink,
	); err != nil {
		s.Logger.Error("mark drive sync uploaded",
			slog.String("attachment_id", job.Attachment.ID.String()),
			slog.String("err", err.Error()),
		)
		return
	}
	s.Logger.Info("drive sync upload completed",
		slog.String("attachment_id", job.Attachment.ID.String()),
		slog.String("post_id", job.Attachment.PostID.String()),
		slog.String("drive_file_id", result.FileID),
		slog.String("root_folder_id", rootFolderID),
		slog.String("root_folder_source", rootFolderSource),
		slog.Bool("drive_folder_id_present", result.FolderID != nil && strings.TrimSpace(*result.FolderID) != ""),
	)
}

func (s *DriveSyncService) failJob(ctx context.Context, job repository.AttachmentDriveSyncJob, err error) {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "google drive sync failed"
	}
	if s.isPermanentFailure(message) {
		if repoErr := s.Repo.MarkAttachmentDriveSyncBlocked(ctx, job.Attachment.ID, message); repoErr != nil {
			s.Logger.Error("mark drive sync blocked",
				slog.String("attachment_id", job.Attachment.ID.String()),
				slog.String("err", repoErr.Error()),
			)
			return
		}
		s.Logger.Warn("drive sync blocked",
			slog.String("attachment_id", job.Attachment.ID.String()),
			slog.String("post_id", job.Attachment.PostID.String()),
			slog.String("err", message),
		)
		return
	}
	nextAttemptAt := time.Now().Add(s.retryDelay(job.Sync.AttemptCount))
	if repoErr := s.Repo.MarkAttachmentDriveSyncFailed(ctx, job.Attachment.ID, message, nextAttemptAt); repoErr != nil {
		s.Logger.Error("mark drive sync failed",
			slog.String("attachment_id", job.Attachment.ID.String()),
			slog.String("err", repoErr.Error()),
		)
		return
	}
	s.Logger.Warn("drive sync failed",
		slog.String("attachment_id", job.Attachment.ID.String()),
		slog.String("post_id", job.Attachment.PostID.String()),
		slog.String("err", message),
		slog.Time("retry_at", nextAttemptAt),
	)
}

func (s *DriveSyncService) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := s.BaseDelay
	if base <= 0 {
		base = time.Minute
	}
	maxDelay := s.MaxDelay
	if maxDelay <= 0 {
		maxDelay = time.Hour
	}
	factor := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * factor)
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func (s *DriveSyncService) driveFileName(attachment repository.PostAttachment) string {
	return attachment.PostID.String() + "_" + attachment.ID.String() + "_" + attachment.FileName
}

func (s *DriveSyncService) resolveDriveRootFolderID(ctx context.Context) (string, string) {
	items, err := s.Repo.ListSystemSettingsByKeys(ctx, []string{DriveRootFolderSettingKey})
	if err != nil {
		s.Logger.Warn("resolve drive root folder id", slog.String("err", err.Error()))
		return s.DefaultRootFolderID, "env_fallback_after_db_error"
	}
	if value := strings.TrimSpace(items[DriveRootFolderSettingKey]); value != "" {
		return value, "system_settings"
	}
	if s.DefaultRootFolderID != "" {
		return s.DefaultRootFolderID, "env_default"
	}
	return "", "empty"
}

func (s *DriveSyncService) isPermanentFailure(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "storagequotaexceeded"):
		return true
	case strings.Contains(lower, "service_disabled"):
		return true
	case strings.Contains(lower, "accessnotconfigured"):
		return true
	case strings.Contains(lower, "invalid_grant"):
		return true
	case strings.Contains(lower, "no google drive connection configured"):
		return true
	case strings.Contains(lower, "google drive oauth chưa được cấu hình"):
		return true
	default:
		return false
	}
}

func closeQuietly(reader io.ReadCloser) {
	_ = reader.Close()
}
