package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
)

type TransferClient interface {
	UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

type S3Client interface {
	GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type S3Presigner interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type AWSS3Storage struct {
	transferClient TransferClient
	s3Client       S3Client
	presign        S3Presigner
	config         config.Config
}

var _ Storage = (*AWSS3Storage)(nil)

func NewAWSS3Storage(ctx context.Context, config config.Config) (*AWSS3Storage, error) {
	uploadConcurrency := config.GetS3UploadConcurrency()
	sdkConfig, err := awsConfig.LoadDefaultConfig(ctx)

	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(sdkConfig)
	psc := s3.NewPresignClient(s3Client)
	transferClient := transfermanager.New(s3Client, configureTransferManager(uploadConcurrency))
	return &AWSS3Storage{transferClient: transferClient, s3Client: s3Client, presign: psc, config: config}, nil
}

func configureTransferManager(uploadConcurrency int) func(*transfermanager.Options) {
	return func(options *transfermanager.Options) {
		// BatchWorker instances already provide job-level concurrency. Keep part
		// concurrency low to bound per-worker memory, and increase it only when
		// measured upload latency justifies the additional part buffers.
		options.Concurrency = uploadConcurrency
	}
}

func (a *AWSS3Storage) Write(ctx context.Context, reader io.Reader, output string) error {

	bucket, key, err := a.extractBucketKey(output)
	if err != nil {
		return err
	}

	_, uploadErr := a.transferClient.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   reader,
	})

	if uploadErr != nil {
		return uploadErr
	}

	return nil
}

func (a *AWSS3Storage) Read(ctx context.Context, input string) (io.ReadCloser, error) {
	bucket, key, err := a.extractBucketKey(input)
	if err != nil {
		return nil, err
	}

	output, err := a.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})

	if err != nil {
		return nil, err
	}

	return output.Body, nil
}

func (a *AWSS3Storage) GetLink(ctx context.Context, input string) (string, time.Time, error) {
	bucket, key, err := a.extractBucketKey(input)
	if err != nil {
		return "", time.Time{}, err
	}

	expirationSeconds := a.config.GetS3PresignedExpirationSeconds()

	result, err := a.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, func(po *s3.PresignOptions) {
		po.Expires = time.Second * time.Duration(expirationSeconds)
	})

	if err != nil {
		return "", time.Time{}, err
	}

	expires := time.Now().UTC().Add(time.Duration(expirationSeconds) * time.Second)
	return result.URL, expires, nil
}

func (a AWSS3Storage) extractBucketKey(s string) (bucket string, key string, err error) {
	uri, parseErr := url.Parse(s)
	if parseErr != nil {
		return "", "", parseErr
	}

	if uri.Scheme != "s3" {
		err = fmt.Errorf("input url should be an s3 address. Got %v", s)
		return
	}

	bucket = uri.Host
	key = strings.TrimPrefix(uri.Path, "/")

	if bucket == "" {
		err = fmt.Errorf("s3 bucket cannot be empty")
		return
	}

	if key == "" {
		err = fmt.Errorf("s3 key cannot be empty")
		return
	}

	return bucket, key, nil
}
