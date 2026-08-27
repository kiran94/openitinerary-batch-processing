package logging

import (
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
	ComponentDatabase Component = "database"
	ComponentHTTP     Component = "http"
	ComponentQueue    Component = "queue"
	ComponentStorage  Component = "storage"
)

type SlogLogger struct {
	logger *slog.Logger
}

func New(config config.Config) Logger {
	return newLogger(os.Stdout, config.GetEnvironment())
}

func newLogger(output io.Writer, environment string) Logger {
	var handler slog.Handler
	if environment == "production" {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = tint.NewTextHandler(output, &tint.Options{Level: slog.LevelDebug})
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
