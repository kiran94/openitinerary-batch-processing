package logging

import "context"

type contextLogger struct {
	logger        Logger
	contextValues []any
}

func newContextLogger(parentLogger Logger, ctx context.Context) Logger {
	contextValues := []any{}

	if v, ok := GetRequestID(ctx); ok {
		contextValues = append(contextValues, "request_id")
		contextValues = append(contextValues, v)
	}

	if v, ok := GetTicketID(ctx); ok {
		contextValues = append(contextValues, "ticket_id")
		contextValues = append(contextValues, v)
	}

	if v, ok := GetWorkerID(ctx); ok {
		contextValues = append(contextValues, "worker_id")
		contextValues = append(contextValues, v)
	}

	return &contextLogger{logger: parentLogger, contextValues: contextValues}
}

func (l *contextLogger) Debug(message string, args ...any) {
	args = append(args, l.contextValues...)
	l.logger.Debug(message, args...)
}

func (l *contextLogger) Info(message string, args ...any) {
	args = append(args, l.contextValues...)
	l.logger.Info(message, args...)
}

func (l *contextLogger) Warn(message string, args ...any) {
	args = append(args, l.contextValues...)
	l.logger.Warn(message, args...)
}

func (l *contextLogger) Error(message string, args ...any) {
	args = append(args, l.contextValues...)
	l.logger.Error(message, args...)
}

func (l *contextLogger) WithContext(ctx context.Context) Logger {
	return newContextLogger(l.logger, ctx)
}

// Context Helpers

type contextValue string

const (
	contextValueRequestID contextValue = "contextValueRequestID"
	contextValueTicketID  contextValue = "contextValueTicketID"
	contextValueWorkerID  contextValue = "contextValueWorkerID"
)

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextValueRequestID, requestID)
}

func GetRequestID(ctx context.Context) (string, bool) {
	return contextGetStringValue(ctx, contextValueRequestID)
}

func WithTicketID(ctx context.Context, ticketID string) context.Context {
	return context.WithValue(ctx, contextValueTicketID, ticketID)
}

func GetTicketID(ctx context.Context) (string, bool) {
	return contextGetStringValue(ctx, contextValueTicketID)
}

func WithWorkerID(ctx context.Context, workerID string) context.Context {
	return context.WithValue(ctx, contextValueWorkerID, workerID)
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
