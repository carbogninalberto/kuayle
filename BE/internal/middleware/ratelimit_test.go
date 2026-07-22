package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestRateLimitReturnsRetryAfter(t *testing.T) {
	e := echo.New()
	handler := RateLimit(0.5, 1)(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	first := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(httptest.NewRequest(http.MethodPost, "/ingest", nil), first)))
	require.Equal(t, http.StatusNoContent, first.Code)

	limited := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(httptest.NewRequest(http.MethodPost, "/ingest", nil), limited)))
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "2", limited.Header().Get("Retry-After"))
}
