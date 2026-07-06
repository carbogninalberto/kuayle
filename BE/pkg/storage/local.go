package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalBackend stores files on the local filesystem.
type LocalBackend struct {
	dir     string
	urlBase string
}

func NewLocalBackend(dir, urlBase string) (*LocalBackend, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating upload dir: %w", err)
	}
	return &LocalBackend{dir: dir, urlBase: urlBase}, nil
}

func (b *LocalBackend) Put(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	path, err := b.pathForKey(key)
	if err != nil {
		return 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()
	n, err := io.Copy(f, r)
	if err != nil {
		return 0, fmt.Errorf("writing file: %w", err)
	}
	return n, nil
}

func (b *LocalBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := b.pathForKey(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (b *LocalBackend) Delete(_ context.Context, key string) error {
	path, err := b.pathForKey(key)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (b *LocalBackend) URL(_ context.Context, key string) (string, error) {
	return fmt.Sprintf("%s/%s", b.urlBase, key), nil
}

func (b *LocalBackend) pathForKey(key string) (string, error) {
	clean := filepath.Clean(key)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid storage key")
	}
	return filepath.Join(b.dir, clean), nil
}
