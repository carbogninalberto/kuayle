package main

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	"github.com/kuayle/kuayle-backend/internal/mcp"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
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
	addNamedRoute(g, method, path, h, "perm:"+perm, middleware...)
}

func sessionOnly(g *echo.Group, method, path string, h echo.HandlerFunc, extra ...echo.MiddlewareFunc) {
	middlewares := append([]echo.MiddlewareFunc{mw.RequireSession()}, extra...)
	addNamedRoute(g, method, path, h, "session", middlewares...)
}

func ownerOnly(g *echo.Group, method, path string, h echo.HandlerFunc) {
	addNamedRoute(g, method, path, h, "owner", mw.RequireOwner())
}

// public registers a route that deliberately needs no authentication.
func public(r routeAdder, method, path string, h echo.HandlerFunc, extra ...echo.MiddlewareFunc) {
	addNamedRoute(r, method, path, h, "public", extra...)
}

// routeAdder is implemented by both *echo.Echo and *echo.Group.
type routeAdder interface {
	AddRoute(route echo.Route) (echo.RouteInfo, error)
}

// addNamedRoute records the permission annotation as the route Name. Like
// echo.Add it panics on an invalid or duplicate registration.
func addNamedRoute(r routeAdder, method, path string, h echo.HandlerFunc, name string, middlewares ...echo.MiddlewareFunc) {
	if _, err := r.AddRoute(echo.Route{Method: method, Path: path, Name: name, Handler: h, Middlewares: middlewares}); err != nil {
		panic(err)
	}
}

// appHandlers groups every HTTP handler so route registration can run
// without a database (tests enumerate e.Routes() with zero-value handlers).
type appHandlers struct {
	config            *handler.ConfigHandler
	inviteLink        *handler.InviteLinkHandler
	workspaceTransfer *handler.WorkspaceTransferHandler
	health            *handler.HealthHandler
	auth              *handler.AuthHandler
	passkey           *handler.PasskeyHandler
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
	workflow          *handler.WorkflowHandler
	agentAutomation   *handler.AgentAutomationHandler
	agentAssignee     *handler.AgentAssigneeHandler
	agentMessage      *handler.AgentMessageHandler
	conversation      *handler.ConversationHandler
	assistantSkill    *handler.AssistantSkillHandler
	assistantTool     *handler.AssistantCustomToolHandler
	assistantResearch *handler.AssistantResearchHandler
	telegram          *handler.TelegramHandler
	voice             *handler.VoiceHandler
	devMachine        *handler.DevMachineHandler
	mergeMethods      *handler.MergeMethodHandler
	analytics         *handler.AnalyticsHandler
	system            *handler.SystemHandler
	webhook           *handler.WebhookHandler
	sharedLink        *handler.SharedLinkHandler
	upload            *handler.UploadHandler
	github            *handler.GitHubHandler
	githubBot         *handler.GitHubBotHandler
	wiki              *handler.WikiHandler
	wikiPublish       *handler.WikiPublishHandler
	openapi           *handler.OpenAPIHandler
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
	mcpRateLimit           echo.MiddlewareFunc
}

