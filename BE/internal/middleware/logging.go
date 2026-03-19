package middleware

import (
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/labstack/echo/v4"
)

func Logging() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			latency := time.Since(start)

			log.WithFields(log.Fields{
				"method":  c.Request().Method,
				"path":    c.Request().URL.Path,
				"status":  c.Response().Status,
				"latency": latency.String(),
				"ip":      c.RealIP(),
			}).Info("request")

			return err
		}
	}
}
