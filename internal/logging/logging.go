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

func (l *SlogLogger) WithContext(ctx context.Context) Logger {
	return newContextLogger(l, ctx)
}
