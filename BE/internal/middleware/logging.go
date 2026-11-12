package middleware

import (
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

func Logging() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)
			latency := time.Since(start)

			status := 0
			if res, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil {
				status = res.Status
			}
			log.WithFields(log.Fields{
				"method": c.Request().Method,
				// The route template, not the raw URL: invite, share, wiki and
				// asset tokens live in path parameters and must not reach logs.
				"path":    domain.RedactSecrets(c.Path()),
				"status":  status,
				"latency": latency.String(),
				"ip":      c.RealIP(),
			}).Info("request")

			return err
		}
	}
}
