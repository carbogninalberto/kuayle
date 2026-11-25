---
name: kuayle-backend
description: Implement Kuayle Go API behavior, authorization, persistence and migrations using its existing handler, service and repository boundaries.
---

# Backend implementation

- Trace the affected endpoint from [route wiring](../../../BE/cmd/server/routes.go)
  through [middleware](../../../BE/internal/middleware), [handlers](../../../BE/internal/handler),
  [DTOs](../../../BE/internal/dto), [services](../../../BE/internal/service), and
  [repositories](../../../BE/internal/repository). Preserve those boundaries:
  HTTP binding/status mapping in handlers, business decisions in services, SQL in
  repositories, shared entities and access context in [domain](../../../BE/internal/domain).
- Use request context, existing response/validation helpers under [pkg](../../../BE/pkg),
  and repository interfaces/test doubles already used by the nearest tests.
  Inspect actual error contracts before changing client-visible statuses or fields.
- Write a failing test for the intended behavior first. Select unit, handler or
  real database coverage through the [testing skill](../kuayle-testing/SKILL.md).
  For access changes cover owner/member/outsider, private teams, workspace scoping,
  and applicable public-share/PAT paths. Check read and write paths; never rely
  solely on a frontend check or an identifier supplied by the caller.
- Constrain every referenced user, label, cycle or status to the request workspace
  in SQL, not only the target row. Requests to user-configured URLs must dial
  through `validate.PublicAddressControl` in [pkg/validate](../../../BE/pkg/validate)
  and not follow redirects. See [SECURITY_AUDIT.md](../../../SECURITY_AUDIT.md).
- Preserve [access context](../../../BE/internal/domain/access.go), transaction
  boundaries and repository visibility predicates. For concurrency-sensitive
  access changes inspect [privacy concurrency tests](../../../BE/internal/repository/privacy_concurrency_test.go).
  Assert persisted results, rollback behavior and forbidden side effects where
  relevant; test stale authorization when changing locking or isolation.
- `issues` and `wiki_pages` are views hiding soft-deleted rows; alter the
  `issue_records`/`wiki_page_records` tables and recreate the view, and name the
  table only to see deleted rows (see [Soft delete and undo](../../../TECHNICAL.md#soft-delete-and-undo)).
- Before choosing a migration number and again before publishing, fetch the
  base with `git fetch origin <base-branch>` and inspect it with
  `git ls-tree --name-only FETCH_HEAD:BE/migrations`. Use the base branch in
  your task's execution contract. If a private repository's fetch is unavailable,
  inspect the base tracking ref Kuayle provided. Kuayle checks the remote base
  during publication. Pick the next number after the highest version on the
  base or your task branch for both files under [migrations](../../../BE/migrations).
  If the base takes your number, renumber the up/down pair, update its
  references and rerun the checks.
  Do not rewrite an already shipped migration. Inspect existing migration tests
  and verify upgrades on a disposable database with representative existing data.
  Document irreversible data effects or rollback limitations.
- For permission changes, consult the [workspace role model](../../../TECHNICAL.md#workspace-roles).
  Keep the role catalog, built-in database grants and PAT scope contract in sync.
- When changing contracts, update relevant [UI API modules](../../../UI/src/lib/api),
  fixtures and documentation. Inspect history, notifications and realtime event
  publication for the affected service; avoid publishing effects before persistence
  succeeds. Preserve existing sanitization and asset visibility rules.
- Routes a personal access token can reach are either published or classified as
  internal in the [OpenAPI registry](../../../BE/internal/openapi/operations.go).
  After changing such a route or a published DTO, regenerate the document and
  review its diff as a public contract change; see the [API guide](../../../docs/api.md).
- Use the toolchain declared by [go.mod](../../../BE/go.mod), format changed Go
  files with `gofmt`, and run the applicable checks. Do not treat skipped integration
  tests as evidence of persistence correctness or use production data for testing.

The [MCP server](../../../docs/mcp.md) exposes API routes as tools. Add a tool as a
row in [tools.go](../../../BE/internal/mcp/tools.go) pointing at a `scoped` route;
never call services from that package, which would bypass the route's checks.

For persistent chat, follow the [conversation contract](../../../BE/docs/conversations.md).
Keep conversation cursors serialized by the conversation transaction, generation
writes fenced by the lease, and reconnects read-only. Conversation audience and
workspace broadcast audience are different authorization boundaries. Issue chats
also retain their original team scope; preserve the fresh checks and lock ordering
documented in that contract across Web, workers and Telegram.
For Telegram changes, preserve the [channel contract](../../../BE/docs/telegram.md):
current allowlist plus verified Kuayle-account binding, atomic update receipts and
conversation writes, owner-only encrypted credentials, and fresh outbound permission
checks. Never log Telegram request URLs, which contain the bot token.

For Slack changes, preserve the [workspace installation contract](../../../BE/docs/slack.md), including session-bound OAuth state, scoped channel mappings, and replay-safe removal.

For sign-in and session work, follow the [passkey contract](../../../BE/docs/passkeys.md):
every sign-in path issues its session through `AuthService.issueSession`, ceremony
challenges are consumed before verification, and a session minted by a passkey ends
with that passkey. Test ceremonies with the software authenticator in
[internal/passkeytest](../../../BE/internal/passkeytest/authenticator.go).

For task/runtime work, follow the [agent task contract](../../../BE/docs/agent-tasks.md).
Keep the [machine-agent instructions](../../../devmachine/common/kuayle-agents.md)
consistent with the turn prompts and publication contract. Preserve native task
ownership, pre-dispatch encrypted credential snapshots, atomic log/activity
persistence and physical cancellation checks.

For periodic delegation, follow the [automation worker and replay contract](../../../BE/docs/agent-tasks.md#automations).
Graph workflows use [stage assistant routing](../../../BE/docs/agent-tasks.md#configurable-workflows).
For pipelines, the [team workflow contract](../../../BE/docs/agent-tasks.md#team-workflows)
covers room permissions, verdicts, grants (a split grant too), waivers, splits and board visibility.
Keep role permissions in [TaskRoomRefusal](../../../BE/internal/domain/task_room.go)
and authenticate the sender in the [room repository](../../../BE/internal/repository/task_room.go).
Represent new conditions as [facts](../../../BE/internal/service/task_facts.go).
Keep error-specific branches out of the [tick](../../../BE/internal/service/team_tick.go)
and exercise [TestTickLiveness](../../../BE/internal/service/team_tick_liveness_test.go)
when changing scheduling or platform actions. For lead reports, cover replay of
the same post under the [notification contract](../../../BE/docs/telegram.md#task-notifications).

Register new backend configuration variables in the [config registry](../../../BE/internal/config/registry.go).
Mark credentials `Secret`. Give a setting a `Runtime` rule only when every reader
resolves it per use through [InstanceConfigService](../../../BE/internal/service/instance_config.go).
A setting the API reads once may get a `Restart` rule only if the API alone reads it, it is not
a secret and not a forbidden kind (`restartForbidden`: credentials, database/Redis, JWT, SYSADMINS,
SSRF guards, internal URLs, variables other services read); it is then edited through
apply-and-restart ([restart-only settings](../../../TECHNICAL.md#restart-only-settings-apply-and-restart)).
See [instance configuration](../../../TECHNICAL.md#instance-configuration) for the settings API
and the [Web Push contract](../../../BE/docs/web-push.md) for notification delivery.
