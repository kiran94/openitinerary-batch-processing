package telemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type ServiceMetrics interface {
	metrics.SubmissionRecorder
	metrics.StorageUploadRecorder
	metrics.ResponseWriteErrorRecorder
	metrics.JobTerminalRecorder
}

type ApplicationOtelMetrics struct {
	apiSubmissionsCounter        metric.Int64Counter
	jobsEnqueuedCounter          metric.Int64Counter
	documentInputSizeHisto       metric.Int64Histogram
	storageUploadDurationHisto   metric.Float64Histogram
	apiWriteResponseErrorCounter metric.Int64Counter
	jobTerminalCounter           metric.Int64Counter
}

type otelMetric string

const (
	otelMetricAPISubmissions         otelMetric = "oibp.api.submissions"
	otelMetricJobsEnqueued           otelMetric = "oibp.jobs.enqueued"
	otelMetricDocumentInputSize      otelMetric = "oibp.document.input.size"
	otelMetricStorageUploadDuration  otelMetric = "oibp.storage.upload.duration"
	otelMetricAPIResponseWriteErrors otelMetric = "oibp.api.response_write.errors"
	otelMetricJobTerminal            otelMetric = "oibp.jobs.terminal"
)

func NewApplicationOtelMetrics(mp metric.MeterProvider) (*ApplicationOtelMetrics, error) {
	meter := mp.Meter("github.com/kiran94/openitinerary-batch-processing/internal/telemetry")

	apiSubmissionsCounter, err := meter.Int64Counter(string(otelMetricAPISubmissions), metric.WithUnit("1"), metric.WithDescription("tracks submissions to the api"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricAPISubmissions), err)
	}

	jobsEnqueuedCounter, err := meter.Int64Counter(string(otelMetricJobsEnqueued), metric.WithUnit("1"), metric.WithDescription("tracks jobs enqueued"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricJobsEnqueued), err)
	}

	documentInputSizeHisto, err := meter.Int64Histogram(string(otelMetricDocumentInputSize), metric.WithUnit("By"), metric.WithDescription("tracks uploaded document sizes"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricDocumentInputSize), err)
	}

	storageUploadDurationHisto, err := meter.Float64Histogram(string(otelMetricStorageUploadDuration), metric.WithUnit("s"), metric.WithDescription("tracks upload times to storage"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricStorageUploadDuration), err)
	}

	apiWriteResponseErrorCounter, err := meter.Int64Counter(string(otelMetricAPIResponseWriteErrors), metric.WithUnit("1"), metric.WithDescription("tracks errors that happen during api response write"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricAPIResponseWriteErrors), err)
	}

	jobTerminalCounter, err := meter.Int64Counter(string(otelMetricJobTerminal), metric.WithUnit("1"), metric.WithDescription("tracks the number of jobs which have terminated"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricJobTerminal), err)
	}

	return &ApplicationOtelMetrics{
		apiSubmissionsCounter:        apiSubmissionsCounter,
		jobsEnqueuedCounter:          jobsEnqueuedCounter,
		documentInputSizeHisto:       documentInputSizeHisto,
		storageUploadDurationHisto:   storageUploadDurationHisto,
		apiWriteResponseErrorCounter: apiWriteResponseErrorCounter,
		jobTerminalCounter:           jobTerminalCounter,
	}, nil

}

func (a *ApplicationOtelMetrics) RecordSubmission(ctx context.Context, m metrics.SubmissionObservation) {

	// oibp.api.submissions
	submissionAttributes := []attribute.KeyValue{
		attribute.String("outcome", string(m.Outcome)),
	}

	if m.Reason != nil {
		submissionAttributes = append(submissionAttributes, attribute.String("reason", string(*m.Reason)))
	}

	a.apiSubmissionsCounter.Add(ctx, 1, metric.WithAttributes(submissionAttributes...))
	contentTypeAttribute := metric.WithAttributes(attribute.String("content_type", string(m.ContentType)))

	// oibp.jobs.enqueued
	if m.Enqueued {
		a.jobsEnqueuedCounter.Add(ctx, 1, contentTypeAttribute)
	}

	// oibp.document.input.size
	if m.InputBytes != nil {
		a.documentInputSizeHisto.Record(ctx, *m.InputBytes, contentTypeAttribute)
	}
}

func (a *ApplicationOtelMetrics) RecordStorageUpload(ctx context.Context, m metrics.StorageUploadObservation) {
	a.storageUploadDurationHisto.Record(
		ctx,
		m.Duration.Seconds(),
		metric.WithAttributes(attribute.String("purpose", string(m.Purpose))),
		metric.WithAttributes(attribute.String("outcome", string(m.Outcome))),
	)
}

func (a *ApplicationOtelMetrics) RecordResponseWriteError(ctx context.Context, m metrics.ResponseObservation) {
	a.apiWriteResponseErrorCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("route", string(m.Endpoint))))
}

func (a *ApplicationOtelMetrics) RecordJobTerminal(ctx context.Context, m metrics.TerminalObservation) {
	attributes := []attribute.KeyValue{}
	attributes = append(attributes, attribute.String("outcome", string(m.Outcome)))

	if m.Reason != nil {
		attributes = append(attributes, attribute.String("status_reason", string(*m.Reason)))
	}

	a.jobTerminalCounter.Add(ctx, 1, metric.WithAttributes(attributes...))
}

// Compile Time Check
var (
	_ ServiceMetrics = (*ApplicationOtelMetrics)(nil)
)
