# API tokens: endpoint and scope matrix

Personal access tokens (`kuayle_pat_…`) authenticate API clients without a
browser session. This page is the contract for which endpoints a token can
reach, which scope gates each one, and why some routes stay interactive-only.
`BE/cmd/server/token_matrix_test.go` generates the matrix from the registered
routes, so this page cannot drift from `routes.go`.

## Permission model

A token request is authorized by the intersection of two sets:

1. **The token's scopes** (`scopes`, the ceiling). A route annotated
   `perm:<scope>` requires that scope in the token.
2. **The owner's workspace role** (RBAC, the floor). The same permission must
   be held by the owner's role in the workspace handling the request
   (`domain.RolePermissions`). User-level routes without a workspace role are
   governed by the scope alone.

A token can additionally be restricted to a list of workspaces
(`workspace_slugs`); requests to any other workspace are rejected before the
handler runs. Routes that return data across workspaces (`/api/notifications`,
`/api/preferences`) also reject tokens with a workspace restriction.

Scope names are the permission codes used by role RBAC. `validTokenScopes` in
`BE/internal/domain/personal_access_token.go` is the public list of scopes a
token may request. Roles hold `account:write` and `notifications:write` for
every member, including guests; every other write scope follows the role
matrix.

## Gaps found (KUA-111)

The audit that produced this page found these concrete gaps:

1. `issue:delete`, `cycle:manage` and `view:manage` were RBAC permissions but
   not valid token scopes. Cycle and view writes (including cycle completion)
   were registered session-only, so even a full-access token got `403`.
2. Issue delete and bulk delete decided "delete any issue" from the workspace
   role alone. A token holding only `issue:delete_own` could delete other
   users' issues when its owner was an admin or owner. The token ceiling is
   now applied (`middleware.TokenAllows`).
3. Personal-state writes were session-only: issue subscribe/unsubscribe,
   favorites POST/DELETE, `PATCH /preferences` and every notification
   mutation. They are now reachable with `account:write` and
   `notifications:write`.
4. The token-creation preset labelled "Full access" expanded to the stored
   scope list, so adding a scope left older full-access tokens behind.
   Migration `000153_pat_full_access_scopes` extends active tokens that held
   the complete previous full-access set.
5. Request logs recorded the raw URL path, leaking invite, share, wiki and
   asset tokens. The logging middleware now records the route template, and
   `domain.RedactSecrets` masks `kuayle_pat_`/`kuayle_cl_` values.

## Scope to resource mapping

| Scope | Reaches |
| --- | --- |
| `issues:read` | Issue list, detail, sub-issues, history, comments list, relations list, GitHub activity |
| `issue:create` | Create issues, sub-issues, comments, templates, uploads, duplicates, drafts |
| `issue:update` | Issue edits, bulk update, comments resolve/reopen, relations, triage, wiki references |
| `issue:delete_own` | Delete own issues; bulk delete applies the same ceiling per issue |
| `issue:delete` | Delete any issue in the workspace (requires `issue:delete_own` route access as well) |
| `comments:read` | Issue comment threads |
| `projects:read` / `project:manage` | Projects and project conversion |
| `cycles:read` / `cycle:manage` | Cycle reads / create, update, complete, delete |
| `views:read` / `view:manage` | Saved views reads / create, update, delete |
| `labels:read` / `label:manage` | Labels |
| `teams:read` / `team:manage` | Teams, memberships and statuses |
| `members:read` / `member:invite` | Workspace members and invitations |
| `workspaces:read` / `workspace:manage` / `workspace:transfer` | Workspace metadata, settings and export |
| `templates:read` | Issue templates |
| `wiki:read` / `wiki:write` | Team and project wikis, registry, publishing |
| `analytics:read` | Analytics screens |
| `notifications:read` / `notifications:write` | Notification list / mutations |
| `account:read` / `account:write` | Profile reads, preferences, favorites, issue subscriptions |
| `assets:read` | Workspace assets |
| `agent_tasks:read` / `workflows:read` | Agent task, workflow and profile reads (member-only) |
| `dev_machine:*` | Dev machine, terminal and agent infrastructure (internal API) |

## Generated endpoint matrix

