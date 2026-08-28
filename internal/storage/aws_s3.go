package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type TransferClient interface {
	UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
	GetObject(ctx context.Context, input *transfermanager.GetObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.GetObjectOutput, error)
}

type AWSS3Storage struct {
	s3Client TransferClient
}

func NewAWSS3Storage(ctx context.Context) (*AWSS3Storage, error) {
	awsConfig, err := config.LoadDefaultConfig(ctx)

	if err != nil {
		return nil, err
	}

	s3Client := transfermanager.New(s3.NewFromConfig(awsConfig))
	return &AWSS3Storage{s3Client}, nil
}

func (a *AWSS3Storage) Write(ctx context.Context, w io.ReadCloser, output string) error {

	bucket, key, err := a.extractBucketKey(output)
	if err != nil {
		return err
	}

	_, uploadErr := a.s3Client.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   w,
	})

	if uploadErr != nil {
		return uploadErr
	}

	return nil
}

func (a *AWSS3Storage) Read(ctx context.Context, input string) (io.Reader, error) {
	bucket, key, err := a.extractBucketKey(input)
	if err != nil {
		return nil, err
	}

	output, err := a.s3Client.GetObject(ctx, &transfermanager.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})

	if err != nil {
		return nil, err
	}

	return output.Body, nil
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
