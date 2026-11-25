package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Instance configuration is for an interactive, unscoped sysadmin session:
// a member, a personal access token (even a sysadmin's, with every scope)
// and a session minted by one workspace's SSO are all refused, and none of
// the refusals is cacheable.
func TestInstanceConfigRequiresAnUnscopedSysadminSession(t *testing.T) {
	admin, member := uuid.New(), uuid.New()
	cfg := &config.Config{JWTSecret: "never-in-a-response-0123456789abcdef", WorkflowPipelines: true, BackupTimeout: 2 * time.Hour}
	for _, session := range []string{"member", "admin PAT", "admin SSO"} {
		t.Run(session, func(t *testing.T) {
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.Set("user_id", admin)
					switch session {
					case "member":
						c.Set("user_id", member)
					case "admin PAT":
						c.Set(mw.TokenScopesKey, []string{"account:read", "account:write", "workspaces:read"})
					case "admin SSO":
						c.Set(mw.SessionSSOWorkspaceKey, uuid.New())
					}
					return next(c)
				}
			}
			e := echo.New()
			svc := service.NewInstanceConfigService(cfg, nil)
			h := &appHandlers{instanceConfig: handler.NewInstanceConfigHandler(svc, func(id uuid.UUID) bool { return id == admin })}
			registerRoutes(e, h, testMiddleware(auth))
			for _, route := range []struct{ method, path, body string }{
				{"GET", "/api/system/config", ""},
				{"PUT", "/api/system/config/" + config.VarWorkflowPipelines, `{"value":"false"}`},
				{"DELETE", "/api/system/config/" + config.VarWorkflowPipelines, ""},
				{"POST", "/api/system/config/restart/validate", `{"changes":{"PASSKEYS_ENABLED":"false"}}`},
				{"POST", "/api/system/config/restart/apply", `{"changes":{"PASSKEYS_ENABLED":"false"}}`},
			} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				req.Header.Set("Content-Type", "application/json")
				e.ServeHTTP(rec, req)
				require.Equal(t, 403, rec.Code, route)
				require.NotContains(t, rec.Body.String(), "never-in-a-response")
			}
		})
	}
}
