package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, method, origin, cookie, fetch, auth string
		status                                    int
	}{
		{"trusted mutation", "POST", "https://app.example.com", "__Host-kuayle-access=x", "same-origin", "", 204},
		{"sibling mutation", "POST", "https://machine.app.example.com", "__Host-kuayle-access=x", "same-site", "", 403},
		{"null origin", "POST", "null", "__Host-kuayle-access=x", "", "", 403},
		{"missing origin cookie", "POST", "", "__Host-kuayle-access=x", "", "", 403},
		{"login csrf", "POST", "https://machine.app.example.com", "", "same-site", "", 403},
		{"origin suppressed", "POST", "", "", "same-site", "", 403},
		{"bearer API", "POST", "", "", "", "Bearer token", 204},
		{"webhook", "POST", "", "", "", "", 204},
		{"normal navigation", "GET", "", "__Host-kuayle-access=x", "none", "", 204},
		{"bearer cannot bypass hostile origin", "POST", "https://machine.app.example.com", "", "same-site", "Bearer token", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.Use(BrowserOrigin("https://app.example.com"))
			e.Any("/", func(c echo.Context) error { return c.NoContent(204) })
			req := httptest.NewRequest(tc.method, "https://app.example.com/", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Cookie", tc.cookie)
			req.Header.Set("Sec-Fetch-Site", tc.fetch)
			req.Header.Set("Authorization", tc.auth)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}
func TestBrowserOriginRejectsSiblingWebSocket(t *testing.T) {
	e := echo.New()
	e.Use(BrowserOrigin("https://app.example.com"))
	e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	req := httptest.NewRequest("GET", "https://app.example.com/", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Origin", "https://machine.app.example.com")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, 403, rec.Code)
}
