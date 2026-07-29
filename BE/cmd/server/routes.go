package main

import (
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v4"
)

// appHandlers groups every HTTP handler so route registration can run
// without a database (tests enumerate e.Routes() with zero-value handlers).
type appHandlers struct {
	inviteLink        *handler.InviteLinkHandler
	workspaceTransfer *handler.WorkspaceTransferHandler
	health            *handler.HealthHandler
	auth              *handler.AuthHandler
	workspace         *handler.WorkspaceHandler
	team              *handler.TeamHandler
	issue             *handler.IssueHandler
	label             *handler.LabelHandler
	project           *handler.ProjectHandler
	notif             *handler.NotificationHandler
	ws                *handler.WSHandler
	relation          *handler.IssueRelationHandler
	template          *handler.IssueTemplateHandler
	view              *handler.ViewHandler
	cycle             *handler.CycleHandler
	teamStatus        *handler.TeamStatusHandler
	fav               *handler.FavoriteHandler
	prefs             *handler.PreferencesHandler
	token             *handler.TokenHandler
	aiSettings        *handler.AISettingsHandler
	devMachine        *handler.DevMachineHandler
	analytics         *handler.AnalyticsHandler
	system            *handler.SystemHandler
	webhook           *handler.WebhookHandler
	sharedLink        *handler.SharedLinkHandler
	upload            *handler.UploadHandler
	github            *handler.GitHubHandler
}

// appMiddleware carries the pre-built route middleware whose construction
// needs config or repositories.
type appMiddleware struct {
	auth                   echo.MiddlewareFunc
	authRateLimit          echo.MiddlewareFunc
	publicRateLimit        echo.MiddlewareFunc
	publicAssetRateLimit   echo.MiddlewareFunc
	workspaceMembership    echo.MiddlewareFunc
	devMachineDemoGuard    echo.MiddlewareFunc
	machineEventsRateLimit echo.MiddlewareFunc
	machineLogsRateLimit   echo.MiddlewareFunc
}

