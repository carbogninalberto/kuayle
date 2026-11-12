package main

// sessionOnlyReason explains why a route stays available to interactive
// sessions only instead of becoming reachable by a personal access token.
// Rules are matched in order against the route's method and path prefix
// (or exact path); an empty Method matches every method.
// token_matrix_test.go requires every session-only route to match a rule and
// every rule to match a route.
type sessionOnlyReason struct {
	Method     string
	PathPrefix string
	Exact      bool
	Reason     string
}

const (
	reasonTokenManagement = "A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in."
	reasonSignInSecrets   = "Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in."
	reasonInstanceUpdate  = "Updating the instance runs privileged host operations and stays an operator action."
	reasonWorkspaceAdmin  = "Workspace creation, import and invite acceptance bind the signed-in account and stay interactive."
	reasonAssistant       = "Assistant conversations, skills, tools and research run with the signed-in user's context and secrets."
	reasonAgentRuntime    = "Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user."
	reasonAgentProfiles   = "Agent profiles, workflows and memory are authored with AI and audited as person decisions."
	reasonDevMachine      = "Dev-machine ports and task bindings are interactive runtime operations."
	reasonSharedLinks     = "Shared links publish workspace data publicly and stay an interactive decision."
	reasonGitHubSetup     = "GitHub integration state is managed from the settings pages."
	reasonWebSocket       = "The realtime WebSocket connection carries live assistant events for a browser session."
	reasonPersonalState   = "Personal state writes follow the signed-in person's UI session."
)

var sessionOnlyReasons = []sessionOnlyReason{
	{PathPrefix: "/api/tokens", Reason: reasonTokenManagement},
	{PathPrefix: "/api/cli-links", Reason: reasonTokenManagement},
	{PathPrefix: "/api/auth/passkey", Reason: reasonSignInSecrets},
	{Method: "PATCH", PathPrefix: "/api/auth/me", Exact: true, Reason: reasonSignInSecrets},
	{Method: "POST", PathPrefix: "/api/auth/me/avatar", Exact: true, Reason: reasonSignInSecrets},
	{PathPrefix: "/api/system/update", Reason: reasonInstanceUpdate},
	{Method: "POST", PathPrefix: "/api/workspaces", Exact: true, Reason: reasonWorkspaceAdmin},
	{Method: "POST", PathPrefix: "/api/workspaces/import", Reason: reasonWorkspaceAdmin},
	{Method: "POST", PathPrefix: "/api/invite/", Reason: reasonWorkspaceAdmin},
	{PathPrefix: "/api/workspaces/:slug/agent-profiles", Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/workflows", Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/workflow-defaults", Exact: true, Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/memory/", Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/agents", Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/issues/:identifier/agents", Reason: reasonAgentProfiles},
	{PathPrefix: "/api/workspaces/:slug/agent-tasks", Reason: reasonAgentRuntime},
	{PathPrefix: "/api/workspaces/:slug/agent-automation", Reason: reasonAgentRuntime},
	{PathPrefix: "/api/workspaces/:slug/agent-messages", Reason: reasonAgentRuntime},
	{PathPrefix: "/api/workspaces/:slug/agent-message-holds", Reason: reasonAgentRuntime},
	{PathPrefix: "/api/workspaces/:slug/agent-sign-ins", Reason: reasonAgentRuntime},
	{PathPrefix: "/api/workspaces/:slug/conversations", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/conversation-folders", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/assistant", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/ai/", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/voice", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/telegram", Reason: reasonAssistant},
	{PathPrefix: "/api/workspaces/:slug/dev-machines/:machineId/ports", Reason: reasonDevMachine},
	{PathPrefix: "/api/workspaces/:slug/dev-machines/:machineId/tasks", Reason: reasonDevMachine},
	{PathPrefix: "/api/workspaces/:slug/shared-links", Reason: reasonSharedLinks},
	{PathPrefix: "/api/workspaces/:slug/github", Reason: reasonGitHubSetup},
	{PathPrefix: "/api/workspaces/:slug/ws", Exact: true, Reason: reasonWebSocket},
	{Method: "POST", PathPrefix: "/api/workspaces/:slug/teams/:teamId/icon", Exact: true, Reason: reasonWorkspaceAdmin},
	{Method: "POST", PathPrefix: "/api/workspaces/:slug/teams/:teamId/leave", Exact: true, Reason: reasonWorkspaceAdmin},
	{PathPrefix: "/api/workspaces/:slug/issues/:identifier/prompt-assets", Exact: true, Reason: reasonAssistant},
}
