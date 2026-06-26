// Package push wraps the webpush-go client. Senders are called fire-and-forget
// from the notification service — failures log but do not block business logic.
package push

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/repository"
)

// Payload is the JSON delivered to the service worker.
type Payload struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Tag   string         `json:"tag,omitempty"`
	URL   string         `json:"url,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type Sender struct {
	cfg    config.WebPushConfig
	repo   *repository.Repo
	logger *slog.Logger
}

func NewSender(cfg config.WebPushConfig, repo *repository.Repo, logger *slog.Logger) *Sender {
	return &Sender{cfg: cfg, repo: repo, logger: logger}
}

// SendToUser delivers payload to every active subscription owned by recipientUserID.
// Subscriptions that return HTTP 404/410 are pruned automatically (browser revoked).
func (s *Sender) SendToUser(ctx context.Context, recipientUserID uuid.UUID, payload Payload) {
	if !s.cfg.Enabled() {
		return
	}
	subs, err := s.repo.ActivePushSubscriptionsForUser(ctx, recipientUserID)
	if err != nil {
		s.logger.Error("push list subs", slog.String("err", err.Error()))
		return
	}
	if len(subs) == 0 {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("push marshal", slog.String("err", err.Error()))
		return
	}

	for _, sub := range subs {
		s.sendOne(ctx, sub, body)
	}
}

func (s *Sender) sendOne(ctx context.Context, sub repository.PushSubscription, body []byte) {
	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      s.cfg.Subject,
		VAPIDPublicKey:  s.cfg.PublicKey,
		VAPIDPrivateKey: s.cfg.PrivateKey,
		TTL:             60,
	})
	if err != nil {
		s.logger.Warn("push send err", slog.String("endpoint", sub.Endpoint), slog.String("err", err.Error()))
		return
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if err := s.repo.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint); err != nil {
			s.logger.Warn("push prune", slog.String("err", err.Error()))
		}
		return
	}
	if resp.StatusCode >= 400 {
		s.logger.Warn("push send status",
			slog.String("endpoint", sub.Endpoint),
			slog.Int("status", resp.StatusCode),
		)
	}
}

func drainAndClose(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
