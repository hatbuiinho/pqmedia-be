// Package storage wraps MinIO/S3 access. Object keys follow the namespace:
//
//	posts/{userID}/{uuid}-{filename}     -- post attachments
//	avatars/{userID}/{uuid}-{filename}   -- user avatars
//
// Public URLs: when MINIO_PUBLIC_BASE_URL is set we use it verbatim; otherwise
// we fall back to {scheme}://{endpoint}/{bucket} so the dev compose stack works
// out of the box. Either way the bucket must allow anonymous GET.
package storage

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"pqmedia/be/internal/config"
)

const presignExpiry = 5 * time.Minute

type MinIO struct {
	client     *minio.Client
	endpoint   string
	bucket     string
	useSSL     bool
	publicBase string
}

func NewMinIO(cfg config.MinIOConfig) (*MinIO, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("minio endpoint and bucket are required")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}
	return &MinIO{
		client:     client,
		endpoint:   cfg.Endpoint,
		bucket:     cfg.Bucket,
		useSSL:     cfg.UseSSL,
		publicBase: strings.TrimRight(cfg.PublicBase, "/"),
	}, nil
}

func (m *MinIO) Bucket() string { return m.bucket }

// PresignedUpload bundles everything FE needs to PUT a file to MinIO.
type PresignedUpload struct {
	Bucket    string    `json:"bucket"`
	ObjectKey string    `json:"object_key"`
	UploadURL string    `json:"upload_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PresignUpload generates a PUT URL for the given prefix (e.g. "posts" or "avatars").
// The final key is `{prefix}/{userID}/{uuid}-{safeName}`.
func (m *MinIO) PresignUpload(ctx context.Context, prefix, userID, fileName string) (PresignedUpload, error) {
	key := buildObjectKey(prefix, userID, fileName)
	u, err := m.client.PresignedPutObject(ctx, m.bucket, key, presignExpiry)
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("presign upload: %w", err)
	}
	return PresignedUpload{
		Bucket:    m.bucket,
		ObjectKey: key,
		UploadURL: u.String(),
		ExpiresAt: time.Now().Add(presignExpiry),
	}, nil
}

// BuildPublicURL returns the public URL for an object. Uses MINIO_PUBLIC_BASE_URL
// when set, otherwise composes {scheme}://{endpoint}/{bucket}/{key} so the default
// docker-compose setup (bucket marked anonymous-read) just works.
func (m *MinIO) BuildPublicURL(objectKey string) string {
	if objectKey == "" {
		return ""
	}
	if m.publicBase != "" {
		return m.publicBase + "/" + objectKey
	}
	scheme := "http"
	if m.useSSL {
		scheme = "https"
	}
	return scheme + "://" + m.endpoint + "/" + m.bucket + "/" + objectKey
}

func buildObjectKey(prefix, userID, fileName string) string {
	safe := sanitizeFileName(fileName)
	return path.Join(prefix, userID, uuid.NewString()+"-"+safe)
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "file"
	}
	// URL-escape weird chars but leave the extension intact for FE consumers.
	name = strings.ReplaceAll(name, " ", "_")
	return url.PathEscape(name)
}
