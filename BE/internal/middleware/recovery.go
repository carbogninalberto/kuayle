package middleware

import (
	"fmt"
	"runtime"

	"github.com/kuayle/kuayle-backend/pkg/response"
	log "github.com/sirupsen/logrus"
	"github.com/labstack/echo/v4"
)

func Recovery() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 2048)
					n := runtime.Stack(buf, false)
					log.WithFields(log.Fields{
						"panic": fmt.Sprintf("%v", r),
						"stack": string(buf[:n]),
					}).Error("panic recovered")
					_ = response.InternalError(c)
				}
			}()
			return next(c)
		}
	}
}
