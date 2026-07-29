package main

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func noopMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
}

// TestRoutePermissionManifest enforces deny-by-default for personal access
// tokens: every registered route must appear in the manifest below, either
// with its required permission ("perm:<code>") or in an explicit exemption
// category. Adding a route without choosing an entry fails this test.
func TestRoutePermissionManifest(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, &appMiddleware{
		auth:                   noopMiddleware(),
		authRateLimit:          noopMiddleware(),
		publicRateLimit:        noopMiddleware(),
		publicAssetRateLimit:   noopMiddleware(),
		workspaceMembership:    noopMiddleware(),
		devMachineDemoGuard:    noopMiddleware(),
		machineEventsRateLimit: noopMiddleware(),
		machineLogsRateLimit:   noopMiddleware(),
	})

	manifest := routePermissionManifest()
	seen := make(map[string]bool, len(manifest))
	for _, r := range e.Routes() {
		if r.Method == echo.RouteNotFound {
			continue // synthetic group entry, not a real route
		}
		key := r.Method + " " + r.Path
		category, ok := manifest[key]
		require.True(t, ok, "route %s has no permission manifest entry (deny-by-default: pick one)", key)
		require.NotEmpty(t, category)
		seen[key] = true
	}
	for key := range manifest {
		require.True(t, seen[key], "manifest entry %s matches no registered route", key)
	}
}

