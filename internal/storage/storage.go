package storage

import (
	"context"
	"io"
)

type Storage interface {
	Write(ctx context.Context, w io.ReadCloser, output string) error
	Read(ctx context.Context, input string) (io.Reader, error)
}
