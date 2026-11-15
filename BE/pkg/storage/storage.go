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

// Object is one stored object returned by Lister.
type Object struct {
	Key  string
	Size int64
	ETag string
}

// Lister is implemented by backends that can enumerate their objects; the
// S3 backend does. Backups use it to record which uploads exist when they
// live in object storage.
type Lister interface {
	List(ctx context.Context, prefix string) ([]Object, error)
}
