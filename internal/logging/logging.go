package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
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

type Component string

const (
	ComponentDatabase  Component = "database"
	ComponentAdapter   Component = "adapter"
	ComponentHTTP      Component = "http"
	ComponentQueue     Component = "queue"
	ComponentStorage   Component = "storage"
	ComponentWorker    Component = "worker"
	ComponentNotifier  Component = "notifier"
	ComponentTelemetry Component = "telemetry"
)

func (c Component) LogValue() slog.Value {
	return slog.StringValue(string(c))
}

type Operation string

const (
	OperationSubmit                Operation = "submit"
	OperationEnqueue               Operation = "enqueue"
	OperationDequeue               Operation = "dequeue"
	OperationDeleteMessage         Operation = "delete_message"
	OperationMarkFailed            Operation = "mark_failed"
	OperationStorageRead           Operation = "storage_read"
	OperationStorageWrite          Operation = "storage_write"
	OperationStoragePresign        Operation = "storage_presign"
	OperationAdapterConvert        Operation = "adapter_convert"
	OperationNotificationSend      Operation = "notification_send"
	OperationTicketCreate          Operation = "ticket_create"
	OperationTicketLookup          Operation = "ticket_lookup"
	OperationTicketUpdate          Operation = "ticket_update"
	OperationDuplicateDelivery     Operation = "duplicate_delivery"
	OperationMultipartCleanup      Operation = "multipart_cleanup"
	OperationTelemetryError        Operation = "telemetry_error"
	OperationResponseSerialization Operation = "response_serialization"
	OperationErrorResponseWrite    Operation = "response_write"
	OperationErrorWriteFailed      Operation = "error_response_write"
)

func (o Operation) LogValue() slog.Value {
	return slog.StringValue(string(o))
}

type SlogLogger struct {
	logger *slog.Logger
}

func New(config config.Config) Logger {
	return newLogger(os.Stdout, config.GetEnvironment(), config.GetLoggingLevel(), config.GetOtelEnabled())
}

func newLogger(output io.Writer, environment string, level slog.Level, otelEnabled bool) Logger {
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
		handler := otelslog.NewHandler("github.com/kiran94/openitinerary-batch-processing/internal/logging")
		handlers = append(handlers, handler)
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
