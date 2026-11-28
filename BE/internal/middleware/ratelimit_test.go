package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/clientip"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestRateLimitReturnsRetryAfter(t *testing.T) {
	e := echo.New()
	handler := RateLimit(0.5, 1)(func(c *echo.Context) error {
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

// A client that reaches the server directly must not choose its own rate-limit
// bucket with forwarding headers; only the trusted proxy networks may supply
// the client address.
func TestRateLimitIgnoresForwardingHeadersFromUntrustedPeers(t *testing.T) {
	e := echo.New()
	e.IPExtractor = clientip.FromRequest
	e.POST("/login", func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	}, RateLimit(0, 1))

	request := func(remoteAddress, forwardedFor, realIP string) int {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = remoteAddress
		if forwardedFor != "" {
			req.Header.Set(echo.HeaderXForwardedFor, forwardedFor)
		}
		if realIP != "" {
			req.Header.Set(echo.HeaderXRealIP, realIP)
		}
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, req)
		return recorder.Code
	}

	// Direct public client: spoofed headers do not open new buckets.
	require.Equal(t, http.StatusNoContent, request("203.0.113.7:4000", "198.51.100.1", ""))
	require.Equal(t, http.StatusTooManyRequests, request("203.0.113.7:4001", "198.51.100.2", ""))
	require.Equal(t, http.StatusTooManyRequests, request("203.0.113.7:4002", "", "198.51.100.3"))
	require.Equal(t, http.StatusTooManyRequests, request("203.0.113.7:4003", "198.51.100.4, 198.51.100.5", "198.51.100.6"))

	// Behind the Compose proxy, each forwarded client keeps its own bucket.
	require.Equal(t, http.StatusNoContent, request("172.18.0.5:5000", "198.51.100.10", ""))
	require.Equal(t, http.StatusTooManyRequests, request("172.18.0.5:5001", "198.51.100.10", ""))
	require.Equal(t, http.StatusNoContent, request("172.18.0.5:5002", "198.51.100.11", ""))
	// A client prepending a forged hop is still keyed on the address the proxy saw.
	require.Equal(t, http.StatusTooManyRequests, request("172.18.0.5:5003", "192.0.2.99, 198.51.100.10", ""))
}
