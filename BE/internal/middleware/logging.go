package middleware

import (
	"time"

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
				"method":  c.Request().Method,
				"path":    c.Request().URL.Path,
				"status":  status,
				"latency": latency.String(),
				"ip":      c.RealIP(),
			}).Info("request")

			return err
		}
	}
}