func registerRoutes(e *echo.Echo, h *appHandlers, m *appMiddleware) {
	// Health
	public(e, http.MethodGet, "/health", h.health.Health)
	public(e, http.MethodGet, "/ready", h.health.Ready)

	// Auth (public) — rate limited: 5 requests/sec, burst of 10
	auth := e.Group("/api/auth", m.authRateLimit)
	public(auth, http.MethodPost, "/register", h.auth.Register)
	public(auth, http.MethodPost, "/login", h.auth.Login)
	public(auth, http.MethodPost, "/refresh", h.auth.Refresh)
	public(auth, http.MethodPost, "/logout", h.auth.Logout)
	public(auth, http.MethodPost, "/forgot-password", h.auth.ForgotPassword)
	public(auth, http.MethodPost, "/reset-password", h.auth.ResetPassword)
	// Passkey sign-in is anonymous: the assertion is the credential.
	public(auth, http.MethodPost, "/passkey/login/begin", h.passkey.BeginLogin)
	public(auth, http.MethodPost, "/passkey/login/complete", h.passkey.FinishLogin)

	// Public share routes (no auth, rate limited)
	pub := e.Group("/api/public", m.publicRateLimit)
	public(pub, http.MethodGet, "/share/:token", h.sharedLink.GetPublicMeta)
	public(pub, http.MethodGet, "/share/:token/issues", h.sharedLink.ListPublicIssues)
	public(pub, http.MethodGet, "/wiki/:token", h.wikiPublish.Public)
	public(e, http.MethodGet, "/api/public/assets/:token", h.upload.PublicAsset, m.publicAssetRateLimit)
	public(pub, http.MethodGet, "/system/update-activity", h.system.UpdateActivity)

	public(e, http.MethodGet, "/api/config", h.config.Get, m.publicAssetRateLimit)
	public(e, http.MethodGet, "/api/invite/:token", h.inviteLink.Preview, m.authRateLimit)
	// The public API reference (internal/openapi); a test keeps it equal to the
	// token-accessible routes below.
	public(e, http.MethodGet, "/api/openapi.json", h.openapi.Spec, m.publicAssetRateLimit)

	// Model Context Protocol server for external agents: personal access tokens
	// only. Each tool call re-enters the scoped routes below through e, so it
	// passes their permission checks; other methods get 405, as the transport
	// allows for a server without a stream.
	addNamedRoute(e, http.MethodPost, "/mcp", mcp.NewServer(e).Handle, "token", m.mcpRateLimit, m.auth)

	// Authenticated routes
	api := e.Group("/api", m.auth)

	// User
	scoped(api, http.MethodGet, "/auth/me", h.auth.Me, "account:read")
	sessionOnly(api, http.MethodPatch, "/auth/me", h.auth.UpdateProfile)
	sessionOnly(api, http.MethodPost, "/auth/me/avatar", h.auth.UploadAvatar)
	scoped(api, http.MethodGet, "/users/:userId/avatar/:file", h.auth.GetAvatar, "account:read")
	// Passkey management is for interactive sessions only; a token can neither
	// add a way to sign in nor inspect the existing ones.
	sessionOnly(api, http.MethodPost, "/auth/passkey/register/begin", h.passkey.BeginRegistration, m.authRateLimit)
	sessionOnly(api, http.MethodPost, "/auth/passkey/register/complete", h.passkey.FinishRegistration, m.authRateLimit)
	sessionOnly(api, http.MethodGet, "/auth/passkey/credentials", h.passkey.List)
	sessionOnly(api, http.MethodPatch, "/auth/passkey/credentials/:id", h.passkey.Rename)
	sessionOnly(api, http.MethodDelete, "/auth/passkey/credentials/:id", h.passkey.Delete)
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
	sessionOnly(ws, http.MethodPost, "/teams/:teamId/icon", h.team.UploadIcon, mw.RequirePermission("team:manage"))
	scoped(ws, http.MethodGet, "/teams/:teamId/icon/:file", h.team.Icon, "teams:read")
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
	scoped(ws, http.MethodPost, "/issues/expand-description", h.aiSettings.ExpandDraftIssueDescription, "issue:create")
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

	// Wikis: pages belong to one team or project; the repository applies the
	// caller's current visibility of that scope on every read and write.
	for _, wiki := range []struct {
		base string
		kind domain.WikiScopeKind
	}{{"/teams/:teamId/wiki", domain.WikiScopeTeam}, {"/projects/:id/wiki", domain.WikiScopeProject}} {
		scoped(ws, http.MethodGet, wiki.base, h.wiki.List(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodGet, wiki.base+"/settings", h.wiki.Settings(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPatch, wiki.base+"/settings", h.wiki.UpdateSettings(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodPost, wiki.base+"/registry", h.wiki.AppendRegistry(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodGet, wiki.base+"/registry", h.wiki.RegistryFeed(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodGet, wiki.base+"/registry/overviews", h.wiki.Overviews(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPost, wiki.base+"/registry/overviews", h.wiki.GenerateOverview(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodPost, wiki.base, h.wiki.Create(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodGet, wiki.base+"/:pageId", h.wiki.Get(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPatch, wiki.base+"/:pageId", h.wiki.Update(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodDelete, wiki.base+"/:pageId", h.wiki.Delete(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodGet, wiki.base+"/:pageId/versions", h.wiki.Revisions(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPost, wiki.base+"/:pageId/rework", h.wiki.Rework(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodGet, wiki.base+"/tree", h.wiki.Tree(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPost, wiki.base+"/tree", h.wiki.CreateInTree(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodPost, wiki.base+"/:pageId/move", h.wiki.Move(wiki.kind), domain.PermWikiWrite)
		scoped(ws, http.MethodGet, wiki.base+"/:pageId/publish", h.wikiPublish.State(wiki.kind), domain.PermWikiRead)
		scoped(ws, http.MethodPut, wiki.base+"/:pageId/publish", h.wikiPublish.Publish(wiki.kind), domain.PermWikiWrite)
		// Backlinks name referencing issues only to callers with issues:read.
		scoped(ws, http.MethodGet, wiki.base+"/:pageId/backlinks", h.wiki.Backlinks(wiki.kind), domain.PermWikiRead)
	}
	// References to wiki pages from issues, comments and other pages. Pages
	// of every team and project are suggested and resolved, each through the
	// caller's current visibility of its wiki; a chip for any other page
	// resolves to nothing but its ID.
	scoped(ws, http.MethodGet, "/wiki/references", h.wiki.SearchReferences, domain.PermWikiRead)
	scoped(ws, http.MethodPost, "/wiki/references/resolve", h.wiki.ResolveReferences, domain.PermWikiRead)
	scoped(ws, http.MethodGet, "/issues/:identifier/wiki-references", h.wiki.IssueReferences, "issues:read", mw.RequirePermission(domain.PermWikiRead))
	scoped(ws, http.MethodPost, "/issues/:identifier/wiki-references", h.wiki.AttachIssueReference, "issue:update", mw.RequirePermission(domain.PermWikiRead))
	scoped(ws, http.MethodDelete, "/issues/:identifier/wiki-references/:pageId", h.wiki.DetachIssueReference, "issue:update")

	// Views
	scoped(ws, http.MethodGet, "/views", h.view.List, "views:read")
	sessionOnly(ws, http.MethodPost, "/views", h.view.Create, mw.RequirePermission(domain.PermViewManage))
	scoped(ws, http.MethodGet, "/views/:id", h.view.Get, "views:read")
	sessionOnly(ws, http.MethodPatch, "/views/:id", h.view.Update, mw.RequirePermission(domain.PermViewManage))
	sessionOnly(ws, http.MethodDelete, "/views/:id", h.view.Delete, mw.RequirePermission(domain.PermViewManage))

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
	// Owners and admins draft skills, profile instructions and step prompts with AI.
	sessionOnly(ws, http.MethodPost, "/ai/write", h.aiSettings.WriteSkill)

	// Conversations are session-only until dedicated PAT scopes are defined.
	sessionOnly(ws, http.MethodGet, "/assistant/status", h.conversation.Status)
	// Members read the assistant's tools; the service lets owners and admins switch them.
	sessionOnly(ws, http.MethodGet, "/assistant-capabilities", h.conversation.Capabilities)
	sessionOnly(ws, http.MethodPut, "/assistant-capabilities/:name", h.conversation.SetCapability)
	// Members read the research settings (never the search key); the service
	// and SQL let only owners and admins change them.
	sessionOnly(ws, http.MethodGet, "/assistant-research-settings", h.assistantResearch.Get)
	sessionOnly(ws, http.MethodPut, "/assistant-research-settings", h.assistantResearch.Update)
	// Agent profiles and workflows (KUA-95): non-guest members read and
	// choose them; the repository lets only current owners and admins write,
	// and audits every change. Tokens may read with workflows:read.
	scoped(ws, http.MethodGet, "/agent-profiles", h.workflow.Profiles, domain.PermWorkflowsRead)
	sessionOnly(ws, http.MethodPost, "/agent-profiles", h.workflow.CreateProfile)
	sessionOnly(ws, http.MethodPut, "/agent-profiles/:profileId", h.workflow.UpdateProfile)
	sessionOnly(ws, http.MethodDelete, "/agent-profiles/:profileId", h.workflow.ArchiveProfile)
	// AI agents as members (KUA-107). Session only: an agent's task runs with
	// the permissions of the person who assigned or mentioned it, so tokens
	// and automations never put an agent to work. The service and SQL refuse
	// guests and agents not enabled for the issue's team; only owners and
	// admins change an agent's teams.
	if h.agentAssignee != nil {
		sessionOnly(ws, http.MethodGet, "/agents", h.agentAssignee.List)
		sessionOnly(ws, http.MethodPut, "/agents/:agentId", h.agentAssignee.Update)
		sessionOnly(ws, http.MethodGet, "/issues/:identifier/agents", h.agentAssignee.IssueAgents)
		sessionOnly(ws, http.MethodPut, "/issues/:identifier/agents", h.agentAssignee.SetIssueAgents)
	}
	scoped(ws, http.MethodGet, "/workflows", h.workflow.List, domain.PermWorkflowsRead)
	sessionOnly(ws, http.MethodPost, "/workflows", h.workflow.Create)
	sessionOnly(ws, http.MethodPost, "/workflows/validate", h.workflow.Validate)
	sessionOnly(ws, http.MethodPut, "/workflow-defaults", h.workflow.SetDefault)
	scoped(ws, http.MethodGet, "/workflows/:workflowId", h.workflow.Get, domain.PermWorkflowsRead)
	sessionOnly(ws, http.MethodPut, "/workflows/:workflowId", h.workflow.Save)
	sessionOnly(ws, http.MethodPost, "/workflows/:workflowId/duplicate", h.workflow.Duplicate)
	sessionOnly(ws, http.MethodPost, "/workflows/:workflowId/archive", h.workflow.Archive)
	sessionOnly(ws, http.MethodDelete, "/workflows/:workflowId", h.workflow.Delete)
	sessionOnly(ws, http.MethodGet, "/workflows/:workflowId/audit", h.workflow.Audit)
	// Members read the skills they can see; the repository lets only current
	// owners and admins write them.
	sessionOnly(ws, http.MethodGet, "/assistant-skills", h.assistantSkill.List)
	sessionOnly(ws, http.MethodPost, "/assistant-skills", h.assistantSkill.Create)
	sessionOnly(ws, http.MethodGet, "/assistant-skills/:skillId", h.assistantSkill.Get)
	sessionOnly(ws, http.MethodPut, "/assistant-skills/:skillId", h.assistantSkill.Update)
	sessionOnly(ws, http.MethodDelete, "/assistant-skills/:skillId", h.assistantSkill.Delete)
	sessionOnly(ws, http.MethodPut, "/assistant-skills/:skillId/enabled", h.assistantSkill.SetEnabled)
	sessionOnly(ws, http.MethodPost, "/assistant-skills/:skillId/activate", h.assistantSkill.Activate)
	sessionOnly(ws, http.MethodGet, "/assistant-skills/:skillId/versions", h.assistantSkill.Versions)
	sessionOnly(ws, http.MethodPost, "/assistant-skills/:skillId/versions/:version/restore", h.assistantSkill.Restore)
	// Members read custom tools and the runs they asked for; the repository
	// lets only current owners and admins write and test-run tools.
	sessionOnly(ws, http.MethodGet, "/assistant-tools", h.assistantTool.List)
	sessionOnly(ws, http.MethodPost, "/assistant-tools", h.assistantTool.Create)
	sessionOnly(ws, http.MethodGet, "/assistant-tools/:toolId", h.assistantTool.Get)
	sessionOnly(ws, http.MethodPut, "/assistant-tools/:toolId", h.assistantTool.Update)
	sessionOnly(ws, http.MethodDelete, "/assistant-tools/:toolId", h.assistantTool.Delete)
	sessionOnly(ws, http.MethodPut, "/assistant-tools/:toolId/enabled", h.assistantTool.SetEnabled)
	sessionOnly(ws, http.MethodPost, "/assistant-tools/:toolId/activate", h.assistantTool.Activate)
	sessionOnly(ws, http.MethodGet, "/assistant-tools/:toolId/versions", h.assistantTool.Versions)
	sessionOnly(ws, http.MethodPost, "/assistant-tools/:toolId/versions/:version/restore", h.assistantTool.Restore)
	sessionOnly(ws, http.MethodPost, "/assistant-tools/:toolId/test-run", h.assistantTool.TestRun)
	sessionOnly(ws, http.MethodGet, "/assistant-tools/:toolId/runs", h.assistantTool.Runs)
	sessionOnly(ws, http.MethodGet, "/assistant-tool-runs/:runId", h.assistantTool.Run)
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
		public(e, http.MethodPost, "/api/telegram/webhook/:workspaceId", h.telegram.Webhook, m.publicRateLimit)
	}

	// GitHub integration (conditional)
	// Public webhook endpoint (no auth, signature-verified internally)
	public(e, http.MethodPost, "/api/github/webhook", h.github.HandleWebhook)
	// The uploaded bot picture is public so GitHub can show it in bot comments.
	public(e, http.MethodGet, "/api/github-bot/:workspaceId/avatar/:file", h.githubBot.Avatar, m.publicAssetRateLimit)
	public(e, http.MethodPost, "/api/dev-machine-ingest/events", h.devMachine.IngestEvent, m.machineEventsRateLimit)
	public(e, http.MethodPost, "/api/dev-machine-ingest/logs", h.devMachine.IngestLog, m.machineLogsRateLimit)

	// GitHub integration (workspace-scoped)
	sessionOnly(ws, http.MethodGet, "/github/status", h.github.Status)
	scoped(ws, http.MethodGet, "/github/setup", h.github.Setup, "workspace:manage")
	scoped(ws, http.MethodGet, "/github/app-setup", h.github.AppSetup, "workspace:manage")
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
	// Members see the bot identity and abilities; managers change them.
	sessionOnly(ws, http.MethodGet, "/github/bot", h.githubBot.Get)
	scoped(ws, http.MethodPatch, "/github/bot", h.githubBot.Update, "workspace:manage")
	scoped(ws, http.MethodPost, "/github/bot/avatar", h.githubBot.UploadAvatar, "workspace:manage")
	scoped(ws, http.MethodGet, "/issues/:identifier/github", h.github.IssueGitHubActivity, "issues:read")
	scoped(ws, http.MethodGet, "/github/issue-links", h.github.AgentIssueLinks, "issues:read")

	// Dev Machines — guarded by demo-mode restriction when active
	dm := ws.Group("", m.devMachineDemoGuard)
	sessionOnly(dm, http.MethodGet, "/conversations/:conversationId/task-options", h.agentTask.Options)
	sessionOnly(dm, http.MethodGet, "/conversations/:conversationId/tasks", h.agentTask.List)
	sessionOnly(dm, http.MethodPost, "/conversations/:conversationId/tasks", h.agentTask.Create)
	// Tokens may read a task, its history and its runs with agent_tasks:read;
	// the repository applies the same task visibility as for sessions. Logs,
	// diffs, artifacts, sessions and live messages stay session-only.
	scoped(dm, http.MethodGet, "/agent-tasks/:taskId", h.agentTask.Get, domain.PermAgentTasksRead)
	scoped(dm, http.MethodGet, "/agent-tasks/:taskId/history", h.agentTask.History, domain.PermAgentTasksRead)
	scoped(dm, http.MethodGet, "/agent-tasks/:taskId/runs", h.agentTask.Runs, domain.PermAgentTasksRead)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/logs", h.agentTask.Logs)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/validation", h.agentTask.Validation)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/checks", h.agentTask.ManualChecks)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/diff", h.agentTask.Diff)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/prompt", h.agentTask.Prompt)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/notebook", h.agentTask.Notebook)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/artifacts", h.agentTask.Artifacts)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/artifacts/:artifactId", h.agentTask.Artifact)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/runs/:runId/publication", h.agentTask.Publication)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/signals", h.agentTask.Signals)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/sessions", h.agentTask.Sessions)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/live-messages", h.agentTask.LiveMessages)
	scoped(dm, http.MethodGet, "/agent-tasks", h.agentTask.Queue, domain.PermAgentTasksRead)
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
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/live-messages", h.agentTask.SendLiveMessage)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/cancel", h.agentTask.Cancel)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/retry", h.agentTask.Retry)
	// A task paused by its agent's usage limit: resume now, or don't resume.
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/resume", h.agentTask.ResumeUsageLimit)
	sessionOnly(dm, http.MethodDelete, "/agent-tasks/:taskId/resume", h.agentTask.CancelUsageLimitResume)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/accept", h.agentTask.Accept)
	sessionOnly(dm, http.MethodGet, "/agent-tasks/:taskId/workflow", h.workflow.TaskWorkflow)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/workflow/decision", h.workflow.Decide)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/sign-in", h.agentTask.ContinueAfterSignIn)
	sessionOnly(dm, http.MethodPost, "/agent-tasks/:taskId/terminal", h.agentTask.OpenInClaudeCode)
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
	scoped(dm, http.MethodGet, "/dev-machine-merge-methods", h.mergeMethods.List, "dev_machine:read")
	scoped(dm, http.MethodPut, "/dev-machine-merge-methods", h.mergeMethods.Update, "dev_machine:admin")
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
	scoped(dm, http.MethodGet, "/agent-model-defaults", h.devMachine.ListAgentModelDefaults, "dev_machine:read")
	scoped(dm, http.MethodPut, "/agent-model-defaults/:provider", h.devMachine.SaveAgentModelDefault, "dev_machine:admin")
	scoped(dm, http.MethodGet, "/agent-model-catalog", h.devMachine.ModelCatalog, "dev_machine:read")
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
