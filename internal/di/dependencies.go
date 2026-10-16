package di

import (
	"context"
	"net/http"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/health"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	oibplogging "github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/notification"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	telemetry_metrics "github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"github.com/kiran94/openitinerary-batch-processing/pkg/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"gorm.io/gorm"
)

type Services struct {
	Config              config.Config
	Logger              logging.Logger
	Telemetry           *telemetry.TelemetryManager
	ServiceMetrics      telemetry_metrics.ServiceMetrics
	WorkerMetrics       telemetry_metrics.WorkerMetrics
	ServiceReadyManager health.ServiceReadyManager

	Database         *gorm.DB
	Storage          storage.Storage
	Queue            queue.BatchQueue
	TicketRepository job.TicketRepository
	Adapter          clients.Adapter
	Notifier         notification.Notifier

	SubmitService service.SubmitService
	ResultService service.ResultService
	StatusService service.StatusService
}

func NewServices(ctx context.Context, serviceName string, logger logging.Logger, cfg config.Config) (*Services, error) {
	s := Services{
		Config: cfg,
		Logger: logging.New(cfg),
	}

	tele, err := telemetry.NewTelemetryManager(ctx, serviceName, cfg)
	if err != nil {
		logger.Error(ctx, "otel initialization failed", "error", err)
		return nil, err
	}
	s.Telemetry = tele

	if err := s.Telemetry.Ready(); err != nil {
		logger.Error(ctx, "otel ready failed", "error", err)
		s.Shutdown()
		return nil, err
	}

	metrics, err := telemetry_metrics.NewApplicationOtelMetrics(tele.GetMeterProvider())
	if err != nil {
		logger.Error(ctx, "metrics initialization failed", "error", err)
		s.Shutdown()
		return nil, err
	}

	traceProvider := tele.GetTracerProvider()
	storageTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/storage")
	queueTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/queue")
	jobTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/job")
	servicesTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/service")
	notificationTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/notification")
	adapterTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/clients")

	telemetry.SetLogger(s.Logger)

	db, err := internal.ConnectDatabase(ctx, logger, cfg)
	if err != nil {
		logger.Error(ctx, "database initialization failed", "component", oibplogging.ComponentDatabase, "error", err)
		s.Shutdown()
		return nil, err
	}

	s3, err := storage.NewAWSS3Storage(ctx, cfg, storageTracer)
	if err != nil {
		logger.Error(ctx, "storage initialization failed", "component", oibplogging.ComponentStorage, "backend", "s3", "error", err)
		s.Shutdown()
		return nil, err
	}

	batchQueue, err := queue.NewQueue(ctx, cfg, logger, queueTracer)
	if err != nil {
		logger.Error(ctx, "queue initialization failed", "component", oibplogging.ComponentQueue, "backend", "sqs", "error", err)
		s.Shutdown()
		return nil, err
	}

	if err := batchQueue.Ready(ctx); err != nil {
		logger.Error(ctx, "queue initialization failed", "component", oibplogging.ComponentQueue, "backend", "sqs", "error", err)
		s.Shutdown()
		return nil, err
	}

	internalDB, err := db.DB()
	if err != nil {
		logger.Error(ctx, "could not get internal db", "component", oibplogging.ComponentDatabase, "error", err)
		s.Shutdown()
		return nil, err
	}

	notifier, err := notification.NewNotifier(ctx, cfg, logger, notificationTracer)
	if err != nil {
		logger.Error(ctx, "could not create notifier", "component", oibplogging.ComponentNotifier, "error", err)
		s.Shutdown()
		return nil, err
	}

	adapter := clients.NewAdapterClient(
		&http.Client{Timeout: time.Duration(60) * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		logger,
		adapterTracer,
		cfg.GetMaximumAdapterResponseSizeBytes(),
	)

	s.ServiceMetrics = metrics
	s.WorkerMetrics = metrics
	s.Database = db
	s.Storage = s3
	s.Queue = batchQueue
	s.TicketRepository = job.NewTicketRepository(db, jobTracer)
	s.ServiceReadyManager = health.NewServiceReadyManager(s.Config, s.Logger, internalDB)
	s.Adapter = adapter
	s.Notifier = notifier

	s.SubmitService = service.NewSubmitService(s.Config, s.Logger, servicesTracer, s.Storage, s.ServiceMetrics, s.TicketRepository, s.Queue)
	s.ResultService = service.NewResultService(s.Config, s.Logger, servicesTracer, s.TicketRepository, s.Storage)
	s.StatusService = service.NewStatusService(s.TicketRepository, servicesTracer)

	return &s, nil
}

func (s *Services) GetTracer(name string) trace.Tracer {
	return s.Telemetry.GetTracerProvider().Tracer(name)
}

const (
	shutdownTelemetryTimeout = 15 * time.Second
)

func (s *Services) Shutdown() {
	ctx := context.Background()

	s.Logger.Debug(ctx, "shutting down telemetry", "timeout_seconds", shutdownTelemetryTimeout.Seconds())
	cancelCtx, cancelFunc := context.WithTimeout(ctx, shutdownTelemetryTimeout)
	defer cancelFunc()

	if s.Telemetry != nil {
		if err := s.Telemetry.Close(cancelCtx); err != nil {
			s.Logger.Error(cancelCtx, "failed to cleanup otel", "error", err)
		}
	}

	if s.Adapter != nil {
		if err := s.Adapter.Close(); err != nil {
			s.Logger.Error(cancelCtx, "failed to close adapter", "error", err)
		}
	}
}
