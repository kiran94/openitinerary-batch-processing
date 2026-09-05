package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/lmittmann/tint"
)

type Logger interface {
	Debug(message string, args ...any)
	Info(message string, args ...any)
	Warn(message string, args ...any)
	Error(message string, args ...any)
}

type Component string

const (
	ComponentDatabase  Component = "database"
	ComponentHTTP      Component = "http"
	ComponentQueue     Component = "queue"
	ComponentStorage   Component = "storage"
	ComponentWorker    Component = "worker"
	ComponentNotifier  Component = "notifier"
	ComponentTelemetry Component = "telemetry"
)

type Operation string

const (
	OperationSubmit             Operation = "submit"
	OperationEnqueue            Operation = "enqueue"
	OperationMarkFailed         Operation = "mark_failed"
	OperationErrorResponseWrite Operation = "response_write"
	OperationErrorWriteFailed   Operation = "error_response_write"
)

type SlogLogger struct {
	logger *slog.Logger
}

func New(config config.Config) Logger {
	return newLogger(os.Stdout, config.GetEnvironment(), config.GetLoggingLevel())
}

func newLogger(output io.Writer, environment string, level slog.Level) Logger {
	var handler slog.Handler
	if environment == "production" {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
				// Ensure that Production logs always output in UTC time.
				if attr.Key == slog.TimeKey {
					return slog.Time(slog.TimeKey, attr.Value.Time().UTC())
				}

				return attr
			},
		})
	} else {
		handler = tint.NewTextHandler(output, &tint.Options{Level: level})
	}

	return &SlogLogger{logger: slog.New(handler)}
}

func (l *SlogLogger) Debug(message string, args ...any) {
	l.logger.Debug(message, args...)
}

func (l *SlogLogger) Info(message string, args ...any) {
	l.logger.Info(message, args...)
}

func (l *SlogLogger) Warn(message string, args ...any) {
	l.logger.Warn(message, args...)
}

func (l *SlogLogger) Error(message string, args ...any) {
	l.logger.Error(message, args...)
}

// Context Helpers

type contextValue string

const (
	contextValueRequestID contextValue = "contextValueRequestID"
	contextValueTicketID  contextValue = "contextValueTicketID"
	contextValueWorkerID  contextValue = "contextValueWorkerID"
)

func WithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, contextValueRequestID, rid)
}

func GetRequestID(ctx context.Context) (string, bool) {
	return contextGetStringValue(ctx, contextValueRequestID)
}

func WithTicketID(ctx context.Context, tid string) context.Context {
	return context.WithValue(ctx, contextValueTicketID, tid)
}

func GetTicketID(ctx context.Context) (string, bool) {
	return contextGetStringValue(ctx, contextValueTicketID)
}

func WithWorkerID(ctx context.Context, wid string) context.Context {
	return context.WithValue(ctx, contextValueWorkerID, wid)
}

func GetWorkerID(ctx context.Context) (string, bool) {
	return contextGetStringValue(ctx, contextValueWorkerID)
}

func contextGetStringValue(ctx context.Context, key any) (string, bool) {
	if v := ctx.Value(key); v != nil {
		convertedValue, ok := v.(string)
		if ok {
			return convertedValue, true
		}
	}

	return "", false
}
