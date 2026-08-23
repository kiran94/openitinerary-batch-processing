package storage

import (
	"context"
	"errors"
	"io"
	"os"
)

type LocalStorage struct {
	createFile func(name string) (io.WriteCloser, error)
}

func NewLocalStorage() *LocalStorage {
	return &LocalStorage{
		createFile: createLocalFile,
	}
}

func createLocalFile(name string) (io.WriteCloser, error) {
	return os.Create(name)
}

func (l LocalStorage) Write(ctx context.Context, reader io.ReadCloser, output string) (err error) {
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
