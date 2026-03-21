package storage

import (
	"context"
	"io"
)

// Backend defines the interface for file storage operations.
type Backend interface {
	// Put stores the content read from r under the given key.
	Put(ctx context.Context, key string, r io.Reader, contentType string) (int64, error)

	// Get retrieves the content stored under the given key.
	// The caller must close the returned ReadCloser.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the content stored under the given key.
	Delete(ctx context.Context, key string) error

	// URL returns an accessible URL for the given key.
	URL(ctx context.Context, key string) (string, error)
}
