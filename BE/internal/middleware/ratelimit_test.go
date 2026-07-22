package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMachineTokenRateLimitSeparatesCollectorIdentities(t *testing.T) {
	e := echo.New()
	handler := MachineTokenRateLimit(0, 1)(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	tokenA := strings.Repeat("a", 64)
	tokenB := strings.Repeat("b", 64)

	request := func(token, remoteAddress string) int {
		req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
		req.RemoteAddr = remoteAddress
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		recorder := httptest.NewRecorder()
		require.NoError(t, handler(e.NewContext(req, recorder)))
		return recorder.Code
	}

	require.Equal(t, http.StatusNoContent, request(tokenA, "192.0.2.1:1000"))
	require.Equal(t, http.StatusTooManyRequests, request(tokenA, "192.0.2.1:1001"))
	require.Equal(t, http.StatusNoContent, request(tokenB, "192.0.2.1:1002"), "collectors behind one IP must have independent buckets")
	require.Equal(t, http.StatusTooManyRequests, request(tokenA, "198.51.100.1:1000"), "one collector must retain its bucket across IPs")
}

func TestMachineTokenRateLimitFallsBackToIPWithoutCredential(t *testing.T) {
	e := echo.New()
	handler := MachineTokenRateLimit(0, 1)(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	request := func(authorization string) int {
		req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
		req.RemoteAddr = "192.0.2.1:1000"
		req.Header.Set(echo.HeaderAuthorization, authorization)
		recorder := httptest.NewRecorder()
		require.NoError(t, handler(e.NewContext(req, recorder)))
		return recorder.Code
	}

	require.Equal(t, http.StatusNoContent, request("Bearer malformed-one"))
	require.Equal(t, http.StatusTooManyRequests, request("Bearer malformed-two"))
}

func TestMachineTokenRateLimitKeyDoesNotRetainCredential(t *testing.T) {
	token := strings.Repeat("c", 64)
	req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	key := machineTokenRateLimitKey(echo.New().NewContext(req, httptest.NewRecorder()))

	require.NotContains(t, key, token)
	require.True(t, strings.HasPrefix(key, "machine-token:"))
}
