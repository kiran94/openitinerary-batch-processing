package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	batchv1 "github.com/kiran94/openitinerary-batch-processing/proto/gen/go/openitinerary/batch/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	var host, filePath, contentType, email string

	flag.StringVar(&host, "host", "localhost:50051", "")
	flag.StringVar(&filePath, "filepath", "", "")
	flag.StringVar(&contentType, "content-type", "pdf", "")
	flag.StringVar(&email, "emails", "example@gmail.com", "")
	flag.Parse()

	var contentTypeProto batchv1.DocumentContentType
	switch contentType {
	case "pdf":
		contentTypeProto = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF
	case "html":
		contentTypeProto = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_HTML
	case "email":
		contentTypeProto = batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_RFC822
	default:
		return fmt.Errorf("unsupported content type '%v'", contentType)
	}

	if filePath == "" {
		return errors.New("filepath is required")
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, logging.HeaderRequestID, uuid.NewString())

	logger := logging.New(config.NewConfig())

	logger.Info(ctx, "connecting to grpc server", "host", host)
	con, err := grpc.NewClient(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() {
		if err := con.Close(); err != nil {
			logger.Error(ctx, "error closing grpc client", "error", err)
		}
	}()

	logger.Debug(ctx, "creating connection")
	client := batchv1.NewOpenItineraryBatchProcessingServiceClient(con)
	stream, err := client.Submit(ctx)
	if err != nil {
		return err
	}

	initialMetadataErr := stream.Send(&batchv1.SubmitRequest{
		Frame: &batchv1.SubmitRequest_Metadata{
			Metadata: &batchv1.SubmitMetadata{ContentType: contentTypeProto, NotificationEmails: []string{email}},
		},
	})

	if initialMetadataErr != nil {
		return initialMetadataErr
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	logger.Info(ctx, "Starting streaming")
	const streamingBufferSize = 32 * 1024 // 32KB
	buffer := make([]byte, streamingBufferSize)
	for {
		m, err := file.Read(buffer)
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		logger.Debug(ctx, "sending bytes", "byte_size", m)
		err = stream.Send(&batchv1.SubmitRequest{Frame: &batchv1.SubmitRequest_Chunk{Chunk: buffer[:m]}})

		if err != nil {
			return err
		}
	}

	logger.Debug(ctx, "closing and getting response")
	response, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	fmt.Println(response.Ticket.Id)

	md, err := stream.Header()
	if err != nil {
		return err
	}

	for k, v := range md {
		fmt.Printf("HEADER-> %v:%v\n", k, v)
	}

	return nil
}
