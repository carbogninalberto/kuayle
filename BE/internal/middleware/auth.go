package middleware

import (
	"strings"

	jwtpkg "github.com/carbon/carbon-backend/pkg/jwt"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func Auth(jwtSecret string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var tokenString string

			// Try cookie first
			cookie, err := c.Cookie("access_token")
			if err == nil && cookie.Value != "" {
				tokenString = cookie.Value
			}

			// Fallback to Authorization header
			if tokenString == "" {
				auth := c.Request().Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					tokenString = strings.TrimPrefix(auth, "Bearer ")
				}
			}

			if tokenString == "" {
				return response.Unauthorized(c)
			}

			claims, err := jwtpkg.ValidateToken(tokenString, jwtSecret)
			if err != nil {
				return response.Unauthorized(c)
			}

			c.Set(string(UserIDKey), claims.UserID)
			return next(c)
		}
	}
}
