package main

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v4"
)

// Route registration helpers make the permission annotation and its
// enforcement structurally inseparable: the same call attaches the
// middleware and records the annotation as the route's Name, which the
// manifest test in routes_test.go audits.
//
//	scoped      PAT needs perm in scopes, then RBAC decides (scope ∩ RBAC)
//	sessionOnly interactive sessions (JWT) only; PATs denied by default
//	ownerOnly   workspace owner, interactive sessions only
func scoped(g *echo.Group, method, path string, h echo.HandlerFunc, perm string, extra ...echo.MiddlewareFunc) {
	middleware := append([]echo.MiddlewareFunc{mw.RequirePermission(perm)}, extra...)
	g.Add(method, path, h, middleware...).Name = "perm:" + perm
}

func sessionOnly(g *echo.Group, method, path string, h echo.HandlerFunc, extra ...echo.MiddlewareFunc) {
	middlewares := append([]echo.MiddlewareFunc{mw.RequireSession()}, extra...)
	g.Add(method, path, h, middlewares...).Name = "session"
}

func ownerOnly(g *echo.Group, method, path string, h echo.HandlerFunc) {
	g.Add(method, path, h, mw.RequireOwner()).Name = "owner"
}

// appHandlers groups every HTTP handler so route registration can run
// without a database (tests enumerate e.Routes() with zero-value handlers).
type appHandlers struct {
	config            *handler.ConfigHandler
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
	agentTask         *handler.AgentTaskHandler
	agentAutomation   *handler.AgentAutomationHandler
	agentMessage      *handler.AgentMessageHandler
	conversation      *handler.ConversationHandler
	telegram          *handler.TelegramHandler
	voice             *handler.VoiceHandler
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
	e.GET("/health", h.health.Health).Name = "public"
	e.GET("/ready", h.health.Ready).Name = "public"

	// Auth (public) — rate limited: 5 requests/sec, burst of 10
	auth := e.Group("/api/auth", m.authRateLimit)
	auth.POST("/register", h.auth.Register).Name = "public"
	auth.POST("/login", h.auth.Login).Name = "public"
	auth.POST("/refresh", h.auth.Refresh).Name = "public"
	auth.POST("/logout", h.auth.Logout).Name = "public"

	// Public share routes (no auth, rate limited)
	pub := e.Group("/api/public", m.publicRateLimit)
	pub.GET("/share/:token", h.sharedLink.GetPublicMeta).Name = "public"
	pub.GET("/share/:token/issues", h.sharedLink.ListPublicIssues).Name = "public"
	e.GET("/api/public/assets/:token", h.upload.PublicAsset, m.publicAssetRateLimit).Name = "public"
	pub.GET("/system/update-activity", h.system.UpdateActivity).Name = "public"

	e.GET("/api/config", h.config.Get, m.publicAssetRateLimit).Name = "public"
	e.GET("/api/invite/:token", h.inviteLink.Preview, m.authRateLimit).Name = "public"

	// Authenticated routes
	api := e.Group("/api", m.auth)

	// User
	scoped(api, http.MethodGet, "/auth/me", h.auth.Me, "account:read")
	sessionOnly(api, http.MethodPatch, "/auth/me", h.auth.UpdateProfile)
	scoped(api, http.MethodGet, "/preferences", h.prefs.Get, "account:read", mw.RequireUnrestrictedTokenWorkspaces())
	sessionOnly(api, http.MethodPatch, "/preferences", h.prefs.Update)

	// Token management requires a session; handlers also guard direct calls.
	sessionOnly(api, http.MethodGet, "/tokens", h.token.List)
	sessionOnly(api, http.MethodPost, "/tokens", h.token.Create)
	sessionOnly(api, http.MethodDelete, "/tokens/:id", h.token.Revoke)
	sessionOnly(api, http.MethodGet, "/system/update-status", h.system.UpdateStatus)
	sessionOnly(api, http.MethodPost, "/system/update", h.system.StartUpdate)

	// Workspaces (no workspace context needed for list/create)
	scoped(api, http.MethodGet, "/workspaces", h.workspace.List, "workspaces:read")
	sessionOnly(api, http.MethodPost, "/workspaces", h.workspace.Create)

	sessionOnly(api, http.MethodPost, "/workspaces/import/preview", h.workspaceTransfer.Preview)
	sessionOnly(api, http.MethodPost, "/workspaces/import", h.workspaceTransfer.Import)
	sessionOnly(api, http.MethodPost, "/invite/:token/accept", h.inviteLink.Accept)

	// Workspace-scoped routes
	ws := api.Group("/workspaces/:slug", m.workspaceMembership)
	scoped(ws, http.MethodGet, "", h.workspace.Get, "workspaces:read")
	ownerOnly(ws, http.MethodPatch, "", h.workspace.Update)
	ownerOnly(ws, http.MethodDelete, "", h.workspace.Delete)
	scoped(ws, http.MethodGet, "/export", h.workspaceTransfer.Export, "workspace:transfer")
	scoped(ws, http.MethodPost, "/invite-links", h.inviteLink.Create, "member:invite")
	scoped(ws, http.MethodGet, "/invite-links", h.inviteLink.List, "member:invite")
	scoped(ws, http.MethodDelete, "/invite-links/:id", h.inviteLink.Revoke, "member:invite")
	scoped(ws, http.MethodPost, "/invite", h.workspace.Invite, "member:invite")
	scoped(ws, http.MethodGet, "/members", h.workspace.ListMembers, "members:read")
	scoped(ws, http.MethodPatch, "/members/:userId", h.workspace.UpdateMemberRole, "member:invite")
	scoped(ws, http.MethodDelete, "/members/:userId", h.workspace.RemoveMember, "member:invite")

	// Teams
	scoped(ws, http.MethodGet, "/teams", h.team.List, "teams:read")
	scoped(ws, http.MethodPost, "/teams", h.team.Create, "team:manage")
	scoped(ws, http.MethodGet, "/teams/:teamId", h.team.Get, "teams:read")
	scoped(ws, http.MethodPatch, "/teams/:teamId", h.team.Update, "team:manage")
	scoped(ws, http.MethodDelete, "/teams/:teamId", h.team.Delete, "team:manage")
	scoped(ws, http.MethodPatch, "/teams/:teamId/visibility", h.team.SetVisibility, "team:manage")
	scoped(ws, http.MethodGet, "/teams/:teamId/members", h.team.Members, "members:read", mw.RequirePermission("teams:read"))
	scoped(ws, http.MethodPost, "/teams/:teamId/members", h.team.AddMember, "team:manage")
	scoped(ws, http.MethodDelete, "/teams/:teamId/members/:userId", h.team.RemoveMember, "team:manage")
	sessionOnly(ws, http.MethodPost, "/teams/:teamId/leave", h.team.Leave)

	// Team Statuses
	scoped(ws, http.MethodGet, "/teams/:teamId/statuses", h.teamStatus.List, "teams:read")
	scoped(ws, http.MethodPost, "/teams/:teamId/statuses", h.teamStatus.Create, "team:manage")
	scoped(ws, http.MethodPatch, "/teams/:teamId/statuses/:statusId", h.teamStatus.Update, "team:manage")
	scoped(ws, http.MethodDelete, "/teams/:teamId/statuses/:statusId", h.teamStatus.Delete, "team:manage")

	// Cycles (team-scoped)
	scoped(ws, http.MethodGet, "/teams/:teamId/cycles", h.cycle.List, "cycles:read")
	sessionOnly(ws, http.MethodPost, "/teams/:teamId/cycles", h.cycle.Create, mw.RequirePermission("cycle:manage"))
	scoped(ws, http.MethodGet, "/teams/:teamId/cycles/velocity", h.cycle.Velocity, "cycles:read")
	scoped(ws, http.MethodGet, "/teams/:teamId/cycles/:cycleId", h.cycle.Get, "cycles:read")
	sessionOnly(ws, http.MethodPatch, "/teams/:teamId/cycles/:cycleId", h.cycle.Update, mw.RequirePermission("cycle:manage"))
	sessionOnly(ws, http.MethodPost, "/teams/:teamId/cycles/:cycleId/complete", h.cycle.Complete, mw.RequirePermission("cycle:manage"))
	scoped(ws, http.MethodGet, "/teams/:teamId/cycles/:cycleId/burndown", h.cycle.Burndown, "cycles:read")
	sessionOnly(ws, http.MethodDelete, "/teams/:teamId/cycles/:cycleId", h.cycle.Delete, mw.RequirePermission("cycle:manage"))

	// Issues
	scoped(ws, http.MethodGet, "/issues", h.issue.List, "issues:read")
	scoped(ws, http.MethodPost, "/issues", h.issue.Create, "issue:create")
	scoped(ws, http.MethodPatch, "/issues/bulk", h.issue.BulkUpdate, "issue:update")
	scoped(ws, http.MethodDelete, "/issues/bulk", h.issue.BulkDelete, "issue:delete_own")
	scoped(ws, http.MethodGet, "/issues/:identifier", h.issue.Get, "issues:read")
	scoped(ws, http.MethodPatch, "/issues/:identifier", h.issue.Update, "issue:update")
	scoped(ws, http.MethodDelete, "/issues/:identifier", h.issue.Delete, "issue:delete_own")
	sessionOnly(ws, http.MethodPost, "/issues/:identifier/subscribe", h.issue.Subscribe)
	sessionOnly(ws, http.MethodDelete, "/issues/:identifier/subscribe", h.issue.Unsubscribe)
	scoped(ws, http.MethodPost, "/issues/:identifier/duplicate", h.issue.Duplicate, "issue:create")
	scoped(ws, http.MethodPost, "/issues/:identifier/convert-to-project", h.issue.ConvertToProject, "project:manage")
	scoped(ws, http.MethodPost, "/issues/:identifier/expand-description", h.aiSettings.ExpandIssueDescription, "issue:update")
	scoped(ws, http.MethodGet, "/issues/:identifier/comments", h.issue.ListComments, "comments:read")
	scoped(ws, http.MethodPost, "/issues/:identifier/comments", h.issue.CreateComment, "issue:create")
	scoped(ws, http.MethodPost, "/issues/:identifier/comments/:commentId/resolve", h.issue.ResolveComment, "issue:update")
	scoped(ws, http.MethodPost, "/issues/:identifier/comments/:commentId/reopen", h.issue.ReopenComment, "issue:update")
	scoped(ws, http.MethodGet, "/issues/:identifier/sub-issues", h.issue.ListSubIssues, "issues:read")
	scoped(ws, http.MethodPost, "/issues/:identifier/sub-issues", h.issue.CreateSubIssue, "issue:create")
	scoped(ws, http.MethodPost, "/issues/:identifier/sub-issues/bulk", h.issue.BulkCreateSubIssues, "issue:create")
	scoped(ws, http.MethodGet, "/issues/:identifier/history", h.issue.GetHistory, "issues:read")
	scoped(ws, http.MethodPost, "/issues/:identifier/triage/accept", h.issue.TriageAccept, "issue:update")
	scoped(ws, http.MethodPost, "/issues/:identifier/triage/decline", h.issue.TriageDecline, "issue:update")

	// Issue Relations
	scoped(ws, http.MethodPost, "/issues/:identifier/relations", h.relation.Create, "issue:update")
	scoped(ws, http.MethodGet, "/issues/:identifier/relations", h.relation.List, "issues:read")
	scoped(ws, http.MethodDelete, "/issues/:identifier/relations/:relationId", h.relation.Delete, "issue:update")

	// Issue Templates
	scoped(ws, http.MethodGet, "/issue-templates", h.template.List, "templates:read")
	scoped(ws, http.MethodPost, "/issue-templates", h.template.Create, "issue:create")
	scoped(ws, http.MethodGet, "/issue-templates/:id", h.template.Get, "templates:read")
	scoped(ws, http.MethodPatch, "/issue-templates/:id", h.template.Update, "issue:create")
	scoped(ws, http.MethodDelete, "/issue-templates/:id", h.template.Delete, "issue:create")

	// Labels
	scoped(ws, http.MethodGet, "/labels", h.label.List, "labels:read")
	scoped(ws, http.MethodPost, "/labels", h.label.Create, "label:manage")
	scoped(ws, http.MethodPatch, "/labels/:id", h.label.Update, "label:manage")
	scoped(ws, http.MethodDelete, "/labels/:id", h.label.Delete, "label:manage")

	// Projects
	scoped(ws, http.MethodGet, "/projects", h.project.List, "projects:read")
	scoped(ws, http.MethodPost, "/projects", h.project.Create, "project:manage")
	scoped(ws, http.MethodGet, "/projects/:id", h.project.Get, "projects:read")
	scoped(ws, http.MethodPatch, "/projects/:id", h.project.Update, "project:manage")
	scoped(ws, http.MethodDelete, "/projects/:id", h.project.Delete, "project:manage")
	scoped(ws, http.MethodGet, "/teams/:teamId/projects", h.project.ListByTeam, "projects:read")

	// Views
	scoped(ws, http.MethodGet, "/views", h.view.List, "views:read")
	sessionOnly(ws, http.MethodPost, "/views", h.view.Create)
	scoped(ws, http.MethodGet, "/views/:id", h.view.Get, "views:read")
	sessionOnly(ws, http.MethodPatch, "/views/:id", h.view.Update)
	sessionOnly(ws, http.MethodDelete, "/views/:id", h.view.Delete)

	// Analytics
	scoped(ws, http.MethodGet, "/analytics/overview", h.analytics.Overview, "analytics:read")
	scoped(ws, http.MethodGet, "/analytics/distribution", h.analytics.IssueDistribution, "analytics:read")
	scoped(ws, http.MethodGet, "/analytics/insights", h.analytics.Insights, "analytics:read")
	scoped(ws, http.MethodGet, "/analytics/burnup", h.analytics.Burnup, "analytics:read")

	// Webhooks
	scoped(ws, http.MethodGet, "/webhooks", h.webhook.List, "workspace:manage")
	scoped(ws, http.MethodPost, "/webhooks", h.webhook.Create, "workspace:manage")
	scoped(ws, http.MethodPatch, "/webhooks/:id", h.webhook.Update, "workspace:manage")
	scoped(ws, http.MethodDelete, "/webhooks/:id", h.webhook.Delete, "workspace:manage")

	// AI settings
	ownerOnly(ws, http.MethodGet, "/ai-settings", h.aiSettings.Get)
	scoped(ws, http.MethodGet, "/ai-settings/issue-copy-prompt", h.aiSettings.GetIssueCopyPrompt, "issues:read")
	ownerOnly(ws, http.MethodPatch, "/ai-settings", h.aiSettings.Update)
	ownerOnly(ws, http.MethodPost, "/ai-settings/vision-check", h.aiSettings.CheckVision)

	// Conversations are session-only until dedicated PAT scopes are defined.
	sessionOnly(ws, http.MethodGet, "/assistant/status", h.conversation.Status)
	// Members read the assistant's tools; the service lets owners and admins switch them.
	sessionOnly(ws, http.MethodGet, "/assistant-capabilities", h.conversation.Capabilities)
	sessionOnly(ws, http.MethodPut, "/assistant-capabilities/:name", h.conversation.SetCapability)
	sessionOnly(ws, http.MethodGet, "/conversations", h.conversation.List)
	sessionOnly(ws, http.MethodPost, "/conversations", h.conversation.Create)
	sessionOnly(ws, http.MethodGet, "/conversations/:conversationId", h.conversation.History)
	sessionOnly(ws, http.MethodPatch, "/conversations/:conversationId", h.conversation.Update)
	sessionOnly(ws, http.MethodDelete, "/conversations/:conversationId", h.conversation.Delete)
	sessionOnly(ws, http.MethodGet, "/conversation-folders", h.conversation.Folders)
	sessionOnly(ws, http.MethodPost, "/conversation-folders", h.conversation.CreateFolder)
	sessionOnly(ws, http.MethodPatch, "/conversation-folders/:folderId", h.conversation.RenameFolder)
	sessionOnly(ws, http.MethodDelete, "/conversation-folders/:folderId", h.conversation.DeleteFolder)
	sessionOnly(ws, http.MethodGet, "/conversations/:conversationId/events", h.conversation.Events)
	sessionOnly(ws, http.MethodGet, "/conversations/:conversationId/stream", h.conversation.Stream)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/messages", h.conversation.Send)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/attachments", h.conversation.UploadAttachment)
	sessionOnly(ws, http.MethodGet, "/conversations/:conversationId/attachments/:attachmentId", h.conversation.Attachment)
	sessionOnly(ws, http.MethodDelete, "/conversations/:conversationId/attachments/:attachmentId", h.conversation.RemoveAttachment)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/messages/:messageId/retry", h.conversation.Retry)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/cancel", h.conversation.Cancel)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/approvals/:approvalId", h.conversation.ResolveApproval)

	// Voice: members read whether the microphone is available; the owner
	// chooses the model; a conversation's reader transcribes into its composer.
	sessionOnly(ws, http.MethodGet, "/voice", h.voice.Status)
	ownerOnly(ws, http.MethodPut, "/voice", h.voice.Select)
	sessionOnly(ws, http.MethodPost, "/conversations/:conversationId/transcriptions", h.voice.Transcribe)

	if h.telegram != nil {
		sessionOnly(ws, http.MethodGet, "/telegram", h.telegram.Status)
		sessionOnly(ws, http.MethodPatch, "/telegram", h.telegram.Save)
		sessionOnly(ws, http.MethodPost, "/telegram/link", h.telegram.Link)
		sessionOnly(ws, http.MethodDelete, "/telegram/link", h.telegram.Unlink)
		e.POST("/api/telegram/webhook/:workspaceId", h.telegram.Webhook, m.publicRateLimit).Name = "public"
	}

	// GitHub integration (conditional)
	// Public webhook endpoint (no auth, signature-verified internally)
	e.POST("/api/github/webhook", h.github.HandleWebhook).Name = "public"
	e.POST("/api/dev-machine-ingest/events", h.devMachine.IngestEvent, m.machineEventsRateLimit).Name = "public"
	e.POST("/api/dev-machine-ingest/logs", h.devMachine.IngestLog, m.machineLogsRateLimit).Name = "public"

	// GitHub integration (workspace-scoped)
	sessionOnly(ws, http.MethodGet, "/github/status", h.github.Status)
	scoped(ws, http.MethodGet, "/github/setup", h.github.Setup, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/setup/callback", h.github.SetupCallback, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/install", h.github.InstallURL, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/callback", h.github.Callback, "workspace:manage")
	scoped(ws, http.MethodDelete, "/github/disconnect", h.github.Disconnect, "workspace:manage")
	scoped(ws, http.MethodDelete, "/github/app", h.github.DeleteApp, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/repos", h.github.ListRepos, "workspace:manage")
	scoped(ws, http.MethodPost, "/github/repos", h.github.LinkRepos, "workspace:manage")
	scoped(ws, http.MethodDelete, "/github/repos/:id", h.github.UnlinkRepo, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/auto-transitions", h.github.ListAutoTransitions, "workspace:manage")
	scoped(ws, http.MethodPatch, "/github/auto-transitions", h.github.UpdateAutoTransitions, "workspace:manage")
	scoped(ws, http.MethodGet, "/issues/:identifier/github", h.github.IssueGitHubActivity, "issues:read")
	scoped(ws, http.MethodGet, "/github/issue-links", h.github.AgentIssueLinks, "issues:read")

	// Dev Machines — guarded by demo-mode restriction when active
	dm := ws.Group("", m.devMachineDemoGuard)
	sessionOnly(dm, http.MethodGet, "/conversations/:conversationId/task-options", h.agentTask.Options)
	sessionOnly(dm, http.MethodGet, "/conversations/:conversationId/tasks", h.agentTask.List)
	sessionOnly(dm, http.MethodPost, "/conversations/:conversationId/tasks", h.agentTask.Create)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId", h.agentTask.Get)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/history", h.agentTask.History)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs", h.agentTask.Runs)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/logs", h.agentTask.Logs)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/validation", h.agentTask.Validation)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/checks", h.agentTask.ManualChecks)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/diff", h.agentTask.Diff)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/artifacts", h.agentTask.Artifacts)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/artifacts/:artifactId", h.agentTask.Artifact)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/publication", h.agentTask.Publication)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/signals", h.agentTask.Signals)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/sessions", h.agentTask.Sessions)
	sessionOnly(dm, http.MethodGet, "/agent-tasks", h.agentTask.Queue)
	sessionOnly(dm, http.MethodGet, "/dev-machines/:machineId/tasks", h.agentTask.MachineTasks)
	if h.agentAutomation != nil {
		sessionOnly(dm, http.MethodGet, "/agent-automations", h.agentAutomation.List)
		sessionOnly(dm, http.MethodPost, "/agent-automations", h.agentAutomation.Create)
		sessionOnly(dm, http.MethodPatch, "/agent-automations/:automationId", h.agentAutomation.Update)
		sessionOnly(dm, http.MethodDelete, "/agent-automations/:automationId", h.agentAutomation.Delete)
		sessionOnly(dm, http.MethodGet, "/agent-automation-runs", h.agentAutomation.Runs)
		sessionOnly(dm, http.MethodPost, "/agent-automation-runs/:runId/approve", h.agentAutomation.Approve)
		sessionOnly(dm, http.MethodPost, "/agent-automation-runs/:runId/dismiss", h.agentAutomation.Dismiss)
	}
	if h.agentMessage != nil {
		sessionOnly(dm, http.MethodGet, "/agent-messages", h.agentMessage.List)
		sessionOnly(dm, http.MethodGet, "/agent-messages/:messageId", h.agentMessage.Get)
		sessionOnly(dm, http.MethodPost, "/agent-messages/:messageId/hold", h.agentMessage.Hold())
		sessionOnly(dm, http.MethodPost, "/agent-messages/:messageId/release", h.agentMessage.Release())
		sessionOnly(dm, http.MethodPost, "/agent-messages/:messageId/drop", h.agentMessage.Drop())
		sessionOnly(dm, http.MethodGet, "/agent-message-holds", h.agentMessage.Holds)
		sessionOnly(dm, http.MethodPost, "/agent-message-holds", h.agentMessage.HoldConversation)
		sessionOnly(dm, http.MethodDelete, "/agent-message-holds/:holdId", h.agentMessage.ReleaseConversation)
	}
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/cancel", h.agentTask.Cancel)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/retry", h.agentTask.Retry)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/accept", h.agentTask.Accept)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/sign-in", h.agentTask.ContinueAfterSignIn)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/preview", h.agentTask.StartPreview)
	sessionOnly(dm, http.MethodDelete, "/agent-tasks/:taskId/preview", h.agentTask.StopPreview)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/checks", h.agentTask.RunCheck)
	sessionOnly(dm, http.MethodGet, "/agent-sign-ins", h.devMachine.ListAgentSignIns)
	sessionOnly(dm, http.MethodPut, "/agent-sign-ins/:provider/token", h.devMachine.SaveAgentSignInToken)
	sessionOnly(dm, http.MethodDelete, "/agent-sign-ins/:provider", h.devMachine.DeleteAgentSignIn)
	scoped(dm, http.MethodGet, "/dev-machines", h.devMachine.List, "dev_machine:read")
	scoped(dm, http.MethodPost, "/dev-machines", h.devMachine.Create, "dev_machine:create")
	scoped(dm, http.MethodDelete, "/dev-machines/bulk", h.devMachine.BulkDelete, "dev_machine:admin")
	scoped(dm, http.MethodPost, "/dev-machines/bulk/permanent-delete", h.devMachine.BulkPermanentDelete, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/dev-machine-names/suggestion", h.devMachine.NameSuggestion, "dev_machine:create")
	scoped(dm, http.MethodGet, "/dev-machine-names/availability", h.devMachine.NameAvailability, "dev_machine:create")
	scoped(dm, http.MethodGet, "/dev-machine-policy", h.devMachine.GetPolicy, "dev_machine:read")
	scoped(dm, http.MethodPatch, "/dev-machine-policy", h.devMachine.UpdatePolicy, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/dev-machine-scope-settings", h.devMachine.ScopeSettings, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machine-scope-setting", h.devMachine.ScopeSetting, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machine-repository-defaults", h.devMachine.RepositoryDefaults, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machine-check-suggestion", h.devMachine.CheckSuggestion, "dev_machine:manage")
	scoped(dm, http.MethodPut, "/dev-machine-scope-setting", h.devMachine.UpdateScopeSetting, "dev_machine:manage")
	scoped(dm, http.MethodDelete, "/dev-machine-scope-setting", h.devMachine.DeleteScopeSetting, "dev_machine:manage")
	scoped(dm, http.MethodGet, "/dev-machine-environments", h.devMachine.Environments, "dev_machine:read")
	scoped(dm, http.MethodPost, "/dev-machine-environments", h.devMachine.SnapshotEnvironment, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/dev-machine-environments/:environmentId", h.devMachine.GetEnvironment, "dev_machine:read")
	scoped(dm, http.MethodDelete, "/dev-machine-environments/:environmentId", h.devMachine.DeleteEnvironment, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/dev-machine-providers", h.devMachine.Providers, "dev_machine:read")
	scoped(dm, http.MethodGet, "/agent-model-servers", h.devMachine.ListModelServers, "dev_machine:read")
	scoped(dm, http.MethodPost, "/agent-model-servers", h.devMachine.CreateModelServer, "dev_machine:admin")
	scoped(dm, http.MethodPost, "/agent-model-servers/discover", h.devMachine.DiscoverModels, "dev_machine:admin")
	scoped(dm, http.MethodPut, "/agent-model-servers/:serverId", h.devMachine.UpdateModelServer, "dev_machine:admin")
	scoped(dm, http.MethodDelete, "/agent-model-servers/:serverId", h.devMachine.DeleteModelServer, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId", h.devMachine.Get, "dev_machine:read")
	scoped(dm, http.MethodPatch, "/dev-machines/:machineId", h.devMachine.Update, "dev_machine:manage")
	scoped(dm, http.MethodDelete, "/dev-machines/:machineId", h.devMachine.Delete, "dev_machine:admin")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/permanent-delete", h.devMachine.PermanentDelete, "dev_machine:admin")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/start", h.devMachine.Start, "dev_machine:manage")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/stop", h.devMachine.Stop, "dev_machine:manage")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/pause", h.devMachine.Pause, "dev_machine:manage")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/teardown", h.devMachine.Teardown, "dev_machine:manage")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/activity", h.devMachine.TouchActivity, "dev_machine:read", mw.RequireTokenPermission("dev_machine:manage"))
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/checkouts", h.devMachine.Checkouts, "dev_machine:read")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/checkouts", h.devMachine.CheckoutIssue, "dev_machine:manage")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/events", h.devMachine.Events, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/logs", h.devMachine.Logs, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/services", h.devMachine.Services, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/providers", h.devMachine.MachineProviders, "dev_machine:read")
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/resource-usage", h.devMachine.ResourceUsage, "dev_machine:read")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/services/:service/launch", h.devMachine.LaunchService, "dev_machine:read", mw.RequireTokenPermission("dev_machine:manage"))
	sessionOnly(dm, http.MethodGet, "/dev-machines/:machineId/ports", h.devMachine.ListPorts)
	sessionOnly(dm, http.MethodPost, "/dev-machines/:machineId/ports", h.devMachine.CreatePort)
	sessionOnly(dm, http.MethodPatch, "/dev-machines/:machineId/ports/:portId", h.devMachine.UpdatePort)
	sessionOnly(dm, http.MethodDelete, "/dev-machines/:machineId/ports/:portId", h.devMachine.DeletePort)
	sessionOnly(dm, http.MethodPost, "/dev-machines/:machineId/ports/:portId/launch", h.devMachine.LaunchPort)
	scoped(dm, http.MethodGet, "/dev-machines/:machineId/terminal-sessions", h.devMachine.ListTerminalSessions, "dev_machine:read")
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/terminal-sessions", h.devMachine.CreateTerminalSession, "dev_machine:read", mw.RequireTokenPermission("dev_machine:manage"))
	scoped(dm, http.MethodPost, "/dev-machines/:machineId/terminal-sessions/:sessionId/close", h.devMachine.CloseTerminalSession, "dev_machine:read", mw.RequireTokenPermission("dev_machine:manage"))

	// Favorites
	scoped(ws, http.MethodGet, "/favorites", h.fav.List, "account:read")
	sessionOnly(ws, http.MethodPost, "/favorites", h.fav.Create)
	sessionOnly(ws, http.MethodDelete, "/favorites/:id", h.fav.Delete)

	// Shared Links
	sessionOnly(ws, http.MethodGet, "/shared-links", h.sharedLink.List)
	sessionOnly(ws, http.MethodPost, "/shared-links", h.sharedLink.Create)
	sessionOnly(ws, http.MethodPatch, "/shared-links/:id", h.sharedLink.Update)
	sessionOnly(ws, http.MethodDelete, "/shared-links/:id", h.sharedLink.Delete)

	// Uploads
	scoped(ws, http.MethodPost, "/upload", h.upload.Upload, "issue:create")
	scoped(ws, http.MethodGet, "/assets/:assetId", h.upload.GetAsset, "assets:read")
	sessionOnly(ws, http.MethodPost, "/issues/:identifier/prompt-assets", h.upload.SignIssuePromptAssets)

	// WebSocket
	sessionOnly(ws, http.MethodGet, "/ws", h.ws.Handle)

	// Notifications (user-scoped, not workspace-scoped)
	scoped(api, http.MethodGet, "/notifications", h.notif.List, "notifications:read", mw.RequireUnrestrictedTokenWorkspaces())
	sessionOnly(api, http.MethodPatch, "/notifications/:id", h.notif.Update)
	sessionOnly(api, http.MethodPost, "/notifications/:id/read", h.notif.MarkRead)
	sessionOnly(api, http.MethodPost, "/notifications/:id/unread", h.notif.MarkUnread)
	sessionOnly(api, http.MethodPost, "/notifications/:id/snooze", h.notif.Snooze)
	sessionOnly(api, http.MethodPost, "/notifications/:id/unsnooze", h.notif.Unsnooze)
	sessionOnly(api, http.MethodPost, "/notifications/:id/archive", h.notif.Archive)
	sessionOnly(api, http.MethodPost, "/notifications/:id/unarchive", h.notif.Unarchive)
	sessionOnly(api, http.MethodPost, "/notifications/mark-all-read", h.notif.MarkAllRead)
}
