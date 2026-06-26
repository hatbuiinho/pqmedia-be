package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a slog.Logger configured for the given environment.
// Production uses JSON for log shippers; everything else gets human-readable text.
func New(appEnv string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if strings.EqualFold(appEnv, "production") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
