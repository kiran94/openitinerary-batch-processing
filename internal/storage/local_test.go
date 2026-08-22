package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingWriteCloser struct {
	bytes.Buffer
	closed   bool
	closeErr error
}

func (w *trackingWriteCloser) Close() error {
	w.closed = true
	return w.closeErr
}

type failingReadCloser struct {
	err    error
	closed bool
}

func (r *failingReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestNewLocalStorage(t *testing.T) {
	storage := NewLocalStorage("test.json")

	assert.NotNil(t, storage.createFile)
}

func TestLocalStorageWrite(t *testing.T) {
	destination := &trackingWriteCloser{}
	storage := NewLocalStorage("test.json")
	var createdFile string
	storage.createFile = func(name string) (io.WriteCloser, error) {
		createdFile = name
		return destination, nil
	}

	err := storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("itinerary")),
		"test.json",
	)

	require.NoError(t, err)
	assert.Equal(t, "test.json", createdFile)
	assert.Equal(t, "itinerary", destination.String())
	assert.True(t, destination.closed)
}

func TestLocalStorageWriteReusesStorageForDifferentOutputs(t *testing.T) {
	destinations := make(map[string]*trackingWriteCloser)
	storage := NewLocalStorage("")
	storage.createFile = func(name string) (io.WriteCloser, error) {
		destination := &trackingWriteCloser{}
		destinations[name] = destination
		return destination, nil
	}

	err := storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("first itinerary")),
		"first.json",
	)
	require.NoError(t, err)

	err = storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("second itinerary")),
		"second.json",
	)
	require.NoError(t, err)

	assert.Equal(t, "first itinerary", destinations["first.json"].String())
	assert.True(t, destinations["first.json"].closed)
	assert.Equal(t, "second itinerary", destinations["second.json"].String())
	assert.True(t, destinations["second.json"].closed)
}

func TestLocalStorageWriteCreateError(t *testing.T) {
	createErr := errors.New("create failed")
	storage := NewLocalStorage("test.json")
	storage.createFile = func(string) (io.WriteCloser, error) {
		return nil, createErr
	}

	err := storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("itinerary")),
		"test.json",
	)

	require.ErrorIs(t, err, createErr)
}

func TestLocalStorageWriteCopyErrorClosesDestination(t *testing.T) {
	copyErr := errors.New("copy failed")
	destination := &trackingWriteCloser{}
	reader := &failingReadCloser{err: copyErr}
	storage := NewLocalStorage("test.json")
	storage.createFile = func(string) (io.WriteCloser, error) {
		return destination, nil
	}

	err := storage.Write(context.Background(), reader, "test.json")

	require.ErrorIs(t, err, copyErr)
	assert.True(t, destination.closed)
	assert.False(t, reader.closed)
}

func TestLocalStorageWriteCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	destination := &trackingWriteCloser{closeErr: closeErr}
	storage := NewLocalStorage("test.json")
	storage.createFile = func(string) (io.WriteCloser, error) {
		return destination, nil
	}

	err := storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("itinerary")),
		"test.json",
	)

	require.ErrorIs(t, err, closeErr)
	assert.True(t, destination.closed)
}

func TestLocalStorageWriteCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.json")
	storage := NewLocalStorage(path)

	err := storage.Write(
		context.Background(),
		io.NopCloser(strings.NewReader("itinerary")),
		path,
	)
	require.NoError(t, err)

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "itinerary", string(contents))
}
