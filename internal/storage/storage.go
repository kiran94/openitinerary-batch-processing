package storage

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	Write(ctx context.Context, reader io.Reader, output string) error
	Read(ctx context.Context, input string) (io.ReadCloser, error)
	GetLink(ctx context.Context, input string) (string, time.Time, error)
}
