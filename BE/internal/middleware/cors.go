package middleware

import (
	"net/url"

	"github.com/labstack/echo/v5"
	echomw "github.com/labstack/echo/v5/middleware"
	log "github.com/sirupsen/logrus"
)

func CORS(frontendURL string) echo.MiddlewareFunc {
	config := echomw.CORSConfig{
		AllowMethods:     []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}
	if isOrigin(frontendURL) {
		config.AllowOrigins = []string{frontendURL}
	} else {
		// Echo v5 rejects non-origin values at startup. Such a value never
		// matches a browser Origin header, so allow no cross-origin requests.
		log.WithField("frontend_url", frontendURL).Warn("FRONTEND_URL is not a bare origin; cross-origin API requests are disabled")
		config.UnsafeAllowOriginFunc = func(*echo.Context, string) (string, bool, error) {
			return "", false, nil
		}
	}
	return echomw.CORSWithConfig(config)
}

func isOrigin(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
