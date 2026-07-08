package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemHandlerStartUpdateRequiresSysAdmin(t *testing.T) {
	e := echo.New()
	userID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/update", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	h := NewSystemHandler("http://updater", "token", func(uuid.UUID) bool { return false })

	err := h.StartUpdate(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSystemHandlerStartUpdateDisabledWhenNotConfigured(t *testing.T) {
	e := echo.New()
	userID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/update", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	h := NewSystemHandler("", "", func(id uuid.UUID) bool { return id == userID })

	err := h.StartUpdate(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "UPDATER_DISABLED")
}

func TestSystemHandlerStartUpdateProxiesToUpdater(t *testing.T) {
	e := echo.New()
	userID := uuid.New()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/update", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		called = true
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"running":true,"message":"System update started"}`))
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	h := NewSystemHandler(server.URL, "secret", func(id uuid.UUID) bool { return id == userID })

	err := h.StartUpdate(c)

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "System update started")
}
