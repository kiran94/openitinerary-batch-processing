package telemetry

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

type MeterManager struct {
	mp metric.MeterProvider
}

func NewMeterManager(ctx context.Context, cfg config.Config) (*MeterManager, error) {

	if !cfg.GetOtelEnabled() {
		p := noop.NewMeterProvider()
		return &MeterManager{mp: p}, nil
	}

	resource, err := createResource(cfg)
	if err != nil {
		return nil, err
	}

	// Uses the standard env var: OTEL_EXPORTER_OTLP_ENDPOINT
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(cfg.GetOtelMetricExportInterval()))),
	)

	return &MeterManager{metricProvider}, nil
}

func (m *MeterManager) GetProvider() metric.MeterProvider {
	return m.mp
}

func (m *MeterManager) Close(ctx context.Context) error {
	p, ok := m.mp.(*sdkmetric.MeterProvider)
	if ok {
		return p.Shutdown(ctx)
	}

	return nil
}

func createResource(cfg config.Config) (*resource.Resource, error) {
	resource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(cfg.GetOtelServiceName()), semconv.ServiceVersion(cfg.GetOtelServiceVersion())),
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}

func SetLogger(l logging.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(cause error) {
		l.Error("otel error", "error", cause.Error(), "component", logging.ComponentTelemetry)
	}))
}
