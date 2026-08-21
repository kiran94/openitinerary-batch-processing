package storage

import (
	"errors"
	"io"
	"os"
)

type LocalStorage struct {
	file       string
	createFile func(name string) (io.WriteCloser, error)
}

func NewLocalStorage(file string) *LocalStorage {
	return &LocalStorage{
		file:       file,
		createFile: createLocalFile,
	}
}

func createLocalFile(name string) (io.WriteCloser, error) {
	return os.Create(name)
}

func (l LocalStorage) Write(reader io.ReadCloser) (err error) {
	f, err := l.createFile(l.file)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	_, err = io.Copy(f, reader)
	return err
}
