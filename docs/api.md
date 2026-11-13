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

## Known limits

- Webhooks: endpoints can be registered and the delivery format and signature
  are implemented (`WebhookService.Dispatch`), but no issue or comment event
  calls it yet. The document marks deliveries as preview and says so.
- `per_page` above 100 on the issue list is outside the documented contract but
  is not rejected by the server today.
- Authenticated routes have no application-level rate limit.
