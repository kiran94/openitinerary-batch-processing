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

type WorkerMetrics interface {
	metrics.StorageUploadRecorder
	metrics.AdapterConversionRecorder
	metrics.AdapterResponseRecorder
	metrics.WorkerRunRecorder
	metrics.WorkerPollingRecorder
	metrics.WorkerProcessingRecorder
	metrics.JobTerminalRecorder
	metrics.JobEndToEndRecorder
}

type ApplicationOtelMetrics struct {
	apiSubmissionsCounter        metric.Int64Counter
	jobsEnqueuedCounter          metric.Int64Counter
	documentInputSizeHisto       metric.Int64Histogram
	storageUploadDurationHisto   metric.Float64Histogram
	apiWriteResponseErrorCounter metric.Int64Counter
	jobTerminalCounter           metric.Int64Counter
	workerRunCounter             metric.Int64Counter
	workerPollingDuration        metric.Float64Histogram
	workerProcessingDuration     metric.Float64Histogram
	adapterConversionDuration    metric.Float64Histogram
	adapterResponseSize          metric.Int64Histogram
	jobEndDurationHisto          metric.Float64Histogram
}

type otelMetric string

const (
	otelMetricAPISubmissions           otelMetric = "oibp.api.submissions"
	otelMetricJobsEnqueued             otelMetric = "oibp.jobs.enqueued"
	otelMetricDocumentInputSize        otelMetric = "oibp.document.input.size"
	otelMetricStorageUploadDuration    otelMetric = "oibp.storage.upload.duration"
	otelMetricAPIResponseWriteErrors   otelMetric = "oibp.api.response_write.errors"
	otelMetricJobTerminal              otelMetric = "oibp.jobs.terminal"
	otelMetricWorkerRuns               otelMetric = "oibp.worker.runs"
	otelMetricWorkerPollingDuration    otelMetric = "oibp.worker.polling.duration"
	otelMetricWorkerProcessingDuration otelMetric = "oibp.worker.processing.duration"
	otelMetricAdapterConvertDuration   otelMetric = "oibp.adapter.convert.duration"
	otelMetricAdapterResponseSize      otelMetric = "oibp.adapter.response.size"
	otelMetricJobEndDuration           otelMetric = "oibp.job.end_to_end.duration"
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

	workerRunCounter, err := meter.Int64Counter(string(otelMetricWorkerRuns), metric.WithUnit("1"), metric.WithDescription("tracks the number of worker runs"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricWorkerRuns), err)
	}

	workerPollingDurationHisto, err := meter.Float64Histogram(string(otelMetricWorkerPollingDuration), metric.WithUnit("s"), metric.WithDescription("tracks the polling time for the worker"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricWorkerPollingDuration), err)
	}

	workerProcessingDurationHisto, err := meter.Float64Histogram(string(otelMetricWorkerProcessingDuration), metric.WithUnit("s"), metric.WithDescription("tracks the processing time for the worker"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricWorkerProcessingDuration), err)
	}

	adapterConversionDurationHisto, err := meter.Float64Histogram(string(otelMetricAdapterConvertDuration), metric.WithUnit("s"), metric.WithDescription("tracks adapter conversion times"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricAdapterConvertDuration), err)
	}

	adapterResponseSizeHisto, err := meter.Int64Histogram(string(otelMetricAdapterResponseSize), metric.WithUnit("By"), metric.WithDescription("tracks adapter response sizes"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricAdapterResponseSize), err)
	}

	jobEndDuration, err := meter.Float64Histogram(string(otelMetricJobEndDuration), metric.WithUnit("s"), metric.WithDescription("tracks the number of seconds the job took from submission to completion"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to create metric %v", otelMetricJobEndDuration), err)
	}

	return &ApplicationOtelMetrics{
		apiSubmissionsCounter:        apiSubmissionsCounter,
		jobsEnqueuedCounter:          jobsEnqueuedCounter,
		documentInputSizeHisto:       documentInputSizeHisto,
		storageUploadDurationHisto:   storageUploadDurationHisto,
		apiWriteResponseErrorCounter: apiWriteResponseErrorCounter,
		jobTerminalCounter:           jobTerminalCounter,
		workerRunCounter:             workerRunCounter,
		workerPollingDuration:        workerPollingDurationHisto,
		workerProcessingDuration:     workerProcessingDurationHisto,
		adapterConversionDuration:    adapterConversionDurationHisto,
		adapterResponseSize:          adapterResponseSizeHisto,
		jobEndDurationHisto:          jobEndDuration,
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
	contentTypeAttributes := []attribute.KeyValue{}
	if m.ContentType != nil {
		contentTypeAttributes = append(
			contentTypeAttributes,
			attribute.String("content_type", string(*m.ContentType)),
		)
	}

	// oibp.jobs.enqueued
	if m.Enqueued {
		a.jobsEnqueuedCounter.Add(ctx, 1, metric.WithAttributes(contentTypeAttributes...))
	}

	// oibp.document.input.size
	if m.InputBytes != nil {
		a.documentInputSizeHisto.Record(ctx, *m.InputBytes, metric.WithAttributes(contentTypeAttributes...))
	}
}

func (a *ApplicationOtelMetrics) RecordStorageUpload(ctx context.Context, m metrics.StorageUploadObservation) {
	a.storageUploadDurationHisto.Record(
		ctx,
		m.Duration.Seconds(),
		metric.WithAttributeSet(attribute.NewSet(
			attribute.String("purpose", string(m.Purpose)),
			attribute.String("outcome", string(m.Outcome)),
		)),
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

func (a *ApplicationOtelMetrics) RecordWorkerRun(ctx context.Context, o metrics.WorkerRunObservation) {
	a.workerRunCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", string(o.Outcome))))
}

func (a *ApplicationOtelMetrics) RecordWorkerPolling(ctx context.Context, o metrics.WorkerPollingObservation) {
	a.workerPollingDuration.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(o.Outcome))))
}

func (a *ApplicationOtelMetrics) RecordWorkerProcessing(ctx context.Context, o metrics.WorkerProcessingObservation) {
	a.workerProcessingDuration.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(o.Outcome))))
}

func (a *ApplicationOtelMetrics) RecordAdapterConversion(ctx context.Context, o metrics.AdapterConversionObservation) {
	a.adapterConversionDuration.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(o.Outcome))))
}

func (a *ApplicationOtelMetrics) RecordAdapterResponse(ctx context.Context, o metrics.AdapterResponseObservation) {
	a.adapterResponseSize.Record(ctx, o.SizeBytes, metric.WithAttributes(attribute.String("content_type", string(o.ContentType))))
}

func (a *ApplicationOtelMetrics) RecordJobEndToEnd(ctx context.Context, o metrics.JobEndObservation) {
	attributes := []attribute.KeyValue{
		attribute.String("outcome", string(o.Outcome)),
	}
	if o.Reason != nil {
		attributes = append(attributes, attribute.String("status_reason", string(*o.Reason)))
	}
	a.jobEndDurationHisto.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(attributes...))
}

// Compile Time Check
var (
	_ ServiceMetrics = (*ApplicationOtelMetrics)(nil)
	_ WorkerMetrics  = (*ApplicationOtelMetrics)(nil)
)
