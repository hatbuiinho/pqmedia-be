package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/database"
	"pqmedia/be/internal/httpserver"
)

type App struct {
	server *http.Server
	db     *pgxpool.Pool
	logger *slog.Logger
	bgCtx  context.Context
	bgStop context.CancelFunc
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}

	svc, err := httpserver.NewServicesFromDB(ctx, db, cfg, logger)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init services: %w", err)
	}
	handler := httpserver.NewRouterWithServices(db, cfg, logger, svc)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	bgCtx, bgStop := context.WithCancel(context.Background())
	if svc.DriveSync != nil {
		go svc.DriveSync.Start(bgCtx)
	}

	return &App{server: server, db: db, logger: logger, bgCtx: bgCtx, bgStop: bgStop}, nil
}

func (a *App) Run() error {
	a.logger.Info("http listen", slog.String("addr", a.server.Addr))
	if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

func (a *App) Close() {
	if a.bgStop != nil {
		a.bgStop()
	}
	if a.db != nil {
		a.db.Close()
	}
}
