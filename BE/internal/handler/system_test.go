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

func TestSystemHandlerStartUpdateAnnouncesOnlyAcceptedRuns(t *testing.T) {
	userID := uuid.New()
	for _, tc := range []struct {
		status    int
		announced bool
	}{
		{http.StatusAccepted, true},
		{http.StatusConflict, false},
		{http.StatusInternalServerError, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"running":true}`))
		}))
		h := NewSystemHandler(server.URL, "secret", func(id uuid.UUID) bool { return id == userID })
		announced := false
		h.OnUpdateStarted(func() { announced = true })
		c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/system/update", nil), httptest.NewRecorder())
		c.Set("user_id", userID)

		require.NoError(t, h.StartUpdate(c))
		assert.Equal(t, tc.announced, announced, "updater status %d", tc.status)
		server.Close()
	}
}

func TestSystemHandlerUpdateStatusIncludesFailureDetails(t *testing.T) {
	userID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"running":false,"message":"Last update failed","state":"failed","error":"Release update failed: boom","output":"step 1\nRelease update failed: boom\n"}`))
	}))
	defer server.Close()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/system/update-status", nil), rec)
	c.Set("user_id", userID)
	h := NewSystemHandler(server.URL, "secret", func(id uuid.UUID) bool { return id == userID })

	require.NoError(t, h.UpdateStatus(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"enabled":true,"running":false,"message":"Last update failed","state":"failed","error":"Release update failed: boom","output":"step 1\nRelease update failed: boom\n"}`, rec.Body.String())
}

func TestSystemHandlerUpdateActivityRevealsOnlyRunningFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/status", r.URL.Path)
		_, _ = w.Write([]byte(`{"running":true,"message":"System update is running","state":"running","output":"secret"}`))
	}))
	defer server.Close()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/public/system/update-activity", nil), rec)
	h := NewSystemHandler(server.URL, "secret", nil)

	require.NoError(t, h.UpdateActivity(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"running":true}`, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestSystemHandlerUpdateActivityWithoutUpdater(t *testing.T) {
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/public/system/update-activity", nil), rec)

	require.NoError(t, NewSystemHandler("", "", nil).UpdateActivity(c))
	assert.JSONEq(t, `{"running":false}`, rec.Body.String())
}
