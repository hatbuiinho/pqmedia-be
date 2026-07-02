package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	googleoauth2 "golang.org/x/oauth2/google"
	oauth2api "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

type GoogleDriveConnectionStatus struct {
	OAuthReady     bool
	Connected      bool
	ConnectedEmail *string
	ConnectedAt    *time.Time
	LastError      *string
}

type GoogleDriveOAuthService struct {
	Repo             *repository.Repo
	Drive            *storage.GoogleDrive
	Logger           *slog.Logger
	OAuthConfig      oauth2.Config
	StateSecret      string
	PostConnectURL   string
	EncryptionSecret string
}

type googleDriveOAuthState struct {
	UserID    string `json:"uid"`
	ExpiresAt int64  `json:"exp"`
}

func NewGoogleDriveOAuthService(repo *repository.Repo, drive *storage.GoogleDrive, cfg config.GoogleDriveConfig, jwtSecret string, allowedOrigins []string, logger *slog.Logger) *GoogleDriveOAuthService {
	postConnectURL := strings.TrimSpace(cfg.OAuthPostConnectURL)
	if postConnectURL == "" && len(allowedOrigins) > 0 {
		postConnectURL = strings.TrimRight(allowedOrigins[0], "/") + "/settings/drive"
	}
	return &GoogleDriveOAuthService{
		Repo:   repo,
		Drive:  drive,
		Logger: logger,
		OAuthConfig: oauth2.Config{
			ClientID:     strings.TrimSpace(cfg.OAuthClientID),
			ClientSecret: strings.TrimSpace(cfg.OAuthClientSecret),
			RedirectURL:  strings.TrimSpace(cfg.OAuthRedirectURL),
			Scopes: []string{
				"https://www.googleapis.com/auth/drive",
				"https://www.googleapis.com/auth/userinfo.email",
			},
			Endpoint: googleoauth2.Endpoint,
		},
		StateSecret:      jwtSecret,
		PostConnectURL:   postConnectURL,
		EncryptionSecret: jwtSecret,
	}
}

func (s *GoogleDriveOAuthService) OAuthReady() bool {
	return strings.TrimSpace(s.OAuthConfig.ClientID) != "" &&
		strings.TrimSpace(s.OAuthConfig.ClientSecret) != "" &&
		strings.TrimSpace(s.OAuthConfig.RedirectURL) != "" &&
		strings.TrimSpace(s.PostConnectURL) != ""
}

func (s *GoogleDriveOAuthService) GetConnectionStatus(ctx context.Context) (GoogleDriveConnectionStatus, error) {
	status := GoogleDriveConnectionStatus{OAuthReady: s.OAuthReady()}
	item, err := s.Repo.GetGoogleDriveOAuthConnection(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return status, nil
		}
		return GoogleDriveConnectionStatus{}, err
	}
	status.Connected = true
	status.ConnectedEmail = stringPtr(strings.TrimSpace(item.GoogleEmail))
	status.ConnectedAt = &item.ConnectedAt
	status.LastError = item.LastError
	return status, nil
}

func (s *GoogleDriveOAuthService) StartConnect(ctx context.Context, actor Principal) (string, error) {
	if !actor.User.IsAdmin {
		return "", ErrForbidden
	}
	if !s.OAuthReady() {
		return "", NewError(400, "google_drive_oauth_not_configured", "Google Drive OAuth chưa được cấu hình ở server")
	}
	state, err := s.signState(actor.User.ID)
	if err != nil {
		return "", err
	}
	return s.OAuthConfig.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	), nil
}

func (s *GoogleDriveOAuthService) HandleCallback(ctx context.Context, state, code, oauthErr string) (string, error) {
	if !s.OAuthReady() {
		return s.redirectWithResult("error", "Google Drive OAuth chưa được cấu hình ở server"), nil
	}
	if oauthErr != "" {
		return s.redirectWithResult("error", "Google Drive authorization was cancelled or denied"), nil
	}
	userID, err := s.verifyState(state)
	if err != nil {
		return s.redirectWithResult("error", "Google Drive OAuth state is invalid or expired"), nil
	}
	if strings.TrimSpace(code) == "" {
		return s.redirectWithResult("error", "Google Drive authorization code is missing"), nil
	}

	token, err := s.OAuthConfig.Exchange(ctx, code)
	if err != nil {
		s.Logger.Warn("google drive oauth exchange failed", slog.String("err", err.Error()))
		return s.redirectWithResult("error", "Không đổi được Google Drive authorization code"), nil
	}

	refreshToken := strings.TrimSpace(token.RefreshToken)
	if refreshToken == "" {
		return s.redirectWithResult("error", "Google không trả về refresh token. Hãy thử kết nối lại và chấp thuận lại quyền."), nil
	}

	email, err := s.lookupGoogleAccountEmail(ctx, token)
	if err != nil {
		s.Logger.Warn("google drive oauth userinfo failed", slog.String("err", err.Error()))
		return s.redirectWithResult("error", "Không lấy được email tài khoản Google đã kết nối"), nil
	}

	encryptedRefreshToken, err := encryptSecret(s.EncryptionSecret, refreshToken)
	if err != nil {
		return "", err
	}

	if err := s.Repo.UpsertGoogleDriveOAuthConnection(ctx, repository.UpsertGoogleDriveOAuthConnectionParams{
		GoogleEmail:           email,
		EncryptedRefreshToken: encryptedRefreshToken,
		Scope:                 strings.TrimSpace(resolveTokenScope(token)),
		TokenType:             stringPtr(token.TokenType),
		ConnectedByUserID:     userID,
		ConnectedAt:           time.Now(),
	}); err != nil {
		return "", err
	}
	_ = s.Repo.RequeueAttachmentDriveSyncs(ctx, []repository.AttachmentDriveSyncStatus{
		repository.AttachmentDriveSyncBlocked,
		repository.AttachmentDriveSyncFailed,
	})

	return s.redirectWithResult("connected", "Google Drive đã được kết nối"), nil
}