Every registered route appears below with the access it requires. `token`
means a personal access token with the listed scope; a token holding every
scope listed in "Scope to resource mapping" (the UI's **Full access** preset)
is allowed into every such row, which the matrix test asserts. `session only`
means the route rejects tokens even when they hold every scope, and the reason
is recorded in `BE/cmd/server/session_only_reasons.go`. `public` routes need no
authentication.

<!-- token-matrix:start -->
<!-- Generated by TestTokenMatrixDocumentation in BE/cmd/server. Do not edit by hand. -->
<!-- Run `cd BE && go test ./cmd/server -run TestTokenMatrixDocumentation -update` to refresh. -->

| Method | Path | Access | Scope / reason |
| --- | --- | --- | --- |
| POST | `/api/auth/forgot-password` | public |  |
| POST | `/api/auth/login` | public |  |
| POST | `/api/auth/logout` | public |  |
| GET | `/api/auth/me` | token | account:read |
| PATCH | `/api/auth/me` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| POST | `/api/auth/me/avatar` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| GET | `/api/auth/passkey/credentials` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| DELETE | `/api/auth/passkey/credentials/:id` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| PATCH | `/api/auth/passkey/credentials/:id` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| POST | `/api/auth/passkey/login/begin` | public |  |
| POST | `/api/auth/passkey/login/complete` | public |  |
| POST | `/api/auth/passkey/register/begin` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| POST | `/api/auth/passkey/register/complete` | session only | Sign-in credentials (passkeys, profile, avatar) stay interactive so a leaked token cannot add or read a way to sign in. |
| POST | `/api/auth/refresh` | public |  |
| POST | `/api/auth/register` | public |  |
| POST | `/api/auth/reset-password` | public |  |
| GET | `/api/auth/sso/:slug` | public |  |
| GET | `/api/auth/sso/:slug/callback` | public |  |
| GET | `/api/auth/sso/:slug/start` | public |  |
| GET | `/api/cli-links` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| POST | `/api/cli-links` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| DELETE | `/api/cli-links/:id` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| POST | `/api/cli/connect` | public |  |
| GET | `/api/config` | public |  |
| POST | `/api/dev-machine-ingest/events` | public |  |
| POST | `/api/dev-machine-ingest/logs` | public |  |
| POST | `/api/forges/webhook/:connectionId` | public |  |
| GET | `/api/github-bot/:workspaceId/avatar/:file` | public |  |
| POST | `/api/github/webhook` | public |  |
| GET | `/api/invite/:token` | public |  |
| POST | `/api/invite/:token/accept` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| GET | `/api/notifications` | token | notifications:read |
| PATCH | `/api/notifications/:id` | token | notifications:write |
| POST | `/api/notifications/:id/archive` | token | notifications:write |
| POST | `/api/notifications/:id/read` | token | notifications:write |
| POST | `/api/notifications/:id/snooze` | token | notifications:write |
| POST | `/api/notifications/:id/unarchive` | token | notifications:write |
| POST | `/api/notifications/:id/unread` | token | notifications:write |
| POST | `/api/notifications/:id/unsnooze` | token | notifications:write |
| POST | `/api/notifications/mark-all-read` | token | notifications:write |
| GET | `/api/openapi.json` | public |  |
| GET | `/api/preferences` | token | account:read |
| PATCH | `/api/preferences` | token | account:write |
| GET | `/api/public/assets/:token` | public |  |
| GET | `/api/public/share/:token` | public |  |
| GET | `/api/public/share/:token/issues` | public |  |
| GET | `/api/public/system/update-activity` | public |  |
| GET | `/api/public/wiki/:token` | public |  |
| POST | `/api/system/update` | session only | Updating the instance runs privileged host operations and stays an operator action. |
| GET | `/api/system/update-status` | session only | Updating the instance runs privileged host operations and stays an operator action. |
| POST | `/api/telegram/webhook/:workspaceId` | public |  |
| GET | `/api/tokens` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| POST | `/api/tokens` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| DELETE | `/api/tokens/:id` | session only | A token cannot mint, inspect or revoke tokens; those actions require the interactive sign-in. |
| DELETE | `/api/tokens/current` | token only | the caller's own token |
| GET | `/api/users/:userId/avatar/:file` | token | account:read |
| GET | `/api/workspace-logos/:workspaceId/:file` | public |  |
| GET | `/api/workspaces` | token | workspaces:read |
| POST | `/api/workspaces` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| DELETE | `/api/workspaces/:slug` | session only | workspace owner |
| GET | `/api/workspaces/:slug` | token | workspaces:read |
| PATCH | `/api/workspaces/:slug` | session only | workspace owner |
| GET | `/api/workspaces/:slug/agent-automation-runs` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-automation-runs/:runId/approve` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-automation-runs/:runId/dismiss` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-automations` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-automations` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| DELETE | `/api/workspaces/:slug/agent-automations/:automationId` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| PATCH | `/api/workspaces/:slug/agent-automations/:automationId` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-message-holds` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-message-holds` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| DELETE | `/api/workspaces/:slug/agent-message-holds/:holdId` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-messages` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-messages/:messageId` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-messages/:messageId/drop` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-messages/:messageId/hold` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-messages/:messageId/release` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-model-catalog` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/agent-model-defaults` | token | dev_machine:read |
| PUT | `/api/workspaces/:slug/agent-model-defaults/:provider` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/agent-model-servers` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/agent-model-servers` | token | dev_machine:admin |
| DELETE | `/api/workspaces/:slug/agent-model-servers/:serverId` | token | dev_machine:admin |
| PUT | `/api/workspaces/:slug/agent-model-servers/:serverId` | token | dev_machine:admin |
| POST | `/api/workspaces/:slug/agent-model-servers/discover` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/agent-profiles` | token | workflows:read |
| POST | `/api/workspaces/:slug/agent-profiles` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| DELETE | `/api/workspaces/:slug/agent-profiles/:profileId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| PUT | `/api/workspaces/:slug/agent-profiles/:profileId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/agent-sign-ins` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| DELETE | `/api/workspaces/:slug/agent-sign-ins/:provider` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| PUT | `/api/workspaces/:slug/agent-sign-ins/:provider/token` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks` | token | agent_tasks:read |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId` | token | agent_tasks:read |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/accept` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/cancel` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/checks` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/debug` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/history` | token | agent_tasks:read |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/live-messages` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/live-messages` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/notebook` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/pause` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| DELETE | `/api/workspaces/:slug/agent-tasks/:taskId/preview` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/preview` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| DELETE | `/api/workspaces/:slug/agent-tasks/:taskId/resume` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/resume` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/retry` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs` | token | agent_tasks:read |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/artifacts` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/artifacts/:artifactId` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/checks` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/diff` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/logs` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/prompt` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/publication` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/runs/:runId/validation` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/sessions` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/sign-in` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/signals` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/terminal` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/workflow` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/workflow/decision` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agent-tasks/:taskId/workflow/switch` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| POST | `/api/workspaces/:slug/agent-tasks/:taskId/workflow/switch` | session only | Agent runs, logs, diffs, prompts and runtime mutations can expose code or credentials and act as the user. |
| GET | `/api/workspaces/:slug/agents` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| PUT | `/api/workspaces/:slug/agents/:agentId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/ai-settings` | session only | workspace owner |
| PATCH | `/api/workspaces/:slug/ai-settings` | session only | workspace owner |
| GET | `/api/workspaces/:slug/ai-settings/issue-copy-prompt` | token | issues:read |
| POST | `/api/workspaces/:slug/ai-settings/vision-check` | session only | workspace owner |
| POST | `/api/workspaces/:slug/ai/write` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/analytics/burnup` | token | analytics:read |
| GET | `/api/workspaces/:slug/analytics/distribution` | token | analytics:read |
| GET | `/api/workspaces/:slug/analytics/insights` | token | analytics:read |
| GET | `/api/workspaces/:slug/analytics/overview` | token | analytics:read |
| GET | `/api/workspaces/:slug/assets/:assetId` | token | assets:read |
| GET | `/api/workspaces/:slug/assistant-capabilities` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-capabilities/:name` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-research-settings` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-research-settings` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-skills` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-skills` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/assistant-skills/:skillId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-skills/:skillId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-skills/:skillId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-skills/:skillId/activate` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-skills/:skillId/enabled` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-skills/:skillId/versions` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-skills/:skillId/versions/:version/restore` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-tool-runs/:runId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-tools` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-tools` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/assistant-tools/:toolId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-tools/:toolId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-tools/:toolId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-tools/:toolId/activate` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/assistant-tools/:toolId/enabled` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-tools/:toolId/runs` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-tools/:toolId/test-run` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant-tools/:toolId/versions` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/assistant-tools/:toolId/versions/:version/restore` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/assistant/status` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversation-folders` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversation-folders` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/conversation-folders/:folderId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PATCH | `/api/workspaces/:slug/conversation-folders/:folderId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/conversations/:conversationId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PATCH | `/api/workspaces/:slug/conversations/:conversationId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/approvals/:approvalId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/attachments` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/conversations/:conversationId/attachments/:attachmentId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId/attachments/:attachmentId` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/cancel` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId/events` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/messages` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/messages/:messageId/retry` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId/stream` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId/task-options` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/conversations/:conversationId/tasks` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/tasks` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/conversations/:conversationId/transcriptions` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/dev-machine-check-suggestion` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machine-environments` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machine-environments` | token | dev_machine:admin |
| DELETE | `/api/workspaces/:slug/dev-machine-environments/:environmentId` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/dev-machine-environments/:environmentId` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machine-merge-methods` | token | dev_machine:read |
| PUT | `/api/workspaces/:slug/dev-machine-merge-methods` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/dev-machine-names/availability` | token | dev_machine:create |
| GET | `/api/workspaces/:slug/dev-machine-names/suggestion` | token | dev_machine:create |
| GET | `/api/workspaces/:slug/dev-machine-policy` | token | dev_machine:read |
| PATCH | `/api/workspaces/:slug/dev-machine-policy` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/dev-machine-providers` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machine-repository-defaults` | token | dev_machine:read |
| DELETE | `/api/workspaces/:slug/dev-machine-scope-setting` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machine-scope-setting` | token | dev_machine:read |
| PUT | `/api/workspaces/:slug/dev-machine-scope-setting` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machine-scope-settings` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machines` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines` | token | dev_machine:create |
| DELETE | `/api/workspaces/:slug/dev-machines/:machineId` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/dev-machines/:machineId` | token | dev_machine:read |
| PATCH | `/api/workspaces/:slug/dev-machines/:machineId` | token | dev_machine:manage |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/activity` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/checkouts` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/checkouts` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/events` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/logs` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/pause` | token | dev_machine:manage |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/permanent-delete` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/ports` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/ports` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| DELETE | `/api/workspaces/:slug/dev-machines/:machineId/ports/:portId` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| PATCH | `/api/workspaces/:slug/dev-machines/:machineId/ports/:portId` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/ports/:portId/launch` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/providers` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/resource-usage` | token | dev_machine:read |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/services` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/services/:service/launch` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/start` | token | dev_machine:manage |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/stop` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/tasks` | session only | Dev-machine ports and task bindings are interactive runtime operations. |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/teardown` | token | dev_machine:manage |
| GET | `/api/workspaces/:slug/dev-machines/:machineId/terminal-sessions` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/terminal-sessions` | token | dev_machine:read |
| POST | `/api/workspaces/:slug/dev-machines/:machineId/terminal-sessions/:sessionId/close` | token | dev_machine:read |
| DELETE | `/api/workspaces/:slug/dev-machines/bulk` | token | dev_machine:admin |
| POST | `/api/workspaces/:slug/dev-machines/bulk/permanent-delete` | token | dev_machine:admin |
| GET | `/api/workspaces/:slug/export` | token | workspace:transfer |
| GET | `/api/workspaces/:slug/favorites` | token | account:read |
| POST | `/api/workspaces/:slug/favorites` | token | account:write |
| DELETE | `/api/workspaces/:slug/favorites/:id` | token | account:write |
| GET | `/api/workspaces/:slug/forges/connections` | token | workspace:manage |
| POST | `/api/workspaces/:slug/forges/connections` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/forges/connections/:id` | token | workspace:manage |
| PATCH | `/api/workspaces/:slug/forges/connections/:id` | token | workspace:manage |
| GET | `/api/workspaces/:slug/forges/connections/:id/repos` | token | workspace:manage |
| POST | `/api/workspaces/:slug/forges/connections/:id/repos` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/forges/connections/:id/repos/:repoId` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/github/app` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/app-setup` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/auto-transitions` | token | workspace:manage |
| PATCH | `/api/workspaces/:slug/github/auto-transitions` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/bot` | session only | GitHub integration state is managed from the settings pages. |
| PATCH | `/api/workspaces/:slug/github/bot` | token | workspace:manage |
| POST | `/api/workspaces/:slug/github/bot/avatar` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/callback` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/github/disconnect` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/install` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/issue-links` | token | issues:read |
| GET | `/api/workspaces/:slug/github/repos` | token | workspace:manage |
| POST | `/api/workspaces/:slug/github/repos` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/github/repos/:id` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/setup` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/setup/callback` | token | workspace:manage |
| GET | `/api/workspaces/:slug/github/status` | session only | GitHub integration state is managed from the settings pages. |
| POST | `/api/workspaces/:slug/invite` | token | member:invite |
| GET | `/api/workspaces/:slug/invite-links` | token | member:invite |
| POST | `/api/workspaces/:slug/invite-links` | token | member:invite |
| DELETE | `/api/workspaces/:slug/invite-links/:id` | token | member:invite |
| GET | `/api/workspaces/:slug/issue-templates` | token | templates:read |
| POST | `/api/workspaces/:slug/issue-templates` | token | issue:create |
| DELETE | `/api/workspaces/:slug/issue-templates/:id` | token | issue:create |
| GET | `/api/workspaces/:slug/issue-templates/:id` | token | templates:read |
| PATCH | `/api/workspaces/:slug/issue-templates/:id` | token | issue:create |
| GET | `/api/workspaces/:slug/issues` | token | issues:read |
| POST | `/api/workspaces/:slug/issues` | token | issue:create |
| DELETE | `/api/workspaces/:slug/issues/:identifier` | token | issue:delete_own |
| GET | `/api/workspaces/:slug/issues/:identifier` | token | issues:read |
| PATCH | `/api/workspaces/:slug/issues/:identifier` | token | issue:update |
| GET | `/api/workspaces/:slug/issues/:identifier/agents` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| PUT | `/api/workspaces/:slug/issues/:identifier/agents` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/issues/:identifier/comments` | token | comments:read |
| POST | `/api/workspaces/:slug/issues/:identifier/comments` | token | issue:create |
| POST | `/api/workspaces/:slug/issues/:identifier/comments/:commentId/reopen` | token | issue:update |
| POST | `/api/workspaces/:slug/issues/:identifier/comments/:commentId/resolve` | token | issue:update |
| POST | `/api/workspaces/:slug/issues/:identifier/convert-to-project` | token | project:manage |
| POST | `/api/workspaces/:slug/issues/:identifier/duplicate` | token | issue:create |
| POST | `/api/workspaces/:slug/issues/:identifier/expand-description` | token | issue:update |
| GET | `/api/workspaces/:slug/issues/:identifier/forges` | token | issues:read |
| GET | `/api/workspaces/:slug/issues/:identifier/github` | token | issues:read |
| GET | `/api/workspaces/:slug/issues/:identifier/history` | token | issues:read |
| POST | `/api/workspaces/:slug/issues/:identifier/prompt-assets` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| GET | `/api/workspaces/:slug/issues/:identifier/relations` | token | issues:read |
| POST | `/api/workspaces/:slug/issues/:identifier/relations` | token | issue:update |
| DELETE | `/api/workspaces/:slug/issues/:identifier/relations/:relationId` | token | issue:update |
| GET | `/api/workspaces/:slug/issues/:identifier/sub-issues` | token | issues:read |
| POST | `/api/workspaces/:slug/issues/:identifier/sub-issues` | token | issue:create |
| POST | `/api/workspaces/:slug/issues/:identifier/sub-issues/bulk` | token | issue:create |
| DELETE | `/api/workspaces/:slug/issues/:identifier/subscribe` | token | account:write |
| POST | `/api/workspaces/:slug/issues/:identifier/subscribe` | token | account:write |
| POST | `/api/workspaces/:slug/issues/:identifier/triage/accept` | token | issue:update |
| POST | `/api/workspaces/:slug/issues/:identifier/triage/decline` | token | issue:update |
| GET | `/api/workspaces/:slug/issues/:identifier/wiki-references` | token | issues:read |
| POST | `/api/workspaces/:slug/issues/:identifier/wiki-references` | token | issue:update |
| DELETE | `/api/workspaces/:slug/issues/:identifier/wiki-references/:pageId` | token | issue:update |
| DELETE | `/api/workspaces/:slug/issues/bulk` | token | issue:delete_own |
| PATCH | `/api/workspaces/:slug/issues/bulk` | token | issue:update |
| POST | `/api/workspaces/:slug/issues/expand-description` | token | issue:create |
| GET | `/api/workspaces/:slug/labels` | token | labels:read |
| POST | `/api/workspaces/:slug/labels` | token | label:manage |
| DELETE | `/api/workspaces/:slug/labels/:id` | token | label:manage |
| PATCH | `/api/workspaces/:slug/labels/:id` | token | label:manage |
| POST | `/api/workspaces/:slug/logo` | session only | workspace owner |
| GET | `/api/workspaces/:slug/members` | token | members:read |
| DELETE | `/api/workspaces/:slug/members/:userId` | token | member:invite |
| PATCH | `/api/workspaces/:slug/members/:userId` | token | member:invite |
| GET | `/api/workspaces/:slug/memory/lessons` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/memory/lessons` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| PUT | `/api/workspaces/:slug/memory/lessons/:lessonId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/memory/lessons/:lessonId/approve` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/memory/lessons/:lessonId/audit` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/memory/lessons/:lessonId/drop` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/memory/lessons/:lessonId/retire` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| PUT | `/api/workspaces/:slug/memory/lessons/order` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/projects` | token | projects:read |
| POST | `/api/workspaces/:slug/projects` | token | project:manage |
| DELETE | `/api/workspaces/:slug/projects/:id` | token | project:manage |
| GET | `/api/workspaces/:slug/projects/:id` | token | projects:read |
| PATCH | `/api/workspaces/:slug/projects/:id` | token | project:manage |
| GET | `/api/workspaces/:slug/projects/:id/wiki` | token | wiki:read |
| POST | `/api/workspaces/:slug/projects/:id/wiki` | token | wiki:write |
| DELETE | `/api/workspaces/:slug/projects/:id/wiki/:pageId` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/:pageId` | token | wiki:read |
| PATCH | `/api/workspaces/:slug/projects/:id/wiki/:pageId` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/:pageId/backlinks` | token | wiki:read |
| POST | `/api/workspaces/:slug/projects/:id/wiki/:pageId/move` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/:pageId/publish` | token | wiki:read |
| PUT | `/api/workspaces/:slug/projects/:id/wiki/:pageId/publish` | token | wiki:write |
| POST | `/api/workspaces/:slug/projects/:id/wiki/:pageId/rework` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/:pageId/versions` | token | wiki:read |
| GET | `/api/workspaces/:slug/projects/:id/wiki/registry` | token | wiki:read |
| POST | `/api/workspaces/:slug/projects/:id/wiki/registry` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/registry/overviews` | token | wiki:read |
| POST | `/api/workspaces/:slug/projects/:id/wiki/registry/overviews` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/settings` | token | wiki:read |
| PATCH | `/api/workspaces/:slug/projects/:id/wiki/settings` | token | wiki:write |
| GET | `/api/workspaces/:slug/projects/:id/wiki/tree` | token | wiki:read |
| POST | `/api/workspaces/:slug/projects/:id/wiki/tree` | token | wiki:write |
| GET | `/api/workspaces/:slug/shared-links` | session only | Shared links publish workspace data publicly and stay an interactive decision. |
| POST | `/api/workspaces/:slug/shared-links` | session only | Shared links publish workspace data publicly and stay an interactive decision. |
| DELETE | `/api/workspaces/:slug/shared-links/:id` | session only | Shared links publish workspace data publicly and stay an interactive decision. |
| PATCH | `/api/workspaces/:slug/shared-links/:id` | session only | Shared links publish workspace data publicly and stay an interactive decision. |
| DELETE | `/api/workspaces/:slug/sso` | session only | An SSO client secret is write-only and points the workspace at an identity provider; only an interactive owner or admin may change it. |
| GET | `/api/workspaces/:slug/sso` | session only | An SSO client secret is write-only and points the workspace at an identity provider; only an interactive owner or admin may change it. |
| PUT | `/api/workspaces/:slug/sso` | session only | An SSO client secret is write-only and points the workspace at an identity provider; only an interactive owner or admin may change it. |
| GET | `/api/workspaces/:slug/teams` | token | teams:read |
| POST | `/api/workspaces/:slug/teams` | token | team:manage |
| DELETE | `/api/workspaces/:slug/teams/:teamId` | token | team:manage |
| GET | `/api/workspaces/:slug/teams/:teamId` | token | teams:read |
| PATCH | `/api/workspaces/:slug/teams/:teamId` | token | team:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/cycles` | token | cycles:read |
| POST | `/api/workspaces/:slug/teams/:teamId/cycles` | token | cycle:manage |
| DELETE | `/api/workspaces/:slug/teams/:teamId/cycles/:cycleId` | token | cycle:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/cycles/:cycleId` | token | cycles:read |
| PATCH | `/api/workspaces/:slug/teams/:teamId/cycles/:cycleId` | token | cycle:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/cycles/:cycleId/burndown` | token | cycles:read |
| POST | `/api/workspaces/:slug/teams/:teamId/cycles/:cycleId/complete` | token | cycle:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/cycles/velocity` | token | cycles:read |
| POST | `/api/workspaces/:slug/teams/:teamId/icon` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| GET | `/api/workspaces/:slug/teams/:teamId/icon/:file` | token | teams:read |
| POST | `/api/workspaces/:slug/teams/:teamId/leave` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| GET | `/api/workspaces/:slug/teams/:teamId/members` | token | members:read |
| POST | `/api/workspaces/:slug/teams/:teamId/members` | token | team:manage |
| DELETE | `/api/workspaces/:slug/teams/:teamId/members/:userId` | token | team:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/projects` | token | projects:read |
| GET | `/api/workspaces/:slug/teams/:teamId/statuses` | token | teams:read |
| POST | `/api/workspaces/:slug/teams/:teamId/statuses` | token | team:manage |
| DELETE | `/api/workspaces/:slug/teams/:teamId/statuses/:statusId` | token | team:manage |
| PATCH | `/api/workspaces/:slug/teams/:teamId/statuses/:statusId` | token | team:manage |
| PATCH | `/api/workspaces/:slug/teams/:teamId/visibility` | token | team:manage |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki` | token | wiki:read |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki` | token | wiki:write |
| DELETE | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId` | token | wiki:read |
| PATCH | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/backlinks` | token | wiki:read |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/move` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/publish` | token | wiki:read |
| PUT | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/publish` | token | wiki:write |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/rework` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/:pageId/versions` | token | wiki:read |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/registry` | token | wiki:read |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki/registry` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/registry/overviews` | token | wiki:read |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki/registry/overviews` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/settings` | token | wiki:read |
| PATCH | `/api/workspaces/:slug/teams/:teamId/wiki/settings` | token | wiki:write |
| GET | `/api/workspaces/:slug/teams/:teamId/wiki/tree` | token | wiki:read |
| POST | `/api/workspaces/:slug/teams/:teamId/wiki/tree` | token | wiki:write |
| GET | `/api/workspaces/:slug/telegram` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PATCH | `/api/workspaces/:slug/telegram` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| DELETE | `/api/workspaces/:slug/telegram/link` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/telegram/link` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| POST | `/api/workspaces/:slug/upload` | token | issue:create |
| GET | `/api/workspaces/:slug/views` | token | views:read |
| POST | `/api/workspaces/:slug/views` | token | view:manage |
| DELETE | `/api/workspaces/:slug/views/:id` | token | view:manage |
| GET | `/api/workspaces/:slug/views/:id` | token | views:read |
| PATCH | `/api/workspaces/:slug/views/:id` | token | view:manage |
| GET | `/api/workspaces/:slug/voice` | session only | Assistant conversations, skills, tools and research run with the signed-in user's context and secrets. |
| PUT | `/api/workspaces/:slug/voice` | session only | workspace owner |
| GET | `/api/workspaces/:slug/webhooks` | token | workspace:manage |
| POST | `/api/workspaces/:slug/webhooks` | token | workspace:manage |
| DELETE | `/api/workspaces/:slug/webhooks/:id` | token | workspace:manage |
| PATCH | `/api/workspaces/:slug/webhooks/:id` | token | workspace:manage |
| GET | `/api/workspaces/:slug/wiki/references` | token | wiki:read |
| POST | `/api/workspaces/:slug/wiki/references/resolve` | token | wiki:read |
| PUT | `/api/workspaces/:slug/workflow-defaults` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/workflows` | token | workflows:read |
| POST | `/api/workspaces/:slug/workflows` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| DELETE | `/api/workspaces/:slug/workflows/:workflowId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/workflows/:workflowId` | token | workflows:read |
| PUT | `/api/workspaces/:slug/workflows/:workflowId` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/workflows/:workflowId/archive` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/workflows/:workflowId/audit` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/workflows/:workflowId/duplicate` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| POST | `/api/workspaces/:slug/workflows/validate` | session only | Agent profiles, workflows and memory are authored with AI and audited as person decisions. |
| GET | `/api/workspaces/:slug/ws` | session only | The realtime WebSocket connection carries live assistant events for a browser session. |
| POST | `/api/workspaces/import` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| POST | `/api/workspaces/import/preview` | session only | Workspace creation, import and invite acceptance bind the signed-in account and stay interactive. |
| GET | `/health` | public |  |
| POST | `/mcp` | token only | the caller's own token |
| GET | `/ready` | public |  |

<!-- token-matrix:end -->



## Connection links for kuayle_cli

`kuayle auth connect <link>` configures a CLI profile without copying a token.
This section is the specification; link creation is session-only.

- **Format:** `{FRONTEND_URL}/cli/connect#code=kuayle_cl_<40 chars>&workspace=<slug>`.
  The code lives in the URL fragment, so a browser never sends it to a server;
  the CLI posts it in a request body.
- **Storage:** only the SHA-256 of the code (`domain.HashToken`) and an
  8-character display prefix are stored.
- **TTL:** `ttl_minutes` defaults to 15; the allowed range is 5–1440 minutes.
- **Uses:** `max_uses` defaults to 1 (single-use) and is capped at 10.
- **Redemption:** `POST /api/cli/connect` is public and rate limited. Its body
  is `{code, client_name?}`; the code is never a path or query parameter. It
  mints a new personal access token named `CLI (link <prefix>)`, with the
  scopes, workspace slugs and token expiry fixed when the link was created.
  It re-checks current workspace membership, returns the plaintext token once,
  and sets `Cache-Control: no-store`.
- **Errors:** an unknown code gets `401 LINK_INVALID`; a known code gets
  `410` with `LINK_EXPIRED`, `LINK_USED` or `LINK_REVOKED`.
- **Revocation:** revoking a link blocks future redemptions only. Tokens
  already minted are ordinary tokens revoked from the token list. A password
  reset revokes the user's open links in the same transaction as its token
  revocation.
- **Why:** token plaintext is never stored, so a link cannot carry an existing
  token; it carries a one-time secret that mints one.
- **Failure handling:** the CLI snapshots the target profile, redeems the
  link, verifies the token with `GET /api/auth/me`, then writes credentials
  and config. On any failure it restores the snapshot and best-effort revokes
  the new token with `DELETE /api/tokens/current`. The token and code are
  never printed.

## Secret redaction

- `domain.RedactSecrets` masks `kuayle_pat_…` and `kuayle_cl_…` values down to
  their prefix in any log message.
- Request logging uses `c.Path()` (the route template) so `/api/invite/:token`,
  `/api/public/share/:token`, `/api/public/wiki/:token` and
  `/api/public/assets/:token` do not put secrets in logs.
- Connection-link codes travel in request bodies, never in server-visible URLs.
- The CLI applies an equivalent `redact()` to every error and message it
  prints.
