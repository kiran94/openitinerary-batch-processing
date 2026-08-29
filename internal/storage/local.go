package storage

import (
	"context"
	"errors"
	"io"
	"os"
)

type LocalStorage struct {
	createFile func(name string) (io.WriteCloser, error)
	openFile   func(name string) (io.ReadCloser, error)
}

var _ Storage = (*LocalStorage)(nil)

func NewLocalStorage() *LocalStorage {
	return &LocalStorage{
		createFile: createLocalFile,
		openFile:   openLocalFile,
	}
}

func createLocalFile(name string) (io.WriteCloser, error) {
	return os.Create(name)
}

func openLocalFile(name string) (io.ReadCloser, error) {
	return os.Open(name)
}

func (l LocalStorage) Write(ctx context.Context, reader io.Reader, output string) (err error) {
	f, err := l.createFile(output)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	_, err = io.Copy(f, reader)
	return err
}

func (l LocalStorage) Read(_ context.Context, input string) (io.ReadCloser, error) {
	return l.openFile(input)
}
