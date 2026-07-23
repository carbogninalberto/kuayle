package middleware

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// DevMachineDemoGuard returns middleware that blocks non-sysadmin users from
// all authenticated Dev Machine API routes when demo mode is active.
//
// The predicate is expected to be config.DemoDevMachineAllowed (or an
// equivalent that returns true when demo mode is off and enforces SYSADMINS
// membership).  This middleware must only be applied after the Auth middleware
// has set the user_id context value.
func DevMachineDemoGuard(allowed func(uuid.UUID) bool) echo.MiddlewareFunc {
	if allowed == nil {
		allowed = func(uuid.UUID) bool { return false }
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID := GetUserID(c)
			if userID == uuid.Nil {
				return response.Forbidden(c)
			}
			if !allowed(userID) {
				return response.Forbidden(c)
			}
			return next(c)
		}
	}
}
