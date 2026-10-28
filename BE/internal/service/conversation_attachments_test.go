package service

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStorage is an in-memory storage.Backend.
type memoryStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemoryStorage() *memoryStorage { return &memoryStorage{objects: map[string][]byte{}} }

func (m *memoryStorage) Put(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return int64(len(data)), nil
}

func (m *memoryStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memoryStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memoryStorage) URL(_ context.Context, key string) (string, error) { return "/" + key, nil }

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return buf.Bytes()
}

func TestImageStoreValidatesContentNotName(t *testing.T) {
	store := newMemoryStorage()
	images := NewImageStore(store)
	saved, err := images.Save(context.Background(), "../../evil\x00name.html", testPNG(t, 3, 2))
	require.NoError(t, err)
	assert.Equal(t, "image/png", saved.ContentType)
	assert.Equal(t, 3, saved.Width)
	assert.Equal(t, 2, saved.Height)
	assert.Equal(t, "evilname.png", saved.Filename, "the extension follows the content")
	assert.True(t, strings.HasPrefix(saved.StorageKey, "conversation-"))
	assert.Contains(t, store.objects, saved.StorageKey)

	_, err = images.Save(context.Background(), "image.png", []byte("<html><script>alert(1)</script>"))
	require.ErrorIs(t, err, ErrAttachmentUnsupported)
	_, err = images.Save(context.Background(), "big.png", make([]byte, domain.ConversationAttachmentMaxBytes+1))
	require.ErrorIs(t, err, ErrAttachmentTooLarge)
	_, err = readImage(bytes.NewReader(make([]byte, domain.ConversationAttachmentMaxBytes+1)))
	require.ErrorIs(t, err, ErrAttachmentTooLarge)
	assert.Len(t, store.objects, 1, "rejected uploads are never stored")
}

func attachmentFixture(store *memoryStorage, size int) domain.ConversationAttachment {
	key := "conversation-" + uuid.NewString() + ".png"
	store.objects[key] = bytes.Repeat([]byte{1}, size)
	return domain.ConversationAttachment{ID: uuid.New(), StorageKey: key, Filename: "shot.png", ContentType: "image/png", Size: int64(size), Width: 640, Height: 480}
}

func TestModelMessagesDescribeImagesWithoutVision(t *testing.T) {
	store := newMemoryStorage()
	svc := &ConversationService{images: NewImageStore(store)}
	image := attachmentFixture(store, 10)
	history := []domain.ConversationMessage{{Role: "user", Content: "What is wrong here?", Attachments: []domain.ConversationAttachment{image}}}

	messages := svc.modelMessages(context.Background(), history, false)
	require.Len(t, messages, 1)
	assert.Empty(t, messages[0].Parts)
	assert.Equal(t, "What is wrong here?\n\n[image attached: shot.png, 640x480, id="+image.ID.String()+"]", messages[0].Content)
	data, err := json.Marshal(messages[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "image_url")
}

func TestModelMessagesSendNewestImagesAsPartsWithinBounds(t *testing.T) {
	store := newMemoryStorage()
	svc := &ConversationService{images: NewImageStore(store)}
	oldest := attachmentFixture(store, 10)
	huge := attachmentFixture(store, visionMaxImageBytes+1)
	recent := []domain.ConversationAttachment{attachmentFixture(store, 10), attachmentFixture(store, 10), attachmentFixture(store, 10), attachmentFixture(store, 10)}
	history := []domain.ConversationMessage{
		{Role: "user", Content: "old", Attachments: []domain.ConversationAttachment{oldest}},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "", Attachments: append([]domain.ConversationAttachment{huge}, recent...)},
	}

	messages := svc.modelMessages(context.Background(), history, true)
	require.Len(t, messages, 3)
	assert.Empty(t, messages[0].Parts, "only the newest four images are sent")
	assert.Contains(t, messages[0].Content, oldest.ID.String(), "older images stay described")
	parts := messages[2].Parts
	require.Len(t, parts, 1+len(recent), "the oversized image is described, not sent")
	assert.Equal(t, "text", parts[0].Type)
	assert.Contains(t, parts[0].Text, huge.ID.String())
	for _, part := range parts[1:] {
		assert.Equal(t, "image_url", part.Type)
		assert.True(t, strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,"))
	}
	data, err := json.Marshal(messages[2])
	require.NoError(t, err)
	var wire struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(data, &wire), string(data[:min(len(data), 200)]))
	assert.Equal(t, "user", wire.Role)
	require.Len(t, wire.Content, 5)
	assert.Equal(t, "image_url", wire.Content[1].Type)
}

func TestVisionProbeReportsWhetherTheModelSawTheImage(t *testing.T) {
	var body []byte
	answer := "Red."
	status := http.StatusOK
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":{"message":"image input is not supported by secret-key"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": answer}}}})
	}))
	defer provider.Close()
	svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))

	result := svc.probeVision(context.Background(), provider.URL, "vision-model", "secret-key")
	assert.True(t, result.Answered)
	assert.True(t, result.SawImage)
	assert.Equal(t, "Red.", result.Answer)
	assert.Contains(t, string(body), `"type":"image_url"`)
	assert.Contains(t, string(body), `data:image/png;base64,`)

	answer = "I cannot view images."
	result = svc.probeVision(context.Background(), provider.URL, "vision-model", "secret-key")
	assert.True(t, result.Answered)
	assert.False(t, result.SawImage)

	status = http.StatusBadRequest
	result = svc.probeVision(context.Background(), provider.URL, "vision-model", "secret-key")
	assert.False(t, result.Answered)
	assert.False(t, result.SawImage)
	assert.Contains(t, result.Error, "image input is not supported")
	assert.NotContains(t, result.Error, "secret-key")
}
