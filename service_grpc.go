package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/di"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/middleware/interceptors"
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	batchv1 "github.com/kiran94/openitinerary-batch-processing/proto/gen/go/openitinerary/batch/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	PORT = ":50051"
)

func StartGRPCService() error {
	ctx := context.Background()
	interruptCtx, interruptCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer interruptCancel()

	cfg := config.NewConfig()
	bootstrapLogger := logging.New(cfg)
	services, err := di.NewServices(ctx, cfg.GetOtelGrpcName(), bootstrapLogger, cfg)
	if err != nil {
		bootstrapLogger.Error(ctx, "failed to create services", "component", logging.ComponentGRPC, "error", err)
		return err
	}
	defer services.Shutdown()

	lis, err := net.Listen("tcp", PORT)
	if err != nil {
		return err
	}

	unaryIntercepts, streamingIntercepts := configureInterceptors(cfg)
	server := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(unaryIntercepts...),
		grpc.ChainStreamInterceptor(streamingIntercepts...),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: cfg.GetGrpcServerIdleTimeout(),
			MaxConnectionAge:  cfg.GetGrpcServerMaxConnectionAge(),
		}),
	)
	defer shutdownGrpcServer(server, bootstrapLogger)

	batchv1.RegisterOpenItineraryBatchProcessingServiceServer(server, &OpenItineraryBatchProcessingGrpcService{
		cfg:           services.Config,
		logger:        services.Logger,
		metrics:       services.ServiceMetrics,
		submitService: services.SubmitService,
		statusService: services.StatusService,
		resultService: services.ResultService,
	})
	reflection.Register(server)

	errorCh := make(chan error, 1)
	go func() {
		bootstrapLogger.Info(ctx, "server listening", "component", logging.ComponentGRPC, "listen_addr", PORT)
		if err := server.Serve(lis); err != nil {
			errorCh <- err
		}
	}()

	select {
	case <-interruptCtx.Done():
		services.Logger.Info(ctx, "server cancellation was signaled", "component", logging.ComponentGRPC)
	case err := <-errorCh:
		return err
	}

	return nil
}

type OpenItineraryBatchProcessingGrpcService struct {
	batchv1.UnimplementedOpenItineraryBatchProcessingServiceServer

	cfg     config.Config
	logger  logging.Logger
	metrics telemetry.ServiceMetrics

	submitService service.SubmitService
	statusService service.StatusService
	resultService service.ResultService
}

func (s *OpenItineraryBatchProcessingGrpcService) Submit(request grpc.ClientStreamingServer[batchv1.SubmitRequest, batchv1.SubmitResponse]) error {
	startedAt := time.Now().UTC()

	ctx := request.Context()
	logger := s.logger.WithContext(ctx)

	var contentType config.ContentType
	var notificationEmails []string

	// Process Metadata
	r, err := request.Recv()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "metadata frame must be provided")
	}

	if err != nil {
		logger.Error(ctx, "failed to read first message", "error", err)
		return err
	}

	m := r.GetMetadata()
	if m == nil {
		logger.Error(ctx, "first message must be metadata")
		return status.Error(codes.InvalidArgument, "first message must be metadata")
	}

	notificationEmails = m.NotificationEmails
	contentType, err = convertGrpcDocumentTypeToContentType(m.ContentType)
	if err != nil {
		logger.Error(ctx, "could not convert document type", "error", err)
		return status.Error(codes.InvalidArgument, "unrecognised content type")
	}

	// Process Data Chunks
	reader, writer := io.Pipe()
	defer reader.Close()

	stopTimeout := context.AfterFunc(ctx, func() {
		_ = writer.CloseWithError(ctx.Err())
	})
	defer stopTimeout()

	document := &CountingReader{reader: reader, maximum: s.cfg.GetMaximumAllowedRequestSizeMb()}

	go func() {
		for {
			rec, err := request.Recv()
			if err == io.EOF {
				_ = writer.Close()
				return
			}

			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}

			switch frame := rec.Frame.(type) {
			case *batchv1.SubmitRequest_Chunk:
				if _, err := writer.Write(frame.Chunk); err != nil {
					_ = writer.CloseWithError(err)
					return
				}

			case *batchv1.SubmitRequest_Metadata:
				err := fmt.Errorf("%w: metadata may only be sent once", service.ErrInvalidDocumentInput)
				_ = writer.CloseWithError(err)
				return

			default:
				err := fmt.Errorf("%w: expected a document chunk", service.ErrInvalidDocumentInput)
				_ = writer.CloseWithError(err)
				return
			}
		}
	}()

	// Submit

	input := service.SubmitInput{
		Document:           document,
		ContentType:        contentType,
		NotificationEmails: notificationEmails,
		SubmitStartedAt:    startedAt,
		ServiceType:        metrics.SubmissionServiceTypeGRPC,
	}

	ticket, err := s.submitService.Submit(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return status.FromContextError(ctx.Err()).Err()
		}

		var invalidEmails service.ErrInvalidNotificationEmails
		if errors.As(err, &invalidEmails) {
			return status.Error(codes.InvalidArgument, err.Error())
		}

		if errors.Is(err, service.ErrDocumentTooLarge) {
			return status.Error(codes.ResourceExhausted, err.Error())
		}

		if errors.Is(err, service.ErrInvalidDocumentInput) {
			return status.Error(codes.InvalidArgument, err.Error())
		}

		logger.Error(ctx, "error submitting ticket", "component", logging.ComponentGRPC, "error", err)
		return status.Error(codes.Internal, "error submitting ticket")
	}

	ctx = logging.WithTicketID(ctx, ticket.ID.String())
	logger = logger.WithContext(ctx)

	s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
		Outcome:     metrics.SubmissionAccepted,
		Enqueued:    true,
		InputBytes:  new(document.SizeBytes()),
		ContentType: &contentType,
		ServiceType: metrics.SubmissionServiceTypeGRPC,
	})

	logger.Info(ctx,
		"submit accepted",
		"content_type", contentType,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"component", logging.ComponentGRPC,
		"operation", logging.OperationSubmit,
	)

	err = request.SendAndClose(&batchv1.SubmitResponse{
		Ticket: convertTicketToGRPCTicket(ctx, ticket, logger),
	})

	if err != nil {
		logger.Error(ctx, "error sending result", "component", logging.ComponentGRPC, "error", err)
		return err
	}
	return nil
}

