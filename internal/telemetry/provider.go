package telemetry

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type MeterManager struct {
	mp *metric.MeterProvider
}

func NewMeterManager(ctx context.Context, cfg config.Config) (*MeterManager, error) {
	resource, err := createResource(cfg)
	if err != nil {
		return nil, err
	}

	// Uses the standard env var: OTEL_EXPORTER_OTLP_ENDPOINT
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	metricProvider := metric.NewMeterProvider(
		metric.WithResource(resource),
		metric.WithReader(metric.NewPeriodicReader(exporter, metric.WithInterval(cfg.GetOtelMetricExportInterval()))),
	)

	return &MeterManager{metricProvider}, nil
}

func (m *MeterManager) GetProvider() *metric.MeterProvider {
	return m.mp
}

func (m *MeterManager) Close(ctx context.Context) error {
	return m.mp.Shutdown(ctx)
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
