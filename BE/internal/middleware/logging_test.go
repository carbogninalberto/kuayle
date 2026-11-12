package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type captureHook struct {
	mu      sync.Mutex
	entries []log.Entry
}

func (h *captureHook) Levels() []log.Level { return log.AllLevels }

func (h *captureHook) Fire(entry *log.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, *entry)
	return nil
}

// The request logger records the route template, never the raw path, so
// invite/share/wiki/asset secrets and connection-link bodies cannot reach
// logs. This is the logrus-level proof that the middleware chooses c.Path().
func TestLoggingNeverRecordsSecrets(t *testing.T) {
	hook := &captureHook{}
	log.AddHook(hook)
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(log.LevelHooks{}) })

	e := echo.New()
	e.Use(Logging())
	e.GET("/api/invite/:token", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	e.POST("/api/cli/connect", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	for _, request := range []struct {
		method, path, body, secret string
	}{
		{http.MethodGet, "/api/invite/invite-secret-123", "", "invite-secret-123"},
		{http.MethodPost, "/api/cli/connect", `{"code":"kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789"}`, "kuayle_cl_abcdefghijklmnopqrstuvwxyz0123456789"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()
	require.Len(t, hook.entries, 2)
	paths := []string{}
	for _, entry := range hook.entries {
		path, _ := entry.Data["path"].(string)
		paths = append(paths, path)
		rendered, err := entry.String()
		require.NoError(t, err)
		require.NotContains(t, rendered, "invite-secret-123")
		require.NotContains(t, rendered, "kuayle_cl_")
	}
	require.ElementsMatch(t, []string{"/api/invite/:token", "/api/cli/connect"}, paths)
}
