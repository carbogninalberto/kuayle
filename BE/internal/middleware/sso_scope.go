package middleware

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// RejectSSOScopedSession refuses durable or account-level actions from a
// session minted by workspace SSO. Such a session is only a credential for its
// own workspace: the workspace admin controls that IdP and could otherwise
// mint a subject for any account and take over its credentials.
func RejectSSOScopedSession() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if GetSessionSSOWorkspace(c) != uuid.Nil {
				return response.SSOSessionScoped(c)
			}
			return next(c)
		}
	}
}