func (s *OpenItineraryBatchProcessingGrpcService) Status(ctx context.Context, request *batchv1.StatusRequest) (*batchv1.StatusResponse, error) {
	logger := s.logger.WithContext(ctx)

	if request.TicketId == "" {
		return nil, status.Error(codes.InvalidArgument, "ticket cannot be empty")
	}

	ticketID, err := uuid.Parse(request.TicketId)
	if err != nil {
		logger.Error(ctx, "ticket was not a valid uuid", "ticket_id", request.TicketId)
		return nil, status.Error(codes.InvalidArgument, "ticket was not a valid uuid")
	}

	ctx = logging.WithTicketID(ctx, request.TicketId)

	ticket, err := s.statusService.GetStatus(ctx, ticketID)
	if err != nil {

		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}

		if errors.Is(err, job.ErrRecordNotFound) {
			logger.Error(ctx, "ticket was not found", "ticket_id", request.TicketId)
			return nil, status.Error(codes.NotFound, "ticket was not found")
		} else {
			logger.Error(ctx, "error getting ticket", "ticket_id", request.TicketId)
			return nil, status.Error(codes.Internal, "error getting ticket")
		}
	}

	response := batchv1.StatusResponse{Ticket: convertTicketToGRPCTicket(ctx, ticket, logger)}
	return &response, nil
}

func (s *OpenItineraryBatchProcessingGrpcService) ResultLink(ctx context.Context, request *batchv1.ResultLinkRequest) (*batchv1.ResultLinkResponse, error) {
	logger := s.logger.WithContext(ctx)

	if request.TicketId == "" {
		return nil, status.Error(codes.InvalidArgument, "ticket cannot be empty")
	}

	ticketID, err := uuid.Parse(request.TicketId)
	if err != nil {
		logger.Error(ctx, "ticket was not a valid uuid", "ticket_id", request.TicketId)
		return nil, status.Error(codes.InvalidArgument, "ticket was not a valid uuid")
	}

	ctx = logging.WithTicketID(ctx, request.TicketId)

	result, err := s.resultService.GetResultLink(ctx, ticketID)
	if err != nil {

		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}

		if errors.Is(err, service.ErrJobNotComplete) {
			return nil, status.Error(codes.NotFound, "job has not completed yet. Result does not exist yet.")
		}

		if errors.Is(err, job.ErrRecordNotFound) {
			logger.Error(ctx, "ticket was not found", "ticket_id", request.TicketId)
			return nil, status.Error(codes.NotFound, "ticket was not found")
		}

		logger.Error(ctx, "error getting results link", "ticket_id", request.TicketId, "error", err)
		return nil, status.Error(codes.Internal, "error getting results link")
	}

	return &batchv1.ResultLinkResponse{
		Link:    result.Link,
		Expires: timestamppb.New(result.Expires),
	}, nil
}

