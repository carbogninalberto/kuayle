package main

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestPushSettingsRequireUnscopedSysadminSession(t *testing.T) {
	admin, member := uuid.New(), uuid.New()
	for _, session := range []string{"member", "admin PAT", "admin SSO"} {
		t.Run(session, func(t *testing.T) {
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.Set("user_id", admin)
					switch session {
					case "member":
						c.Set("user_id", member)
					case "admin PAT":
						c.Set(mw.TokenScopesKey, []string{})
					case "admin SSO":
						c.Set(mw.SessionSSOWorkspaceKey, uuid.New())
					}
					return next(c)
				}
			}
			e := echo.New()
			h := &appHandlers{push: handler.NewPushHandler(nil, func(id uuid.UUID) bool { return id == admin })}
			registerRoutes(e, h, testMiddleware(auth))
			for _, route := range []struct{ method, path string }{{"GET", "/settings"}, {"PUT", "/settings"}, {"POST", "/keys"}, {"POST", "/test"}} {
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(route.method, "/api/system/push"+route.path, nil))
				require.Equal(t, 403, rec.Code, route)
			}
		})
	}
}