// routePermissionManifest is the audit list of how personal access tokens may
// reach each route:
//
//	"public"      no authentication at all
//	"token"       /api/tokens itself; the handler rejects PAT callers
//	"session"     RequireSession: interactive sessions (JWT) only
//	"owner"       RequireOwner: workspace owner, interactive sessions only
//	"perm:<code>" RequirePermission: PAT needs <code> in scopes, then RBAC
func routePermissionManifest() map[string]string {
	w := "/api/workspaces/:slug" // workspace-scoped prefix, keeps entries short
	manifest := map[string]string{
		// Public, unauthenticated
		"GET /health":                         "public",
		"GET /ready":                          "public",
		"POST /api/auth/register":             "public",
		"POST /api/auth/login":                "public",
		"POST /api/auth/refresh":              "public",
		"POST /api/auth/logout":               "public",
		"GET /api/public/share/:token":        "public",
		"GET /api/public/share/:token/issues": "public",
		"GET /api/public/assets/:token":       "public",
		"POST /api/github/webhook":            "public",
		"POST /api/dev-machine-ingest/events": "public",
		"POST /api/dev-machine-ingest/logs":   "public",

		// Token management (handler rejects PAT callers)
		"GET /api/tokens":        "token",
		"POST /api/tokens":       "token",
		"DELETE /api/tokens/:id": "token",

		// Interactive-session-only
		"PATCH /api/auth/me":                    "session",
		"PATCH /api/preferences":                "session",
		"GET /api/system/update-status":         "session",
		"POST /api/system/update":               "session",
		"POST /api/workspaces":                  "session",
		"PATCH /api/notifications/:id":          "session",
		"POST /api/notifications/:id/read":      "session",
		"POST /api/notifications/:id/unread":    "session",
		"POST /api/notifications/:id/snooze":    "session",
		"POST /api/notifications/:id/unsnooze":  "session",
		"POST /api/notifications/:id/archive":   "session",
		"POST /api/notifications/:id/unarchive": "session",
		"POST /api/notifications/mark-all-read": "session",

		// User-level reads
		"GET /api/auth/me":       "perm:account:read",
		"GET /api/preferences":   "perm:account:read",
		"GET /api/workspaces":    "perm:workspaces:read",
		"GET /api/notifications": "perm:notifications:read",
	}

	session := []string{
		"POST " + w + "/teams/:teamId/leave",
		"POST " + w + "/teams/:teamId/cycles",
		"PATCH " + w + "/teams/:teamId/cycles/:cycleId",
		"POST " + w + "/teams/:teamId/cycles/:cycleId/complete",
		"DELETE " + w + "/teams/:teamId/cycles/:cycleId",
		"POST " + w + "/issues/:identifier/subscribe",
		"DELETE " + w + "/issues/:identifier/subscribe",
		"POST " + w + "/views",
		"PATCH " + w + "/views/:id",
		"DELETE " + w + "/views/:id",
		"GET " + w + "/github/status",
		"GET " + w + "/github/auto-transitions",
		"POST " + w + "/favorites",
		"DELETE " + w + "/favorites/:id",
		"POST " + w + "/shared-links",
		"PATCH " + w + "/shared-links/:id",
		"DELETE " + w + "/shared-links/:id",
		"GET " + w + "/assets/:assetId",
		"POST " + w + "/issues/:identifier/prompt-assets",
		"GET " + w + "/ws",
	}
	for _, key := range session {
		manifest[key] = "session"
	}

	owner := []string{
		"PATCH " + w,
		"DELETE " + w,
		"GET " + w + "/ai-settings",
		"PATCH " + w + "/ai-settings",
	}
	for _, key := range owner {
		manifest[key] = "owner"
	}

	perm := map[string]string{
		"GET " + w:                         "workspaces:read",
		"POST " + w + "/invite":            "member:invite",
		"GET " + w + "/members":            "members:read",
		"PATCH " + w + "/members/:userId":  "member:invite",
		"DELETE " + w + "/members/:userId": "member:invite",

		"GET " + w + "/teams":            "teams:read",
		"POST " + w + "/teams":           "team:manage",
		"GET " + w + "/teams/:teamId":    "teams:read",
		"PATCH " + w + "/teams/:teamId":  "team:manage",
		"DELETE " + w + "/teams/:teamId": "team:manage",

		"GET " + w + "/teams/:teamId/statuses":              "teams:read",
		"POST " + w + "/teams/:teamId/statuses":             "team:manage",
		"PATCH " + w + "/teams/:teamId/statuses/:statusId":  "team:manage",
		"DELETE " + w + "/teams/:teamId/statuses/:statusId": "team:manage",

		"GET " + w + "/teams/:teamId/cycles":                   "cycles:read",
		"GET " + w + "/teams/:teamId/cycles/velocity":          "cycles:read",
		"GET " + w + "/teams/:teamId/cycles/:cycleId":          "cycles:read",
		"GET " + w + "/teams/:teamId/cycles/:cycleId/burndown": "cycles:read",
		"GET " + w + "/teams/:teamId/projects":                 "projects:read",

		"GET " + w + "/issues":                                          "issues:read",
		"POST " + w + "/issues":                                         "issue:create",
		"PATCH " + w + "/issues/bulk":                                   "issue:update",
		"DELETE " + w + "/issues/bulk":                                  "issue:delete_own",
		"GET " + w + "/issues/:identifier":                              "issues:read",
		"PATCH " + w + "/issues/:identifier":                            "issue:update",
		"DELETE " + w + "/issues/:identifier":                           "issue:delete_own",
		"POST " + w + "/issues/:identifier/duplicate":                   "issue:create",
		"POST " + w + "/issues/:identifier/convert-to-project":          "project:manage",
		"POST " + w + "/issues/:identifier/expand-description":          "issue:update",
		"GET " + w + "/issues/:identifier/comments":                     "comments:read",
		"POST " + w + "/issues/:identifier/comments":                    "issue:create",
		"POST " + w + "/issues/:identifier/comments/:commentId/resolve": "issue:update",
		"POST " + w + "/issues/:identifier/comments/:commentId/reopen":  "issue:update",
		"GET " + w + "/issues/:identifier/sub-issues":                   "issues:read",
		"POST " + w + "/issues/:identifier/sub-issues":                  "issue:create",
		"POST " + w + "/issues/:identifier/sub-issues/bulk":             "issue:create",
		"GET " + w + "/issues/:identifier/history":                      "issues:read",
		"POST " + w + "/issues/:identifier/triage/accept":               "issue:update",
		"POST " + w + "/issues/:identifier/triage/decline":              "issue:update",
		"POST " + w + "/issues/:identifier/relations":                   "issue:update",
		"GET " + w + "/issues/:identifier/relations":                    "issues:read",
		"DELETE " + w + "/issues/:identifier/relations/:relationId":     "issue:update",
		"GET " + w + "/issues/:identifier/github":                       "issues:read",

		"GET " + w + "/issue-templates":        "templates:read",
		"POST " + w + "/issue-templates":       "issue:create",
		"GET " + w + "/issue-templates/:id":    "templates:read",
		"PATCH " + w + "/issue-templates/:id":  "issue:create",
		"DELETE " + w + "/issue-templates/:id": "issue:create",

		"GET " + w + "/labels":        "labels:read",
		"POST " + w + "/labels":       "label:manage",
		"PATCH " + w + "/labels/:id":  "label:manage",
		"DELETE " + w + "/labels/:id": "label:manage",

		"GET " + w + "/projects":        "projects:read",
		"POST " + w + "/projects":       "project:manage",
		"GET " + w + "/projects/:id":    "projects:read",
		"PATCH " + w + "/projects/:id":  "project:manage",
		"DELETE " + w + "/projects/:id": "project:manage",

		"GET " + w + "/views":     "views:read",
		"GET " + w + "/views/:id": "views:read",

		"GET " + w + "/favorites":    "account:read",
		"GET " + w + "/shared-links": "account:read",

		"GET " + w + "/analytics/overview":     "analytics:read",
		"GET " + w + "/analytics/distribution": "analytics:read",
		"GET " + w + "/analytics/insights":     "analytics:read",
		"GET " + w + "/analytics/burnup":       "analytics:read",

		"GET " + w + "/webhooks":        "workspace:manage",
		"POST " + w + "/webhooks":       "workspace:manage",
		"PATCH " + w + "/webhooks/:id":  "workspace:manage",
		"DELETE " + w + "/webhooks/:id": "workspace:manage",

		"GET " + w + "/ai-settings/issue-copy-prompt": "issues:read",

		"GET " + w + "/github/setup":              "workspace:manage",
		"GET " + w + "/github/setup/callback":     "workspace:manage",
		"GET " + w + "/github/install":            "workspace:manage",
		"GET " + w + "/github/callback":           "workspace:manage",
		"DELETE " + w + "/github/disconnect":      "workspace:manage",
		"DELETE " + w + "/github/app":             "workspace:manage",
		"GET " + w + "/github/repos":              "workspace:manage",
		"POST " + w + "/github/repos":             "workspace:manage",
		"DELETE " + w + "/github/repos/:id":       "workspace:manage",
		"PATCH " + w + "/github/auto-transitions": "workspace:manage",
		"GET " + w + "/github/issue-links":        "issues:read",

		"POST " + w + "/upload": "issue:create",

		"GET " + w + "/dev-machines":                                                "dev_machine:read",
		"POST " + w + "/dev-machines":                                               "dev_machine:create",
		"DELETE " + w + "/dev-machines/bulk":                                        "dev_machine:admin",
		"POST " + w + "/dev-machines/bulk/permanent-delete":                         "dev_machine:admin",
		"GET " + w + "/dev-machine-names/suggestion":                                "dev_machine:create",
		"GET " + w + "/dev-machine-names/availability":                              "dev_machine:create",
		"GET " + w + "/dev-machine-policy":                                          "dev_machine:read",
		"PATCH " + w + "/dev-machine-policy":                                        "dev_machine:admin",
		"GET " + w + "/dev-machine-scope-settings":                                  "dev_machine:read",
		"GET " + w + "/dev-machine-scope-setting":                                   "dev_machine:read",
		"PUT " + w + "/dev-machine-scope-setting":                                   "dev_machine:manage",
		"DELETE " + w + "/dev-machine-scope-setting":                                "dev_machine:manage",
		"GET " + w + "/dev-machine-environments":                                    "dev_machine:read",
		"POST " + w + "/dev-machine-environments":                                   "dev_machine:admin",
		"GET " + w + "/dev-machine-environments/:environmentId":                     "dev_machine:read",
		"DELETE " + w + "/dev-machine-environments/:environmentId":                  "dev_machine:admin",
		"GET " + w + "/dev-machine-providers":                                       "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId":                                     "dev_machine:read",
		"PATCH " + w + "/dev-machines/:machineId":                                   "dev_machine:manage",
		"DELETE " + w + "/dev-machines/:machineId":                                  "dev_machine:admin",
		"POST " + w + "/dev-machines/:machineId/permanent-delete":                   "dev_machine:admin",
		"POST " + w + "/dev-machines/:machineId/start":                              "dev_machine:manage",
		"POST " + w + "/dev-machines/:machineId/stop":                               "dev_machine:manage",
		"POST " + w + "/dev-machines/:machineId/pause":                              "dev_machine:manage",
		"POST " + w + "/dev-machines/:machineId/teardown":                           "dev_machine:manage",
		"POST " + w + "/dev-machines/:machineId/activity":                           "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/checkouts":                           "dev_machine:read",
		"POST " + w + "/dev-machines/:machineId/checkouts":                          "dev_machine:manage",
		"GET " + w + "/dev-machines/:machineId/events":                              "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/logs":                                "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/services":                            "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/providers":                           "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/resource-usage":                      "dev_machine:read",
		"POST " + w + "/dev-machines/:machineId/services/:service/launch":           "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/terminal-sessions":                   "dev_machine:read",
		"POST " + w + "/dev-machines/:machineId/terminal-sessions":                  "dev_machine:read",
		"POST " + w + "/dev-machines/:machineId/terminal-sessions/:sessionId/close": "dev_machine:read",
		"GET " + w + "/dev-machines/:machineId/agent-runs":                          "dev_machine:read",
		"POST " + w + "/dev-machines/:machineId/agent-runs":                         "dev_machine:manage",
		"GET " + w + "/agent-runs":                                                  "dev_machine:read",
		"GET " + w + "/agent-runs/:agentRunId":                                      "dev_machine:read",
		"POST " + w + "/agent-runs/:agentRunId/cancel":                              "dev_machine:manage",
		"GET " + w + "/agent-runs/:agentRunId/trace":                                "dev_machine:read",
	}
	for key, code := range perm {
		manifest[key] = "perm:" + code
	}
	return manifest
}