func (s *GoogleDriveOAuthService) Disconnect(ctx context.Context, actor Principal) error {
	if !actor.User.IsAdmin {
		return ErrForbidden
	}
	return s.Repo.ClearGoogleDriveOAuthConnection(ctx)
}

func (s *GoogleDriveOAuthService) UploadVideo(
	ctx context.Context,
	folderID string,
	fileName string,
	contentType string,
	reader io.Reader,
) (storage.DriveUploadResult, error) {
	if s.Drive == nil {
		return storage.DriveUploadResult{}, fmt.Errorf("google drive uploader is not configured")
	}
	connection, err := s.Repo.GetGoogleDriveOAuthConnection(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return storage.DriveUploadResult{}, fmt.Errorf("no google drive connection configured")
		}
		return storage.DriveUploadResult{}, err
	}
	refreshToken, err := decryptSecret(s.EncryptionSecret, connection.EncryptedRefreshToken)
	if err != nil {
		return storage.DriveUploadResult{}, fmt.Errorf("decrypt google drive refresh token: %w", err)
	}
	tokenSource := s.OAuthConfig.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	if _, err := tokenSource.Token(); err != nil {
		_ = s.Repo.MarkGoogleDriveOAuthConnectionError(ctx, strings.TrimSpace(err.Error()))
		return storage.DriveUploadResult{}, fmt.Errorf("refresh google drive access token: %w", err)
	}
	if err := s.Repo.MarkGoogleDriveOAuthConnectionRefreshed(ctx); err != nil {
		s.Logger.Warn("mark google drive oauth connection refreshed", slog.String("err", err.Error()))
	}
	return s.Drive.UploadVideo(ctx, tokenSource, folderID, fileName, contentType, reader)
}

func (s *GoogleDriveOAuthService) signState(userID uuid.UUID) (string, error) {
	payload, err := json.Marshal(googleDriveOAuthState{
		UserID:    userID.String(),
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal google drive oauth state: %w", err)
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(s.StateSecret))
	_, _ = mac.Write([]byte(payloadEncoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadEncoded + "." + signature, nil
}

func (s *GoogleDriveOAuthService) verifyState(raw string) (uuid.UUID, error) {
	payloadEncoded, signatureEncoded, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || payloadEncoded == "" || signatureEncoded == "" {
		return uuid.Nil, fmt.Errorf("invalid oauth state")
	}
	mac := hmac.New(sha256.New, []byte(s.StateSecret))
	_, _ = mac.Write([]byte(payloadEncoded))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(signatureEncoded)
	if err != nil || !hmac.Equal(actual, expected) {
		return uuid.Nil, fmt.Errorf("invalid oauth state signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadEncoded)
	if err != nil {
		return uuid.Nil, fmt.Errorf("decode oauth state: %w", err)
	}
	var state googleDriveOAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return uuid.Nil, fmt.Errorf("unmarshal oauth state: %w", err)
	}
	if state.ExpiresAt < time.Now().Unix() {
		return uuid.Nil, fmt.Errorf("oauth state expired")
	}
	userID, err := uuid.Parse(state.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse oauth user id: %w", err)
	}
	return userID, nil
}

func (s *GoogleDriveOAuthService) lookupGoogleAccountEmail(ctx context.Context, token *oauth2.Token) (string, error) {
	service, err := oauth2api.NewService(ctx, option.WithTokenSource(s.OAuthConfig.TokenSource(ctx, token)))
	if err != nil {
		return "", fmt.Errorf("init google oauth2 service: %w", err)
	}
	info, err := service.Userinfo.Get().Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("get google userinfo: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(info.Email))
	if email == "" {
		return "", fmt.Errorf("google account email is empty")
	}
	return email, nil
}

func (s *GoogleDriveOAuthService) redirectWithResult(status, message string) string {
	u, err := url.Parse(s.PostConnectURL)
	if err != nil {
		return s.PostConnectURL
	}
	query := u.Query()
	query.Set("google_drive", status)
	if strings.TrimSpace(message) != "" {
		query.Set("message", message)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func encryptSecret(secret, plain string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("init gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptSecret(secret, encrypted string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("init gcm: %w", err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	nonce, payload := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func resolveTokenScope(token *oauth2.Token) string {
	if token == nil {
		return ""
	}
	raw := token.Extra("scope")
	switch value := raw.(type) {
	case string:
		return value
	case []string:
		return strings.Join(value, " ")
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return strings.Join(out, " ")
	default:
		return ""
	}
}
