package service

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type SubmitInput struct {
	Document           io.Reader
	SizeBytes          int64
	ContentType        config.ContentType
	NotificationEmails []string
	SubmitStartedAt    time.Time
}

type SubmitService interface {
	Submit(ctx context.Context, input SubmitInput) (*job.Ticket, error)
}

func NewSubmitService(cfg config.Config, lg logging.Logger, t trace.Tracer, s storage.Storage, m telemetry.ServiceMetrics, j job.TicketRepository, q queue.BatchQueue) SubmitService {
	return &submitService{
		config:  cfg,
		logger:  lg,
		tracer:  t,
		storage: s,
		metrics: m,
		job:     j,
		queue:   q,
	}
}

type submitService struct {
	config  config.Config
	logger  logging.Logger
	tracer  trace.Tracer
	storage storage.Storage
	metrics telemetry.ServiceMetrics
	job     job.TicketRepository
	queue   queue.BatchQueue
}

func (s *submitService) Submit(ctx context.Context, input SubmitInput) (*job.Ticket, error) {
	ctx, span := s.tracer.Start(ctx, "Submit")
	defer span.End()

	ft, err := input.ContentType.FileType()
	if err != nil {
		return nil, err
	}

	ticket := job.NewTicket()
	ticket.DocumentPath = s.config.GetDocumentPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String(), ft)
	ticket.ContentType = string(input.ContentType)
	ticket.NotificationEmails = input.NotificationEmails

	ctx = logging.WithTicketID(ctx, ticket.ID.String())
	logger := s.logger.WithContext(ctx)
	span.SetAttributes(attribute.String("ticket_id", ticket.ID.String()))

	if err := s.validateInput(ctx, input); err != nil {
		return nil, err
	}

	if err := s.storeDocument(ctx, logger, input, ticket); err != nil {
		return nil, err
	}

	if err := s.createTicket(ctx, logger, input, ticket); err != nil {
		return nil, err
	}

	if err := s.enqueue(ctx, logger, input, ticket); err != nil {
		return nil, err
	}

	return ticket, nil
}

type ErrInvalidNotificationEmails struct {
	maximumEntries int
}

func (e ErrInvalidNotificationEmails) Error() string {
	return fmt.Sprintf("notification_emails must contain at most %d entries", e.maximumEntries)
}

func (s *submitService) validateInput(ctx context.Context, input SubmitInput) error {
	notificationEmailLimit := s.config.GetNotificationEmailLimit()
	if len(input.NotificationEmails) > notificationEmailLimit {
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionRejected,
			Reason:      new(metrics.SubmissionReasonInvalidNotificationEmails),
			ContentType: &input.ContentType,
		})

		return ErrInvalidNotificationEmails{maximumEntries: notificationEmailLimit}
	}

	return nil
}

func (s *submitService) storeDocument(ctx context.Context, logger logging.Logger, input SubmitInput, ticket *job.Ticket) error {
	ctx, span := s.tracer.Start(ctx, "storeDocument")
	defer span.End()

	storageStart := time.Now()

	if err := s.storage.Write(ctx, input.Document, ticket.DocumentPath); err != nil {
		s.metrics.RecordStorageUpload(
			ctx,
			metrics.StorageUploadObservation{
				Duration: time.Since(storageStart),
				Purpose:  metrics.StorageUploadPurposeDocument,
				Outcome:  metrics.StorageUploadFailed,
			},
		)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonStorageWriteFailed),
			ContentType: &input.ContentType,
			InputBytes:  &input.SizeBytes,
		})
		logger.Error(ctx, "document storage failed", "component", logging.ComponentStorage, "operation", logging.OperationStorageWrite, "content_type", input.ContentType, "error", err)
		return err
	}

	s.metrics.RecordStorageUpload(
		ctx,
		metrics.StorageUploadObservation{
			Duration: time.Since(storageStart),
			Purpose:  metrics.StorageUploadPurposeDocument,
			Outcome:  metrics.StorageUploadSuccess,
		},
	)
	return nil
}

func (s *submitService) createTicket(ctx context.Context, logger logging.Logger, input SubmitInput, ticket *job.Ticket) error {
	ctx, span := s.tracer.Start(ctx, "createTicket")
	defer span.End()

	if err := s.job.Create(ctx, ticket); err != nil {
		logger.Error(ctx, "ticket creation failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketCreate, "error", err)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonTicketCreationFailed),
			ContentType: &input.ContentType,
			InputBytes:  &input.SizeBytes,
		})
		span.RecordError(err)
		return err
	}

	return nil
}

func (s *submitService) enqueue(ctx context.Context, logger logging.Logger, input SubmitInput, ticket *job.Ticket) error {
	ctx, span := s.tracer.Start(ctx, "enqueue")
	defer span.End()

	if err := s.queue.Enqueue(ctx, ticket); err != nil {

		detail := fmt.Sprintf("enqueue failed. err: %v", err)
		commonUpdateErrLoggingFields := []any{
			"component", logging.ComponentDatabase,
			"reason", string(job.EnqueueFailed),
			"original_error", err,
			"operation", logging.OperationMarkFailed,
		}

		updated, updateErr := s.job.MarkFailed(ctx, ticket.ID, job.EnqueueFailed, detail)
		if updateErr != nil {
			commonUpdateErrLoggingFields = append(commonUpdateErrLoggingFields, "error", updateErr)
			logger.Error(ctx, "ticket failure update failed", commonUpdateErrLoggingFields...)
		} else if !updated {
			logger.Warn(ctx, "ticket failure update skipped", commonUpdateErrLoggingFields...)
		} else {
			logger.Info(ctx, "ticket marked failed", commonUpdateErrLoggingFields...)
		}

		logger.Error(ctx, "queue enqueue failed", "component", logging.ComponentQueue, "error", err, "operation", logging.OperationEnqueue)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonQueueEnqueueFailed),
			ContentType: &input.ContentType,
			InputBytes:  &input.SizeBytes,
		})
		s.metrics.RecordJobTerminal(ctx, metrics.TerminalObservation{
			Outcome: metrics.TerminalFailed,
			Reason:  new(metrics.TerminalReasonEnqueueFailed),
		})
		s.metrics.RecordJobEndToEnd(ctx, metrics.JobEndObservation{
			Duration: time.Since(input.SubmitStartedAt),
			Outcome:  metrics.JobEndOutcomeFailed,
			Reason:   new(metrics.TerminalReasonEnqueueFailed),
		})
		span.RecordError(err)
		return err
	}

	return nil
}