type CountingReader struct {
	reader  io.Reader
	read    int64
	maximum int64
}

func (r *CountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)

	if r.read > r.maximum {
		return n, fmt.Errorf("%w: maximum is %d bytes", service.ErrDocumentTooLarge, r.maximum)
	}

	return n, err
}

func (r *CountingReader) SizeBytes() int64 {
	return r.read
}

func shutdownGrpcServer(server *grpc.Server, logger logging.Logger) {
	ctx := context.Background()

	doneCh := make(chan struct{}, 1)
	go func() {
		logger.Info(ctx, "gracefully stopping all GRPC connections", "component", logging.ComponentGRPC)
		server.GracefulStop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		logger.Debug(ctx, "graceful stop complete", "component", logging.ComponentGRPC)
	case <-time.After(shutdownServerTimeout):
		logger.Info(ctx, "force stopping all GRPC connections", "component", logging.ComponentGRPC)
		server.Stop()
	}
}

func convertTicketToGRPCTicket(ctx context.Context, t *job.Ticket, logger logging.Logger) *batchv1.Ticket {
	grpcTicket := batchv1.Ticket{
		Id: t.ID.String(),
	}

	switch t.ContentType {
	case string(config.ContentTypePDF):
		grpcTicket.ContentType = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF
	case string(config.ContentTypeHTML):
		grpcTicket.ContentType = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_HTML
	case string(config.ContentTypeEmail):
		grpcTicket.ContentType = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_RFC822
	default:
		grpcTicket.ContentType = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_UNSPECIFIED
		logger.Warn(ctx, fmt.Sprintf("found an unmapped ticket content type: %v", t.ContentType))
	}

	switch t.Status {
	case job.Queued:
		grpcTicket.TicketStatus = batchv1.TicketStatus_TICKET_STATUS_QUEUED
	case job.Processing:
		grpcTicket.TicketStatus = batchv1.TicketStatus_TICKET_STATUS_PROCESSING
	case job.Complete:
		grpcTicket.TicketStatus = batchv1.TicketStatus_TICKET_STATUS_COMPLETE
	case job.Failed:
		grpcTicket.TicketStatus = batchv1.TicketStatus_TICKET_STATUS_FAILED
	default:
		grpcTicket.TicketStatus = batchv1.TicketStatus_TICKET_STATUS_UNSPECIFIED
		logger.Warn(ctx, fmt.Sprintf("found an unmapped ticket status: %v", string(t.Status)))
	}

	if t.StatusReason != nil {
		switch *t.StatusReason {
		case job.EnqueueFailed:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_ENQUEUE_FAILED
		case job.StorageReadFailed:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_STORAGE_READ_FAILED
		case job.StorageReadNil:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_STORAGE_READ_NIL
		case job.ConvertFailed:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_CONVERT_FAILED
		case job.ResponseReaderNil:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_RESPONSE_READER_NIL
		case job.StorageWriteFailed:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_STORAGE_WRITE_FAILED
		default:
			grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_UNSPECIFIED
			logger.Warn(ctx, fmt.Sprintf("found an unmapped ticket status reason: %v", string(*t.StatusReason)))
		}
	} else {
		grpcTicket.TicketStatusReason = batchv1.TicketStatusReason_TICKET_STATUS_REASON_UNSPECIFIED
	}

	grpcTicket.CreatedAt = timestamppb.New(t.CreatedAt)
	grpcTicket.UpdatedAt = timestamppb.New(t.UpdatedAt)
	return &grpcTicket
}

func convertGrpcDocumentTypeToContentType(dct batchv1.DocumentContentType) (config.ContentType, error) {
	switch dct {
	case batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF:
		return config.ContentTypePDF, nil
	case batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_HTML:
		return config.ContentTypeHTML, nil
	case batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_RFC822:
		return config.ContentTypeEmail, nil
	default:
		return config.ContentTypeUndefined, fmt.Errorf("document type %v did not have a valid content type", dct.String())
	}
}

func configureInterceptors(cfg config.Config) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	interceptors := []interceptors.GrpcInterceptor{
		&interceptors.TimeoutGrpcInterceptor{Config: cfg},
		&interceptors.RequestIDGrpcInterceptor{},
	}
	unaryIntercepts := []grpc.UnaryServerInterceptor{}
	streamingIntercepts := []grpc.StreamServerInterceptor{}

	for _, interceptor := range interceptors {
		unaryIntercepts = append(unaryIntercepts, interceptor.Unary)
		streamingIntercepts = append(streamingIntercepts, interceptor.Streaming)
	}

	return unaryIntercepts, streamingIntercepts
}
