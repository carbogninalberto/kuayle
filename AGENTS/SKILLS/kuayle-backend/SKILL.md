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
- Add the next numbered up/down migration under [migrations](../../../BE/migrations).
  Do not rewrite an already shipped migration. Inspect existing migration tests
  and verify upgrades on a disposable database with representative existing data.
  Document irreversible data effects or rollback limitations.
- When changing contracts, update relevant [UI API modules](../../../UI/src/lib/api),
  fixtures and documentation. Inspect history, notifications and realtime event
  publication for the affected service; avoid publishing effects before persistence
  succeeds. Preserve existing sanitization and asset visibility rules.
- Use the toolchain declared by [go.mod](../../../BE/go.mod), format changed Go
  files with `gofmt`, and run the applicable checks. Do not treat skipped integration
  tests as evidence of persistence correctness or use production data for testing.

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
