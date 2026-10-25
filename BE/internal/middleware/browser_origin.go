package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// BrowserOrigin supplies the CSRF boundary when workloads share the app's site.
// Browser mutations require the exact configured origin; non-browser webhooks
// and bearer clients without ambient cookies remain supported.
func BrowserOrigin(frontendOrigin string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			r := c.Request()
			safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
			if safe && !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				return next(c)
			}
			origin := r.Header.Get("Origin")
			if origin != "" {
				if origin != frontendOrigin {
					return echo.NewHTTPError(http.StatusForbidden, "Request origin is not allowed")
				}
			} else if len(r.Cookies()) > 0 || r.Header.Get("Sec-Fetch-Site") != "" {
				return echo.NewHTTPError(http.StatusForbidden, "Browser requests require the application origin")
			}
			return next(c)
		}
	}
}
