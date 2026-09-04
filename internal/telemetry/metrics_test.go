package telemetry

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

const applicationInstrumentationScope = "github.com/kiran94/openitinerary-batch-processing/internal/telemetry"

type mockMeterProvider struct {
	noop.MeterProvider
	mock.Mock
}

func (m *mockMeterProvider) Meter(name string, options ...metric.MeterOption) metric.Meter {
	args := m.Called(name, options)
	if meter := args.Get(0); meter != nil {
		return meter.(metric.Meter)
	}

	return nil
}

type mockMeter struct {
	noop.Meter
	mock.Mock
}

func (m *mockMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	args := m.Called(name, options)
	if instrument := args.Get(0); instrument != nil {
		return instrument.(metric.Int64Counter), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *mockMeter) Int64Histogram(name string, options ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	args := m.Called(name, options)
	if instrument := args.Get(0); instrument != nil {
		return instrument.(metric.Int64Histogram), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *mockMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	args := m.Called(name, options)
	if instrument := args.Get(0); instrument != nil {
		return instrument.(metric.Float64Histogram), args.Error(1)
	}

	return nil, args.Error(1)
}

func TestNewApplicationOtelMetricsCreatesConfiguredInstruments(t *testing.T) {
	provider := new(mockMeterProvider)
	meter := new(mockMeter)

	apiSubmissionsCounter := &noop.Int64Counter{}
	jobsEnqueuedCounter := &noop.Int64Counter{}
	documentInputSizeHistogram := &noop.Int64Histogram{}
	storageUploadDurationHistogram := &noop.Float64Histogram{}
	apiResponseWriteErrorsCounter := &noop.Int64Counter{}
	jobTerminalCounter := &noop.Int64Counter{}
	workerRunCounter := &noop.Int64Counter{}
	workerPollingDurationHistogram := &noop.Float64Histogram{}
	workerProcessingDurationHistogram := &noop.Float64Histogram{}
	adapterConversionDurationHistogram := &noop.Float64Histogram{}
	adapterResponseSizeHistogram := &noop.Int64Histogram{}

	provider.On("Meter", applicationInstrumentationScope, mock.Anything).Return(meter).Once()
	meter.
		On(
			"Int64Counter",
			string(otelMetricAPISubmissions),
			mock.MatchedBy(func(options []metric.Int64CounterOption) bool {
				config := metric.NewInt64CounterConfig(options...)
				return config.Unit() == "1"
			}),
		).
		Return(apiSubmissionsCounter, nil).
		Once()
	meter.
		On(
			"Int64Counter",
			string(otelMetricJobsEnqueued),
			mock.MatchedBy(func(options []metric.Int64CounterOption) bool {
				config := metric.NewInt64CounterConfig(options...)
				return config.Unit() == "1"
			}),
		).
		Return(jobsEnqueuedCounter, nil).
		Once()
	meter.
		On(
			"Int64Histogram",
			string(otelMetricDocumentInputSize),
			mock.MatchedBy(func(options []metric.Int64HistogramOption) bool {
				config := metric.NewInt64HistogramConfig(options...)
				return config.Unit() == "By"
			}),
		).
		Return(documentInputSizeHistogram, nil).
		Once()
	meter.
		On(
			"Float64Histogram",
			string(otelMetricStorageUploadDuration),
			mock.MatchedBy(func(options []metric.Float64HistogramOption) bool {
				config := metric.NewFloat64HistogramConfig(options...)
				return config.Unit() == "s"
			}),
		).
		Return(storageUploadDurationHistogram, nil).
		Once()
	meter.
		On(
			"Int64Counter",
			string(otelMetricAPIResponseWriteErrors),
			mock.MatchedBy(func(options []metric.Int64CounterOption) bool {
				config := metric.NewInt64CounterConfig(options...)
				return config.Unit() == "1"
			}),
		).
		Return(apiResponseWriteErrorsCounter, nil).
		Once()
	meter.
		On(
			"Int64Counter",
			string(otelMetricJobTerminal),
			mock.MatchedBy(func(options []metric.Int64CounterOption) bool {
				config := metric.NewInt64CounterConfig(options...)
				return config.Unit() == "1"
			}),
		).
		Return(jobTerminalCounter, nil).
		Once()
	meter.
		On(
			"Int64Counter",
			string(otelMetricWorkerRuns),
			mock.MatchedBy(func(options []metric.Int64CounterOption) bool {
				config := metric.NewInt64CounterConfig(options...)
				return config.Unit() == "1"
			}),
		).
		Return(workerRunCounter, nil).
		Once()
	meter.
		On(
			"Float64Histogram",
			string(otelMetricWorkerPollingDuration),
			mock.MatchedBy(func(options []metric.Float64HistogramOption) bool {
				config := metric.NewFloat64HistogramConfig(options...)
				return config.Unit() == "s"
			}),
		).
		Return(workerPollingDurationHistogram, nil).
		Once()
	meter.
		On(
			"Float64Histogram",
			string(otelMetricWorkerProcessingDuration),
			mock.MatchedBy(func(options []metric.Float64HistogramOption) bool {
				config := metric.NewFloat64HistogramConfig(options...)
				return config.Unit() == "s"
			}),
		).
		Return(workerProcessingDurationHistogram, nil).
		Once()
	meter.
		On(
			"Float64Histogram",
			string(otelMetricAdapterConvertDuration),
			mock.MatchedBy(func(options []metric.Float64HistogramOption) bool {
				config := metric.NewFloat64HistogramConfig(options...)
				return config.Unit() == "s"
			}),
		).
		Return(adapterConversionDurationHistogram, nil).
		Once()
	meter.
		On(
			"Int64Histogram",
			string(otelMetricAdapterResponseSize),
			mock.MatchedBy(func(options []metric.Int64HistogramOption) bool {
				config := metric.NewInt64HistogramConfig(options...)
				return config.Unit() == "By"
			}),
		).
		Return(adapterResponseSizeHistogram, nil).
		Once()
	got, err := NewApplicationOtelMetrics(provider)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Same(t, apiSubmissionsCounter, got.apiSubmissionsCounter)
	assert.Same(t, jobsEnqueuedCounter, got.jobsEnqueuedCounter)
	assert.Same(t, documentInputSizeHistogram, got.documentInputSizeHisto)
	assert.Same(t, storageUploadDurationHistogram, got.storageUploadDurationHisto)
	assert.Same(t, apiResponseWriteErrorsCounter, got.apiWriteResponseErrorCounter)
	assert.Same(t, jobTerminalCounter, got.jobTerminalCounter)
	assert.Same(t, workerRunCounter, got.workerRunCounter)
	assert.Same(t, workerPollingDurationHistogram, got.workerPollingDuration)
	assert.Same(t, workerProcessingDurationHistogram, got.workerProcessingDuration)
	assert.Same(t, adapterConversionDurationHistogram, got.adapterConversionDuration)
	assert.Same(t, adapterResponseSizeHistogram, got.adapterResponseSize)
	provider.AssertExpectations(t)
	meter.AssertExpectations(t)
}

func TestNewApplicationOtelMetricsReturnsInstrumentCreationError(t *testing.T) {
	expectedErr := errors.New("instrument creation failed")
	tests := []struct {
		name   string
		metric string
		setup  func(*mockMeter, error)
	}{
		{
			name:   "api submissions",
			metric: string(otelMetricAPISubmissions),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "jobs enqueued",
			metric: string(otelMetricJobsEnqueued),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "document input size",
			metric: string(otelMetricDocumentInputSize),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "storage upload duration",
			metric: string(otelMetricStorageUploadDuration),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "api response write errors",
			metric: string(otelMetricAPIResponseWriteErrors),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricAPIResponseWriteErrors), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "job terminal",
			metric: string(otelMetricJobTerminal),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricAPIResponseWriteErrors), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobTerminal), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "worker processing duration",
			metric: string(otelMetricWorkerProcessingDuration),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricAPIResponseWriteErrors), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricJobTerminal), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Int64Counter", string(otelMetricWorkerRuns), mock.Anything).
					Return(&noop.Int64Counter{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricWorkerPollingDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).
					Once()
				meter.On("Float64Histogram", string(otelMetricWorkerProcessingDuration), mock.Anything).
					Return(nil, expectedErr).
					Once()
			},
		},
		{
			name:   "adapter conversion duration",
			metric: string(otelMetricAdapterConvertDuration),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricAPIResponseWriteErrors), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricJobTerminal), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricWorkerRuns), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricWorkerPollingDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricWorkerProcessingDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricAdapterConvertDuration), mock.Anything).
					Return(nil, expectedErr).Once()
			},
		},
		{
			name:   "adapter response size",
			metric: string(otelMetricAdapterResponseSize),
			setup: func(meter *mockMeter, expectedErr error) {
				meter.On("Int64Counter", string(otelMetricAPISubmissions), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricJobsEnqueued), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Histogram", string(otelMetricDocumentInputSize), mock.Anything).
					Return(&noop.Int64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricStorageUploadDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricAPIResponseWriteErrors), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricJobTerminal), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Int64Counter", string(otelMetricWorkerRuns), mock.Anything).
					Return(&noop.Int64Counter{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricWorkerPollingDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricWorkerProcessingDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Float64Histogram", string(otelMetricAdapterConvertDuration), mock.Anything).
					Return(&noop.Float64Histogram{}, nil).Once()
				meter.On("Int64Histogram", string(otelMetricAdapterResponseSize), mock.Anything).
					Return(nil, expectedErr).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := new(mockMeterProvider)
			meter := new(mockMeter)
			provider.On("Meter", applicationInstrumentationScope, mock.Anything).Return(meter).Once()
			tt.setup(meter, expectedErr)

			got, err := NewApplicationOtelMetrics(provider)

			require.Error(t, err)
			assert.Nil(t, got)
			require.ErrorIs(t, err, expectedErr)
			require.ErrorContains(t, err, tt.metric)
			provider.AssertExpectations(t)
			meter.AssertExpectations(t)
		})
	}
}
