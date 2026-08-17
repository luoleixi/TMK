package observability

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
)

func ConfigureLogging(environment string) {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler).With("service", "tmk-glance", "environment", environment)
	slog.SetDefault(logger)
	log.SetFlags(0)
	log.SetOutput(logWriter{logger: logger})
}

type logWriter struct{ logger *slog.Logger }

func (w logWriter) Write(data []byte) (int, error) {
	w.logger.Info(strings.TrimSpace(string(data)))
	return len(data), nil
}

func Logger(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok && id != "" {
		return slog.Default().With("request_id", id)
	}
	return slog.Default()
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

var _ io.Writer = logWriter{}
