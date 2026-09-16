package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"go.opentelemetry.io/otel/trace"
)

type ResultOutput struct {
	Link    string    `json:"link"`
	Expires time.Time `json:"expires"`
}

type ResultService interface {
	GetResultLink(ctx context.Context, ticketID uuid.UUID) (*ResultOutput, error)
}

type resultService struct {
	config  config.Config
	logger  logging.Logger
	trace   trace.Tracer
	job     job.TicketRepository
	storage storage.Storage
}

func NewResultService(
	config config.Config,
	logger logging.Logger,
	trace trace.Tracer,
	job job.TicketRepository,
	storage storage.Storage,
) ResultService {
	return &resultService{config: config, logger: logger, trace: trace, job: job, storage: storage}
}

func (r *resultService) GetResultLink(ctx context.Context, ticketID uuid.UUID) (*ResultOutput, error) {
	ctx, span := r.trace.Start(ctx, "GetResultLink")
	defer span.End()

	logger := r.logger.WithContext(ctx)

	ticket, err := r.job.Get(ctx, ticketID)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			return nil, job.ErrRecordNotFound
		} else {
			logger.Error(ctx, "ticket status lookup failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketLookup, "error", err)
			return nil, err
		}
	}

	if ticket.Status != job.Complete {
		return nil, ErrJobNotComplete
	}

	outputPath := r.config.GetOutputPath(r.config.GetBucket(), r.config.GetBucketPath(), ticket.ID.String())
	link, expiration, err := r.storage.GetLink(ctx, outputPath)
	if err != nil {
		logger.Error(ctx, "failed to generate link", "component", logging.ComponentStorage, "error", err)
		return nil, err
	}

	return &ResultOutput{Link: link, Expires: expiration}, nil
}

var (
	ErrJobNotComplete = errors.New("job has not been completed")
)
