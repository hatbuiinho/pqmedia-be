package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/handlers"
	"pqmedia/be/internal/push"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/service"
	"pqmedia/be/internal/storage"
)

// Services bundles wired-up services for handler injection.
type Services struct {
	User         *service.UserService
	Post         *service.PostService
	Comment      *service.CommentService
	Reaction     *service.ReactionService
	Publication  *service.PublicationService
	Notification *service.NotificationService
	Storage      *storage.MinIO
}

func NewServices(repo *repository.Repo, store *storage.MinIO, cfg config.Config, logger *slog.Logger) Services {
	notif := &service.NotificationService{
		Repo:   repo,
		Sender: push.NewSender(cfg.WebPush, repo, logger),
		Logger: logger,
	}
	return Services{
		Storage: store,
		User: &service.UserService{
			Repo:            repo,
			JWTSecret:       cfg.JWTSecret,
			AccessTokenTTL:  cfg.AccessTokenTTL,
			RefreshTokenTTL: cfg.RefreshTokenTTL,
		},
		Post:         &service.PostService{Repo: repo, Storage: store},
		Comment:      &service.CommentService{Repo: repo, Storage: store, Notification: notif},
		Reaction:     &service.ReactionService{Repo: repo, Storage: store, Notification: notif},
		Publication:  &service.PublicationService{Repo: repo},
		Notification: notif,
	}
}

func NewRouter(db *pgxpool.Pool, cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	repo := repository.New(db)
	store, err := storage.NewMinIO(cfg.MinIO)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	svc := NewServices(repo, store, cfg, logger)

	authHandler := handlers.AuthHandler{Service: svc.User}
	userHandler := handlers.UserHandler{Service: svc.User}
	postHandler := handlers.PostHandler{Service: svc.Post}
	commentHandler := handlers.CommentHandler{Service: svc.Comment}
	reactionHandler := handlers.ReactionHandler{Service: svc.Reaction}
	publicationHandler := handlers.PublicationHandler{Service: svc.Publication}
	notificationHandler := handlers.NotificationHandler{Service: svc.Notification, VAPIDKey: cfg.WebPush.PublicKey}
	uploadHandler := handlers.UploadHandler{Storage: svc.Storage}
	healthHandler := handlers.HealthHandler{DB: db}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(requestLogger(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", healthHandler.ServeHTTP)
	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/refresh", authHandler.Refresh)

	r.Group(func(p chi.Router) {
		p.Use(Authentication(svc.User))

		p.Get("/me", authHandler.Me)
		p.Patch("/me/profile", userHandler.UpdateOwnProfile)
		p.Post("/auth/logout", authHandler.Logout)

		p.Get("/users", userHandler.List)
		p.Post("/users", userHandler.Create)
		p.Patch("/users/{userID}/profile", userHandler.UpdateProfile)

		p.Post("/uploads/presign", uploadHandler.Presign)

		p.Get("/feed", postHandler.ListFeed)
		p.Post("/posts", postHandler.Create)
		p.Get("/posts/{postID}", postHandler.Get)
		p.Patch("/posts/{postID}", postHandler.Update)
		p.Delete("/posts/{postID}", postHandler.Delete)

		p.Get("/hashtags", postHandler.SearchHashtags)

		p.Get("/posts/{postID}/comments", commentHandler.ListByPost)
		p.Post("/posts/{postID}/comments", commentHandler.Create)
		p.Delete("/comments/{commentID}", commentHandler.Delete)

		p.Get("/reactions", reactionHandler.List)
		p.Get("/reactions/details", reactionHandler.GetDetails)
		p.Post("/reactions", reactionHandler.Toggle)

		p.Get("/posts/{postID}/publications", publicationHandler.List)
		p.Put("/posts/{postID}/publications/{platform}", publicationHandler.Upsert)
		p.Delete("/posts/{postID}/publications/{platform}", publicationHandler.Delete)

		p.Get("/notifications", notificationHandler.List)
		p.Post("/notifications/{id}/read", notificationHandler.MarkRead)
		p.Post("/notifications/read-all", notificationHandler.MarkAllRead)
		p.Get("/push/vapid-public-key", notificationHandler.VAPIDPublicKey)
		p.Post("/push-subscriptions", notificationHandler.UpsertSubscription)
		p.Delete("/push-subscriptions", notificationHandler.DisableSubscription)
	})

	return r, nil
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Duration("dur", time.Since(start)),
				slog.String("req_id", middleware.GetReqID(r.Context())),
			)
		})
	}
}