func registerRoutes(e *echo.Echo, h *appHandlers, m *appMiddleware) {
	// Health
	e.GET("/health", h.health.Health)
	e.GET("/ready", h.health.Ready)

	// Auth (public) — rate limited: 5 requests/sec, burst of 10
	auth := e.Group("/api/auth", m.authRateLimit)
	auth.GET("/config", h.auth.Config)
	auth.POST("/register", h.auth.Register)
	auth.POST("/login", h.auth.Login)
	auth.POST("/refresh", h.auth.Refresh)
	auth.POST("/logout", h.auth.Logout)

	// Public share routes (no auth, rate limited)
	pub := e.Group("/api/public", m.publicRateLimit)
	pub.GET("/invite/:token", h.inviteLink.Preview)
	pub.GET("/share/:token", h.sharedLink.GetPublicMeta)
	pub.GET("/share/:token/issues", h.sharedLink.ListPublicIssues)
	e.GET("/api/public/assets/:token", h.upload.PublicAsset, m.publicAssetRateLimit)

	// Authenticated routes
	api := e.Group("/api", m.auth)

	// User
	api.GET("/auth/me", h.auth.Me, mw.RequirePermission("account:read"))
	api.PATCH("/auth/me", h.auth.UpdateProfile, mw.RequireSession())
	api.GET("/preferences", h.prefs.Get, mw.RequirePermission("account:read"))
	api.PATCH("/preferences", h.prefs.Update, mw.RequireSession())

	// Personal access tokens (PAT callers are rejected by the handler)
	api.GET("/tokens", h.token.List)
	api.POST("/tokens", h.token.Create)
	api.DELETE("/tokens/:id", h.token.Revoke)
	api.GET("/system/update-status", h.system.UpdateStatus, mw.RequireSession())
	api.POST("/system/update", h.system.StartUpdate, mw.RequireSession())

	// Workspaces (no workspace context needed for list/create)
	api.GET("/workspaces", h.workspace.List, mw.RequirePermission("workspaces:read"))
	api.POST("/workspaces", h.workspace.Create, mw.RequireSession())

	api.POST("/workspaces/import/preview", h.workspaceTransfer.Preview, mw.RequireSession())
	api.POST("/workspaces/import", h.workspaceTransfer.Import, mw.RequireSession())
	api.POST("/invite/:token/accept", h.inviteLink.Accept, mw.RequireSession())

	// Workspace-scoped routes
	ws := api.Group("/workspaces/:slug", m.workspaceMembership)
	ws.GET("", h.workspace.Get, mw.RequirePermission("workspaces:read"))
	ws.PATCH("", h.workspace.Update, mw.RequireOwner())
	ws.DELETE("", h.workspace.Delete, mw.RequireOwner())
	ws.GET("/export", h.workspaceTransfer.Export, mw.RequirePermission("workspace:transfer"))
	ws.POST("/invite-links", h.inviteLink.Create, mw.RequirePermission("member:invite"))
	ws.GET("/invite-links", h.inviteLink.List, mw.RequirePermission("member:invite"))
	ws.DELETE("/invite-links/:id", h.inviteLink.Revoke, mw.RequirePermission("member:invite"))
	ws.POST("/invite", h.workspace.Invite, mw.RequirePermission("member:invite"))
	ws.GET("/members", h.workspace.ListMembers, mw.RequirePermission("members:read"))
	ws.PATCH("/members/:userId", h.workspace.UpdateMemberRole, mw.RequirePermission("member:invite"))
	ws.DELETE("/members/:userId", h.workspace.RemoveMember, mw.RequirePermission("member:invite"))

	// Teams
	ws.GET("/teams", h.team.List, mw.RequirePermission("teams:read"))
	ws.POST("/teams", h.team.Create, mw.RequirePermission("team:manage"))
	ws.GET("/teams/:teamId", h.team.Get, mw.RequirePermission("teams:read"))
	ws.PATCH("/teams/:teamId", h.team.Update, mw.RequirePermission("team:manage"))
	ws.DELETE("/teams/:teamId", h.team.Delete, mw.RequirePermission("team:manage"))
	ws.POST("/teams/:teamId/leave", h.team.Leave, mw.RequireSession())

	// Team Statuses
	ws.GET("/teams/:teamId/statuses", h.teamStatus.List, mw.RequirePermission("teams:read"))
	ws.POST("/teams/:teamId/statuses", h.teamStatus.Create, mw.RequirePermission("team:manage"))
	ws.PATCH("/teams/:teamId/statuses/:statusId", h.teamStatus.Update, mw.RequirePermission("team:manage"))
	ws.DELETE("/teams/:teamId/statuses/:statusId", h.teamStatus.Delete, mw.RequirePermission("team:manage"))

	// Cycles (team-scoped)
	ws.GET("/teams/:teamId/cycles", h.cycle.List, mw.RequirePermission("cycles:read"))
	ws.POST("/teams/:teamId/cycles", h.cycle.Create, mw.RequireSession())
	ws.GET("/teams/:teamId/cycles/velocity", h.cycle.Velocity, mw.RequirePermission("cycles:read"))
	ws.GET("/teams/:teamId/cycles/:cycleId", h.cycle.Get, mw.RequirePermission("cycles:read"))
	ws.PATCH("/teams/:teamId/cycles/:cycleId", h.cycle.Update, mw.RequireSession())
	ws.POST("/teams/:teamId/cycles/:cycleId/complete", h.cycle.Complete, mw.RequireSession())
	ws.GET("/teams/:teamId/cycles/:cycleId/burndown", h.cycle.Burndown, mw.RequirePermission("cycles:read"))
	ws.DELETE("/teams/:teamId/cycles/:cycleId", h.cycle.Delete, mw.RequireSession())

	// Issues
	ws.GET("/issues", h.issue.List, mw.RequirePermission("issues:read"))
	ws.POST("/issues", h.issue.Create, mw.RequirePermission("issue:create"))
	ws.PATCH("/issues/bulk", h.issue.BulkUpdate, mw.RequirePermission("issue:update"))
	ws.DELETE("/issues/bulk", h.issue.BulkDelete, mw.RequirePermission("issue:delete_own"))
	ws.GET("/issues/:identifier", h.issue.Get, mw.RequirePermission("issues:read"))
	ws.PATCH("/issues/:identifier", h.issue.Update, mw.RequirePermission("issue:update"))
	ws.DELETE("/issues/:identifier", h.issue.Delete, mw.RequirePermission("issue:delete_own"))
	ws.POST("/issues/:identifier/subscribe", h.issue.Subscribe, mw.RequireSession())
	ws.DELETE("/issues/:identifier/subscribe", h.issue.Unsubscribe, mw.RequireSession())
	ws.POST("/issues/:identifier/duplicate", h.issue.Duplicate, mw.RequirePermission("issue:create"))
	ws.POST("/issues/:identifier/convert-to-project", h.issue.ConvertToProject, mw.RequirePermission("project:manage"))
	ws.POST("/issues/:identifier/expand-description", h.aiSettings.ExpandIssueDescription, mw.RequirePermission("issue:update"))
	ws.GET("/issues/:identifier/comments", h.issue.ListComments, mw.RequirePermission("comments:read"))
	ws.POST("/issues/:identifier/comments", h.issue.CreateComment, mw.RequirePermission("issue:create"))
	ws.POST("/issues/:identifier/comments/:commentId/resolve", h.issue.ResolveComment, mw.RequirePermission("issue:update"))
	ws.POST("/issues/:identifier/comments/:commentId/reopen", h.issue.ReopenComment, mw.RequirePermission("issue:update"))
	ws.GET("/issues/:identifier/sub-issues", h.issue.ListSubIssues, mw.RequirePermission("issues:read"))
	ws.POST("/issues/:identifier/sub-issues", h.issue.CreateSubIssue, mw.RequirePermission("issue:create"))
	ws.POST("/issues/:identifier/sub-issues/bulk", h.issue.BulkCreateSubIssues, mw.RequirePermission("issue:create"))
	ws.GET("/issues/:identifier/history", h.issue.GetHistory, mw.RequirePermission("issues:read"))
	ws.POST("/issues/:identifier/triage/accept", h.issue.TriageAccept, mw.RequirePermission("issue:update"))
	ws.POST("/issues/:identifier/triage/decline", h.issue.TriageDecline, mw.RequirePermission("issue:update"))

	// Issue Relations
	ws.POST("/issues/:identifier/relations", h.relation.Create, mw.RequirePermission("issue:update"))
	ws.GET("/issues/:identifier/relations", h.relation.List, mw.RequirePermission("issues:read"))
	ws.DELETE("/issues/:identifier/relations/:relationId", h.relation.Delete, mw.RequirePermission("issue:update"))

	// Issue Templates
	ws.GET("/issue-templates", h.template.List, mw.RequirePermission("templates:read"))
	ws.POST("/issue-templates", h.template.Create, mw.RequirePermission("issue:create"))
	ws.GET("/issue-templates/:id", h.template.Get, mw.RequirePermission("templates:read"))
	ws.PATCH("/issue-templates/:id", h.template.Update, mw.RequirePermission("issue:create"))
	ws.DELETE("/issue-templates/:id", h.template.Delete, mw.RequirePermission("issue:create"))

	// Labels
	ws.GET("/labels", h.label.List, mw.RequirePermission("labels:read"))
	ws.POST("/labels", h.label.Create, mw.RequirePermission("label:manage"))
	ws.PATCH("/labels/:id", h.label.Update, mw.RequirePermission("label:manage"))
	ws.DELETE("/labels/:id", h.label.Delete, mw.RequirePermission("label:manage"))

	// Projects
	ws.GET("/projects", h.project.List, mw.RequirePermission("projects:read"))
	ws.POST("/projects", h.project.Create, mw.RequirePermission("project:manage"))
	ws.GET("/projects/:id", h.project.Get, mw.RequirePermission("projects:read"))
	ws.PATCH("/projects/:id", h.project.Update, mw.RequirePermission("project:manage"))
	ws.DELETE("/projects/:id", h.project.Delete, mw.RequirePermission("project:manage"))
	ws.GET("/teams/:teamId/projects", h.project.ListByTeam, mw.RequirePermission("projects:read"))

	// Views
	ws.GET("/views", h.view.List, mw.RequirePermission("views:read"))
	ws.POST("/views", h.view.Create, mw.RequireSession())
	ws.GET("/views/:id", h.view.Get, mw.RequirePermission("views:read"))
	ws.PATCH("/views/:id", h.view.Update, mw.RequireSession())
	ws.DELETE("/views/:id", h.view.Delete, mw.RequireSession())

	// Analytics
	ws.GET("/analytics/overview", h.analytics.Overview, mw.RequirePermission("analytics:read"))
	ws.GET("/analytics/distribution", h.analytics.IssueDistribution, mw.RequirePermission("analytics:read"))
	ws.GET("/analytics/insights", h.analytics.Insights, mw.RequirePermission("analytics:read"))
	ws.GET("/analytics/burnup", h.analytics.Burnup, mw.RequirePermission("analytics:read"))

	// Webhooks
	ws.GET("/webhooks", h.webhook.List, mw.RequirePermission("workspace:manage"))
	ws.POST("/webhooks", h.webhook.Create, mw.RequirePermission("workspace:manage"))
	ws.PATCH("/webhooks/:id", h.webhook.Update, mw.RequirePermission("workspace:manage"))
	ws.DELETE("/webhooks/:id", h.webhook.Delete, mw.RequirePermission("workspace:manage"))

	// AI settings
	ws.GET("/ai-settings", h.aiSettings.Get, mw.RequireOwner())
	ws.GET("/ai-settings/issue-copy-prompt", h.aiSettings.GetIssueCopyPrompt, mw.RequirePermission("issues:read"))
	ws.PATCH("/ai-settings", h.aiSettings.Update, mw.RequireOwner())

	// GitHub integration (conditional)
	// Public webhook endpoint (no auth, signature-verified internally)
	e.POST("/api/github/webhook", h.github.HandleWebhook)
	e.POST("/api/dev-machine-ingest/events", h.devMachine.IngestEvent, m.machineEventsRateLimit)
	e.POST("/api/dev-machine-ingest/logs", h.devMachine.IngestLog, m.machineLogsRateLimit)

	// GitHub integration (workspace-scoped)
	ws.GET("/github/status", h.github.Status, mw.RequireSession())
	ws.GET("/github/setup", h.github.Setup, mw.RequirePermission("workspace:manage"))
	ws.GET("/github/setup/callback", h.github.SetupCallback, mw.RequirePermission("workspace:manage"))
	ws.GET("/github/install", h.github.InstallURL, mw.RequirePermission("workspace:manage"))
	ws.GET("/github/callback", h.github.Callback, mw.RequirePermission("workspace:manage"))
	ws.DELETE("/github/disconnect", h.github.Disconnect, mw.RequirePermission("workspace:manage"))
	ws.DELETE("/github/app", h.github.DeleteApp, mw.RequirePermission("workspace:manage"))
	ws.GET("/github/repos", h.github.ListRepos, mw.RequirePermission("workspace:manage"))
	ws.POST("/github/repos", h.github.LinkRepos, mw.RequirePermission("workspace:manage"))
	ws.DELETE("/github/repos/:id", h.github.UnlinkRepo, mw.RequirePermission("workspace:manage"))
	ws.GET("/github/auto-transitions", h.github.ListAutoTransitions, mw.RequireSession())
	ws.PATCH("/github/auto-transitions", h.github.UpdateAutoTransitions, mw.RequirePermission("workspace:manage"))
	ws.GET("/issues/:identifier/github", h.github.IssueGitHubActivity, mw.RequirePermission("issues:read"))
	ws.GET("/github/issue-links", h.github.AgentIssueLinks, mw.RequirePermission("issues:read"))

	// Dev Machines — guarded by demo-mode restriction when active
	dm := ws.Group("", m.devMachineDemoGuard)
	dm.GET("/dev-machines", h.devMachine.List, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines", h.devMachine.Create, mw.RequirePermission("dev_machine:create"))
	dm.DELETE("/dev-machines/bulk", h.devMachine.BulkDelete, mw.RequirePermission("dev_machine:admin"))
	dm.POST("/dev-machines/bulk/permanent-delete", h.devMachine.BulkPermanentDelete, mw.RequirePermission("dev_machine:admin"))
	dm.GET("/dev-machine-names/suggestion", h.devMachine.NameSuggestion, mw.RequirePermission("dev_machine:create"))
	dm.GET("/dev-machine-names/availability", h.devMachine.NameAvailability, mw.RequirePermission("dev_machine:create"))
	dm.GET("/dev-machine-policy", h.devMachine.GetPolicy, mw.RequirePermission("dev_machine:read"))
	dm.PATCH("/dev-machine-policy", h.devMachine.UpdatePolicy, mw.RequirePermission("dev_machine:admin"))
	dm.GET("/dev-machine-scope-settings", h.devMachine.ScopeSettings, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machine-scope-setting", h.devMachine.ScopeSetting, mw.RequirePermission("dev_machine:read"))
	dm.PUT("/dev-machine-scope-setting", h.devMachine.UpdateScopeSetting, mw.RequirePermission("dev_machine:manage"))
	dm.DELETE("/dev-machine-scope-setting", h.devMachine.DeleteScopeSetting, mw.RequirePermission("dev_machine:manage"))
	dm.GET("/dev-machine-environments", h.devMachine.Environments, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machine-environments", h.devMachine.SnapshotEnvironment, mw.RequirePermission("dev_machine:admin"))
	dm.GET("/dev-machine-environments/:environmentId", h.devMachine.GetEnvironment, mw.RequirePermission("dev_machine:read"))
	dm.DELETE("/dev-machine-environments/:environmentId", h.devMachine.DeleteEnvironment, mw.RequirePermission("dev_machine:admin"))
	dm.GET("/dev-machine-providers", h.devMachine.Providers, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId", h.devMachine.Get, mw.RequirePermission("dev_machine:read"))
	dm.PATCH("/dev-machines/:machineId", h.devMachine.Update, mw.RequirePermission("dev_machine:manage"))
	dm.DELETE("/dev-machines/:machineId", h.devMachine.Delete, mw.RequirePermission("dev_machine:admin"))
	dm.POST("/dev-machines/:machineId/permanent-delete", h.devMachine.PermanentDelete, mw.RequirePermission("dev_machine:admin"))
	dm.POST("/dev-machines/:machineId/start", h.devMachine.Start, mw.RequirePermission("dev_machine:manage"))
	dm.POST("/dev-machines/:machineId/stop", h.devMachine.Stop, mw.RequirePermission("dev_machine:manage"))
	dm.POST("/dev-machines/:machineId/pause", h.devMachine.Pause, mw.RequirePermission("dev_machine:manage"))
	dm.POST("/dev-machines/:machineId/teardown", h.devMachine.Teardown, mw.RequirePermission("dev_machine:manage"))
	dm.POST("/dev-machines/:machineId/activity", h.devMachine.TouchActivity, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/checkouts", h.devMachine.Checkouts, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines/:machineId/checkouts", h.devMachine.CheckoutIssue, mw.RequirePermission("dev_machine:manage"))
	dm.GET("/dev-machines/:machineId/events", h.devMachine.Events, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/logs", h.devMachine.Logs, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/services", h.devMachine.Services, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/providers", h.devMachine.MachineProviders, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/resource-usage", h.devMachine.ResourceUsage, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines/:machineId/services/:service/launch", h.devMachine.LaunchService, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/terminal-sessions", h.devMachine.ListTerminalSessions, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines/:machineId/terminal-sessions", h.devMachine.CreateTerminalSession, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines/:machineId/terminal-sessions/:sessionId/close", h.devMachine.CloseTerminalSession, mw.RequirePermission("dev_machine:read"))
	dm.GET("/dev-machines/:machineId/agent-runs", h.devMachine.ListMachineAgentRuns, mw.RequirePermission("dev_machine:read"))
	dm.POST("/dev-machines/:machineId/agent-runs", h.devMachine.CreateAgentRun, mw.RequirePermission("dev_machine:manage"))
	dm.GET("/agent-runs", h.devMachine.ListAgentRuns, mw.RequirePermission("dev_machine:read"))
	dm.GET("/agent-runs/:agentRunId", h.devMachine.GetAgentRun, mw.RequirePermission("dev_machine:read"))
	dm.POST("/agent-runs/:agentRunId/cancel", h.devMachine.CancelAgentRun, mw.RequirePermission("dev_machine:manage"))
	dm.GET("/agent-runs/:agentRunId/trace", h.devMachine.AgentRunTrace, mw.RequirePermission("dev_machine:read"))

	// Favorites
	ws.GET("/favorites", h.fav.List, mw.RequirePermission("account:read"))
	ws.POST("/favorites", h.fav.Create, mw.RequireSession())
	ws.DELETE("/favorites/:id", h.fav.Delete, mw.RequireSession())

	// Shared Links
	ws.GET("/shared-links", h.sharedLink.List, mw.RequirePermission("account:read"))
	ws.POST("/shared-links", h.sharedLink.Create, mw.RequireSession())
	ws.PATCH("/shared-links/:id", h.sharedLink.Update, mw.RequireSession())
	ws.DELETE("/shared-links/:id", h.sharedLink.Delete, mw.RequireSession())

	// Uploads
	ws.POST("/upload", h.upload.Upload, mw.RequirePermission("issue:create"))
	ws.GET("/assets/:assetId", h.upload.GetAsset, mw.RequireSession())
	ws.POST("/issues/:identifier/prompt-assets", h.upload.SignIssuePromptAssets, mw.RequireSession())

	// WebSocket
	ws.GET("/ws", h.ws.Handle, mw.RequireSession())

	// Notifications (user-scoped, not workspace-scoped)
	api.GET("/notifications", h.notif.List, mw.RequirePermission("notifications:read"))
	api.PATCH("/notifications/:id", h.notif.Update, mw.RequireSession())
	api.POST("/notifications/:id/read", h.notif.MarkRead, mw.RequireSession())
	api.POST("/notifications/:id/unread", h.notif.MarkUnread, mw.RequireSession())
	api.POST("/notifications/:id/snooze", h.notif.Snooze, mw.RequireSession())
	api.POST("/notifications/:id/unsnooze", h.notif.Unsnooze, mw.RequireSession())
	api.POST("/notifications/:id/archive", h.notif.Archive, mw.RequireSession())
	api.POST("/notifications/:id/unarchive", h.notif.Unarchive, mw.RequireSession())
	api.POST("/notifications/mark-all-read", h.notif.MarkAllRead, mw.RequireSession())
}
