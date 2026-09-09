package telemetry

import (
	"context"
	"errors"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	sdktrace "go.opentelemetry.io/otel/trace"
	sdktracenoop "go.opentelemetry.io/otel/trace/noop"
)

type TelemetryManager struct {
	mp  metric.MeterProvider    // Metrics
	lg  *log.LoggerProvider     // Logging
	tp  sdktrace.TracerProvider // Tracing
	cfg config.Config

	ready bool
}

func NewTelemetryManager(ctx context.Context, serviceName string, cfg config.Config) (*TelemetryManager, error) {

	if !cfg.GetOtelEnabled() {
		p := noop.NewMeterProvider()
		t := sdktracenoop.NewTracerProvider()
		return &TelemetryManager{mp: p, tp: t, cfg: cfg}, nil
	}

	resource, err := createResource(serviceName, cfg)
	if err != nil {
		return nil, err
	}

	metricProvider, err := newMetricsProvider(ctx, resource, cfg)
	if err != nil {
		return nil, err
	}

	loggerProvider, err := newLoggerProvider(ctx, resource, cfg)
	if err != nil {
		return nil, err
	}

	traceProvider, err := newTracingProvider(ctx, resource)
	if err != nil {
		return nil, err
	}

	return &TelemetryManager{metricProvider, loggerProvider, traceProvider, cfg, false}, nil
}

func newLoggerProvider(ctx context.Context, resource *resource.Resource, cfg config.Config) (*log.LoggerProvider, error) {
	exporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}

	batchExporter := log.NewBatchProcessor(exporter, log.WithExportInterval(cfg.GetOtelExportInterval()))

	return log.NewLoggerProvider(
		log.WithResource(resource),
		log.WithProcessor(batchExporter),
	), nil
}

func newMetricsProvider(ctx context.Context, resource *resource.Resource, cfg config.Config) (metric.MeterProvider, error) {
	// Uses the standard env var: OTEL_EXPORTER_OTLP_ENDPOINT
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExporter,
				sdkmetric.WithInterval(cfg.GetOtelExportInterval()),
			),
		),
	)

	return metricProvider, nil
}

func newTracingProvider(ctx context.Context, resource *resource.Resource) (sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithResource(resource),
		trace.WithBatcher(exporter),
	)

	return tp, nil
}

func (m *TelemetryManager) GetMeterProvider() metric.MeterProvider {
	return m.mp
}

func (m *TelemetryManager) GetTracerProvider() sdktrace.TracerProvider {
	return m.tp
}

func (m *TelemetryManager) Close(ctx context.Context) error {
	shutdownErrors := make([]error, 0, 3)

	if m.mp != nil {
		if p, ok := m.mp.(*sdkmetric.MeterProvider); ok {
			shutdownErrors = append(shutdownErrors, p.Shutdown(ctx))
		}
	}

	if m.lg != nil {
		shutdownErrors = append(shutdownErrors, m.lg.Shutdown(ctx))
	}

	if m.tp != nil {
		if t, ok := m.tp.(*trace.TracerProvider); ok {
			shutdownErrors = append(shutdownErrors, t.Shutdown(ctx))
		}
	}

	return errors.Join(shutdownErrors...)
}

func createResource(serviceName string, cfg config.Config) (*resource.Resource, error) {
	resource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName), semconv.ServiceVersion(cfg.GetOtelServiceVersion())),
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}

func SetLogger(l logging.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(cause error) {
		l.Error(
			"otel error",
			"component", logging.ComponentTelemetry,
			"operation", logging.OperationTelemetryError,
			"error", cause,
		)
	}))
}

func (m *TelemetryManager) Ready() error {
	if m.ready {
		return nil
	}

	if !m.cfg.GetOtelEnabled() {
		return nil
	}

	otel.SetMeterProvider(m.GetMeterProvider())
	otel.SetTracerProvider(m.tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	global.SetLoggerProvider(m.lg)

	// Go runtime memory metrics
	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(m.cfg.GetOtelExportInterval())); err != nil {
		return err
	}

	m.ready = true
	return nil
}
