package storage

import "io"

type Storage interface {
	Write(w io.ReadCloser) error
}
