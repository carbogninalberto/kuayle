package middleware

import (
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func RequirePermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, ok := c.Get("workspace_role").(string)
			if !ok {
				return response.Forbidden(c)
			}
			if !domain.HasPermission(role, permission) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}

func GetUserID(c echo.Context) uuid.UUID {
	id, _ := c.Get(string(UserIDKey)).(uuid.UUID)
	return id
}
