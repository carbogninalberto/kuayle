# Public API reference (OpenAPI)

Kuayle publishes an OpenAPI 3.1 description of the stable, token-accessible
HTTP API: issues, comments, teams, statuses, cycles, projects, labels, views,
wikis, members and webhooks.

| Where | What |
| --- | --- |
| `GET /api/openapi.json` on any instance | The document for the release that instance runs. Public, no token needed. |
| [kuayle.com/api-reference](https://kuayle.com/api-reference) | The rendered reference for the current release, with the document at `/openapi.json`. |
| [BE/internal/openapi/openapi.json](../BE/internal/openapi/openapi.json) | The checked-in document (version placeholder `0.0.0-dev`). |

The document's `info.description` covers authentication and token scopes,
pagination, the error format, rate limits, webhook payloads and signature
verification. [API tokens](api-tokens.md) records every endpoint and verb a
token can reach, the scope that gates it, and the routes that stay
session-only. This page is for people changing the API.

## How the document is produced

Nothing in the document is written as JSON by hand.

- [operations.go](../BE/internal/openapi/operations.go) lists each stable
  operation: method, path, token scope, summary and the Go types of its request
  and response. It also classifies every other token-accessible route as
  internal, with a reason. Internal routes are not published.
- [schema.go](../BE/internal/openapi/schema.go) turns those Go types into JSON
  Schema by reflection: JSON names, `omitempty`, pointers (nullable) and the
  `validate` rules (required, lengths, ranges, enums, formats).
- [guide.go](../BE/internal/openapi/guide.go) holds the written guide. The
  reference page renders the same sections from the `x-kuayle-guide` extension.
- The server stamps `info.version` with its release when it serves the file.

Regenerate both checked-in copies (backend and marketing site) from `BE/`:

```sh
go run ./cmd/openapi
```

## What fails when the API and the document disagree

| Change | Failing test |
| --- | --- |
| A field, JSON name or validation rule of a published DTO changes | `TestCheckedInDocumentMatchesRoutesAndDTOs` in `BE/internal/openapi`, until the files are regenerated and the contract change is visible in review |
| A token-accessible route is added, removed, moved or given another scope | `TestTokenRoutesMatchThePublicReference` in `BE/cmd/server`: the route must be an operation or an internal rule |
| A documented scope is not what the route's middleware enforces | `TestDocumentedScopesAreEnforced` in `BE/cmd/server` |
| The server's real responses do not match the documented schemas | `TestOpenAPIClientCreatesIssueAndComment` in `BE/cmd/server` (needs `DATABASE_URL`) |
| The marketing site's copy differs from the backend's | `TestCheckedInDocumentMatchesRoutesAndDTOs` |

To publish a new endpoint, register the route with `scoped(...)`, add an
`Operation` for it, regenerate, and review the diff of `openapi.json`. To keep a
token-accessible route out of the reference, add an `InternalRoute` with the
reason. Session-only and owner-only routes reject tokens and need neither.

Responses that are not DTOs (for example `{"status":"deleted"}`) are described
by small types in `operations.go`; keep them equal to what the handler writes.

## Try a generated client

[api-client-smoke](api-client-smoke/smoke.ts) generates TypeScript types from a
running instance with `openapi-typescript`, compiles a small `openapi-fetch`
client against them and uses it to create an issue and a comment:

```sh
cd docs/api-client-smoke
npm ci
KUAYLE_URL=http://localhost:8080 KUAYLE_TOKEN=kuayle_pat_... npm run smoke
```

The token needs `workspaces:read`, `teams:read`, `issue:create`, `issues:read`
and `comments:read`, and the workspace needs one team. Set `KUAYLE_WORKSPACE`
(slug) and `KUAYLE_TEAM` (key) to choose where the issue is created. The script
writes real data, so point it at a development instance.

## Recurring issue templates

`POST /api/workspaces/{slug}/issue-templates` and `PATCH` on a template accept a
`recurrence_rule` object with a five-field `cron` expression and an IANA
`timezone`, which defaults to `UTC`. For example,
`{"cron":"30 9 * * *","timezone":"America/New_York"}` schedules a run at 9:30 a.m.
local time each day. A `PATCH` with `recurrence_rule: null` removes the schedule.

Each API instance runs the scheduler. A scheduled issue uses the template
creator's current access and requires a team. It uses the default issue title
when set, otherwise the template title, followed by the scheduled date in the
configured timezone. At a forward clock change, slots in the gap run together
when the clock resumes. A repeated local time runs at its first occurrence.

Template responses report `next_run_at`, `last_run_at`, and `last_error`.
After downtime, the worker processes the stored due slot and resumes at the
next future run. A creation failure records a reason and advances the schedule.
A rule the worker cannot interpret leaves `next_run_at` empty until the schedule
is edited. Workspace settings show the next run in the browser's timezone and
the last failure reason.

Rolling back migration `000200` removes the scheduler's run history. Generated
issues and the templates' next run times remain in the database.

## Known limits

- Webhooks deliver `issue.created`, `issue.updated`, `issue.deleted`,
  `issue.triaged`, `comment.created`, `comment.updated` (resolving, reopening
  or editing a comment) and `comment.deleted` at least once. Each change is stored in the database,
  one delivery per webhook, and survives restarts; a failure is retried after
  30 s, 2 min, 10 min, 30 min and 2 h, then given up. A delivery that waited a
  day, or that does not fit its workspace's share (1024 pending deliveries,
  8 MiB), its webhook's (256) or the whole store's, is dropped. A webhook that
  fails three times in a row is paused for five minutes; its deliveries wait.
  Receivers should deduplicate on `X-Webhook-Delivery` and not rely on order.
  `GET /webhooks/:id/deliveries` and Settings → Webhooks show recent
  deliveries. Restoring a deleted issue or comment sends no event. Webhooks are off for workspaces
  that have or had a private team: creating or enabling one there answers 403
  `PRIVATE_WORKSPACE_OPERATION_DISABLED`. Other changes (projects, cycles, wiki
  pages), changes the database makes by itself (clearing a deleted parent,
  project or cycle, deleting a team's issues, rewriting identifiers after a
  team key rename) and workspace import do not emit events.
- `per_page` above 100 on the issue list is outside the documented contract but
  is not rejected by the server today.
- Authenticated routes have no application-level rate limit.
