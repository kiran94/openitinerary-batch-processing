package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

type Logger interface {
	Debug(ctx context.Context, message string, args ...any)
	Info(ctx context.Context, message string, args ...any)
	Warn(ctx context.Context, message string, args ...any)
	Error(ctx context.Context, message string, args ...any)
	WithContext(ctx context.Context) Logger
}

type SlogLogger struct {
	logger *slog.Logger
}

type minimumLevelHandler struct {
	level   slog.Level
	handler slog.Handler
}

func (h *minimumLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.handler.Enabled(ctx, level)
}

func (h *minimumLevelHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < h.level {
		return nil
	}

	return h.handler.Handle(ctx, record)
}

func (h *minimumLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &minimumLevelHandler{
		level:   h.level,
		handler: h.handler.WithAttrs(attrs),
	}
}

func (h *minimumLevelHandler) WithGroup(name string) slog.Handler {
	return &minimumLevelHandler{
		level:   h.level,
		handler: h.handler.WithGroup(name),
	}
}

type loggingConfig interface {
	GetEnvironment() string
	GetLoggingLevel() slog.Level
	GetOtelEnabled() bool
	GetOtelHandlerName() string
}

func New(config loggingConfig) Logger {
	return newLogger(
		os.Stdout,
		config.GetEnvironment(),
		config.GetLoggingLevel(),
		config.GetOtelEnabled(),
		config.GetOtelHandlerName(),
	)
}

func newLogger(output io.Writer, environment string, level slog.Level, otelEnabled bool, otelHandlerName string) Logger {
	handlers := make([]slog.Handler, 0, 2)

	if environment == "production" {
		handlers = append(handlers, slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
				// Ensure that Production logs always output in UTC time.
				if attr.Key == slog.TimeKey {
					return slog.Time(slog.TimeKey, attr.Value.Time().UTC())
				}

				return attr
			},
		}))

	} else {
		handlers = append(handlers, tint.NewTextHandler(output, &tint.Options{Level: level}))
	}

	if otelEnabled {
		handler := otelslog.NewHandler(otelHandlerName)
		handlers = append(handlers, &minimumLevelHandler{level: level, handler: handler})
	}

	handler := slog.NewMultiHandler(handlers...)
	return &SlogLogger{logger: slog.New(handler)}
}

func (l *SlogLogger) Debug(ctx context.Context, message string, args ...any) {
	l.logger.DebugContext(ctx, message, args...)
}

func (l *SlogLogger) Info(ctx context.Context, message string, args ...any) {
	l.logger.InfoContext(ctx, message, args...)
}

func (l *SlogLogger) Warn(ctx context.Context, message string, args ...any) {
	l.logger.WarnContext(ctx, message, args...)
}

func (l *SlogLogger) Error(ctx context.Context, message string, args ...any) {
	l.logger.ErrorContext(ctx, message, args...)
}

func (l *SlogLogger) WithContext(ctx context.Context) Logger {
	return newContextLogger(l, ctx)
}
