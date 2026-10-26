package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endlessBody counts bytes consumed from an unauthenticated request body.
type endlessBody struct{ read int64 }

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	b.read += int64(len(p))
	return len(p), nil
}

// The webhook is public and reads its body before the signature check, so the
// body must be bounded rather than buffered without limit.
func TestGitHubWebhookBoundsUnauthenticatedBody(t *testing.T) {
	body := &endlessBody{}
	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook", io.NopCloser(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	svc := service.NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", &service.GlobalGitHubAppConfig{AppID: 1, WebhookSecret: "secret"})

	require.NoError(t, NewGitHubHandler(svc).HandleWebhook(c))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.LessOrEqual(t, body.read, int64(maxGitHubWebhookBytes+64*1024))
}
