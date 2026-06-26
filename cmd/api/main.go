package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pqmedia/be/internal/app"
	"pqmedia/be/internal/config"
	"pqmedia/be/internal/logging"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		fatal(nil, "load config", err)
	}

	logger := logging.New(cfg.AppEnv)

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "build app", err)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- application.Run() }()

	select {
	case err := <-errCh:
		if err != nil {
			fatal(logger, "run app", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := application.Shutdown(shutdownCtx); err != nil {
			fatal(logger, "shutdown", err)
		}
		logger.Info("shutdown complete")
	}
}

func fatal(logger *slog.Logger, msg string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error(msg, slog.String("err", err.Error()))
	os.Exit(1)
}
