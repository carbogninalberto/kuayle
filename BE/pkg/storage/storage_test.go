package storage

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalBackend_PutGetDeleteURL(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocalBackend(dir, "/uploads")
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-image.png"
	content := "fake image data"

	// Put
	n, err := b.Put(ctx, key, strings.NewReader(content), "image/png")
	assert.NoError(t, err)
	assert.Equal(t, int64(len(content)), n)

	// URL
	url, err := b.URL(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, "/uploads/test-image.png", url)

	// Get
	rc, err := b.Get(ctx, key)
	assert.NoError(t, err)
	data, _ := io.ReadAll(rc)
	rc.Close()
	assert.Equal(t, content, string(data))

	// Delete
	err = b.Delete(ctx, key)
	assert.NoError(t, err)
	_, err = b.Get(ctx, key)
	assert.Error(t, err)
}

func TestLocalBackend_CreatesDir(t *testing.T) {
	dir := t.TempDir() + "/nested/uploads"
	b, err := NewLocalBackend(dir, "/uploads")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = b.Put(ctx, "test.png", strings.NewReader("data"), "image/png")
	assert.NoError(t, err)
}

func TestLocalBackend_RejectsPathTraversal(t *testing.T) {
	b, err := NewLocalBackend(t.TempDir(), "/uploads")
	require.NoError(t, err)

	_, err = b.Put(context.Background(), "../secret.txt", strings.NewReader("data"), "text/plain")
	assert.Error(t, err)

	_, err = b.Get(context.Background(), "../secret.txt")
	assert.Error(t, err)

	err = b.Delete(context.Background(), "../secret.txt")
	assert.Error(t, err)
}

func TestNewConfig_DefaultsToLocal(t *testing.T) {
	cfg := Config{}
	assert.Equal(t, Type(""), cfg.Type)

	// Empty type should select local backend
	dir := t.TempDir()
	cfg.LocalDir = dir
	cfg.LocalURL = "/uploads"
	b, err := New(cfg)
	require.NoError(t, err)

	url, err := b.URL(context.Background(), "file.png")
	assert.NoError(t, err)
	assert.Equal(t, "/uploads/file.png", url)
}

func TestNewConfig_UnsupportedType(t *testing.T) {
	cfg := Config{Type: "gcs"}
	_, err := New(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported storage type")
}
