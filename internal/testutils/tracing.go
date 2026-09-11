package testutils

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

type MockTracer struct {
	trace.Tracer
	spans []*MockSpan
}

type MockSpan struct {
	trace.Span
	recordedErrors []error
	status         codes.Code
}

func NewMockTracer() *MockTracer {
	return &MockTracer{Tracer: tracenoop.NewTracerProvider().Tracer("test")}
}

func (m *MockTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	_, span := m.Tracer.Start(ctx, name, opts...)
	mockSpan := &MockSpan{Span: span}
	m.spans = append(m.spans, mockSpan)
	return ctx, mockSpan
}

func (m *MockSpan) RecordError(err error, _ ...trace.EventOption) {
	m.recordedErrors = append(m.recordedErrors, err)
}

func (m *MockSpan) SetStatus(code codes.Code, _ string) {
	m.status = code
}

func (m *MockSpan) RecordedErrors() []error {
	return append([]error(nil), m.recordedErrors...)
}

func (m *MockSpan) Status() codes.Code {
	return m.status
}
