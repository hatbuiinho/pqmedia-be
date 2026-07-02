package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppEnv          string
	HTTPAddr        string
	DatabaseURL     string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AllowedOrigins  []string
	MinIO           MinIOConfig
	GoogleDrive     GoogleDriveConfig
	WebPush         WebPushConfig
}

type MinIOConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	Bucket     string
	Region     string
	UseSSL     bool
	PublicBase string
}

type WebPushConfig struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

type GoogleDriveConfig struct {
	Enabled               bool
	OAuthClientID         string
	OAuthClientSecret     string
	OAuthRedirectURL      string
	OAuthPostConnectURL   string
	ServiceAccountJSON    string
	ServiceAccountFile    string
	RootFolderID          string
	SharedDriveID         string
	UploadTimeout         time.Duration
	SyncPollInterval      time.Duration
	SyncBatchSize         int
	FailureRetryBaseDelay time.Duration
	FailureRetryMaxDelay  time.Duration
}

func (c WebPushConfig) Enabled() bool {
	return c.PublicKey != "" && c.PrivateKey != "" && c.Subject != ""
}

func Load(dotenvPath string) (Config, error) {
	if err := loadDotEnv(dotenvPath); err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:          getEnv("APP_ENV", "development"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		AccessTokenTTL:  getDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		AllowedOrigins:  splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:5173")),
		MinIO: MinIOConfig{
			Endpoint:   os.Getenv("MINIO_ENDPOINT"),
			AccessKey:  os.Getenv("MINIO_ACCESS_KEY"),
			SecretKey:  os.Getenv("MINIO_SECRET_KEY"),
			Bucket:     os.Getenv("MINIO_BUCKET"),
			Region:     getEnv("MINIO_REGION", "us-east-1"),
			UseSSL:     getEnv("MINIO_USE_SSL", "false") == "true",
			PublicBase: os.Getenv("MINIO_PUBLIC_BASE_URL"),
		},
		GoogleDrive: GoogleDriveConfig{
			Enabled:               getEnv("GOOGLE_DRIVE_ENABLE_VIDEO_SYNC", "false") == "true",
			OAuthClientID:         os.Getenv("GOOGLE_DRIVE_OAUTH_CLIENT_ID"),
			OAuthClientSecret:     os.Getenv("GOOGLE_DRIVE_OAUTH_CLIENT_SECRET"),
			OAuthRedirectURL:      os.Getenv("GOOGLE_DRIVE_OAUTH_REDIRECT_URL"),
			OAuthPostConnectURL:   os.Getenv("GOOGLE_DRIVE_OAUTH_POST_CONNECT_URL"),
			ServiceAccountJSON:    os.Getenv("GOOGLE_DRIVE_SERVICE_ACCOUNT_JSON"),
			ServiceAccountFile:    os.Getenv("GOOGLE_DRIVE_SERVICE_ACCOUNT_FILE"),
			RootFolderID:          os.Getenv("GOOGLE_DRIVE_ROOT_FOLDER_ID"),
			SharedDriveID:         os.Getenv("GOOGLE_DRIVE_SHARED_DRIVE_ID"),
			UploadTimeout:         getDuration("GOOGLE_DRIVE_UPLOAD_TIMEOUT", 15*time.Minute),
			SyncPollInterval:      getDuration("GOOGLE_DRIVE_SYNC_POLL_INTERVAL", 15*time.Second),
			SyncBatchSize:         getInt("GOOGLE_DRIVE_SYNC_BATCH_SIZE", 4),
			FailureRetryBaseDelay: getDuration("GOOGLE_DRIVE_RETRY_BASE_DELAY", 1*time.Minute),
			FailureRetryMaxDelay:  getDuration("GOOGLE_DRIVE_RETRY_MAX_DELAY", 1*time.Hour),
		},
		WebPush: WebPushConfig{
			PublicKey:  os.Getenv("WEB_PUSH_VAPID_PUBLIC_KEY"),
			PrivateKey: os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY"),
			Subject:    os.Getenv("WEB_PUSH_VAPID_SUBJECT"),
		},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid env line: %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			return fmt.Errorf("invalid env key in line: %q", line)
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set env %s: %w", key, err)
			}
		}
	}
	return scanner.Err()
}
