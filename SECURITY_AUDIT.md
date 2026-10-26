# Kuayle security audit — October 2026

Audited commit: `76e84b4` (`v0.1.31`, origin/main). Fixes are on branch
`alberto/security-audit`. Line references in findings point at the audited
commit unless stated otherwise.

## Scope

In priority order:

1. Backend authorization and tenant isolation: every route in
   `BE/cmd/server/routes.go`, its permission middleware, workspace/team scoping
   in handlers, services and repositories, IDOR on ID parameters, sysadmin
   checks, public demo mode and personal access token (PAT) scopes.
2. Authentication and sessions: JWT signing, validation and expiry, refresh and
   logout, invites, GitHub App flows, rate limiting, CSRF and Origin checks.
3. Dev Machines: machine-manager container options, egress proxy, gateway
   tickets and sessions, isolation, secret delivery, credential encryption,
   agent configuration files, `devmachine/` shell scripts, collector ingest.
4. Injection and input handling: SQL construction, command/shell injection,
   path traversal, SSRF, deserialization, upload and payload limits.
5. UI and WEB: XSS sinks, token storage, CSP and headers, open redirects.
6. Supply chain and infrastructure: Go and npm dependencies, Dockerfiles,
   `selfhosting/`, GitHub Actions.
7. Secrets hygiene: repository and git history, example configuration, weak
   fallback keys.

## Method

- Narrow `rg` searches per area, then a trace of each candidate from route to
  handler, service, repository SQL and database constraint. A finding is listed
  only when the data flow was confirmed in code; Critical/High/Medium findings
  were additionally reproduced by a regression test that failed before the fix.
- An independent read-only second pass over authorization and Dev Machines ran
  through the `deepseek-parallel-review` launcher (DeepSeek Flash, high effort,
  no network, no edits). Every claim it made was re-verified; three were
  refuted by database triggers or design documentation and are recorded below.
- Tooling: `govulncheck` (Go), `npm audit --omit=dev` (UI, WEB), `gitleaks`
  v8.24.3 over the full git history (redacted output), and the repository's
  backend, frontend and browser suites against a throwaway PostgreSQL 16 and
  Redis 7. Trivy image scans run in the PR's Security and Release Readiness
  workflows.
- No real credentials from `.env`, the environment or the host were used or
  printed. Test databases were disposable containers removed afterwards.

## Findings

| ID | Severity | Status | Title | Location |
| --- | --- | --- | --- | --- |
| KS-01 | High | Fixed | Imported asset content types enable stored XSS on the app origin | `BE/internal/service/workspace_transfer.go:617`, `BE/internal/handler/upload.go:272` |
| KS-02 | High | Fixed | Shared GitHub App installations can be claimed by any workspace | `BE/internal/service/github.go:289` |
| KS-03 | Medium | Fixed | Refresh tokens are accepted as access tokens and survive logout | `BE/pkg/jwt/jwt.go:42`, `BE/internal/middleware/auth.go:69` |
| KS-04 | Medium | Fixed | Workspace AI base URL allows SSRF into the server network | `BE/internal/service/ai_settings.go:125`, `:268` |
| KS-05 | Medium | Fixed | Linked GitHub repositories can be deleted across workspaces | `BE/internal/repository/github.go:103` |
| KS-06 | Medium | Fixed | Issues, projects and templates accept other workspaces' users, labels, cycles and statuses | `BE/internal/repository/issue.go:390`, `:507`, `:602` |
| KS-07 | Medium | Fixed | Unauthenticated GitHub webhook body is read without a limit | `BE/internal/handler/github.go:247` |
| KS-08 | Medium | Deferred (product decision) | Workspace DeepSeek key is readable inside every member's Dev Machine | `BE/internal/machine/manager.go:702` |
| KS-09 | Low | Deferred | `issue:delete_own` PATs of owners/admins delete any issue | `BE/internal/handler/issue.go:348`, `:405` |
| KS-10 | Low | Deferred | Workspace WebSocket accepts any Origin | `BE/internal/handler/ws.go:26` |
| KS-11 | Low | Deferred | SVG uploads render inline from the API origin | `BE/internal/handler/upload.go:36` |
| KS-12 | Low | Deferred | Per-email login lockout and proxy-dependent client IPs | `BE/internal/middleware/loginthrottle.go:51` |
| KS-13 | Low | Deferred (product decision) | Existing users can be added to workspaces without consent; email enumeration | `BE/internal/service/workspace.go:198` |
| KS-14 | Low | Deferred (latent) | Outgoing webhook delivery would be SSRF-prone if enabled | `BE/internal/service/webhook.go:112` |
| KS-15 | Low | Deferred | Role downgrades leave Dev Machine side effects available | `BE/internal/service/dev_machine.go:642` |
| KS-16 | Low | Deferred | Collector redaction omits the workspace model credential | `BE/internal/service/dev_machine.go:2324` |
| KS-17 | Low | Deferred (documented limitation) | Browser container compromise reaches unauthenticated IDE/terminal ports | `devmachine/browser/start.sh:30` |
| KS-18 | Low | Deferred (trust assumption) | Admin-built environment images run with members' secrets | Dev Machine environments |
| KS-19 | Low | Deferred | CI: tag-pinned third-party actions and release tag in a `run:` line | `.github/workflows/build.yml:49` |
| KS-20 | Low | Deferred | Images running as root, tag-pinned bases, SPA without CSP | `BE/Dockerfile.updater`, `UI/Dockerfile`, `Dockerfile`, `UI/Caddyfile:4` |
| KS-21 | Low | Deferred (product gap) | No password change/reset or session revocation | `BE/internal/service/auth.go` |

## Fixed findings

### KS-01 — Imported asset content types enable stored XSS (High)

**Scenario.** Any signed-in user may import a workspace archive
(`POST /api/workspaces/import`, session-only). The importer stored each asset's
`content_type` verbatim from `data.json`, and `streamAsset` served it as the
response `Content-Type`. An attacker imports an asset labelled
`text/javascript`, uploads an ordinary SVG (an allowed type, served inline)
containing `<script href="/api/public/assets/<token>">`, and mints anonymous
asset tokens for both through `POST /issues/:identifier/prompt-assets`. The API
CSP (`script-src 'self'`) allows the same-origin script and `nosniff` accepts the
JavaScript MIME type, so opening the SVG link runs attacker JavaScript on the
application origin. Same-origin requests then carry the victim's `SameSite`
cookies.

**Impact.** Account takeover of any user on the instance who opens a link,
for example creating a personal access token or exporting workspaces.

**Fix.** The upload allowlist moved to `domain.AssetContentTypeExtensions`.
`domain.SafeAssetContentType` serves any other stored type as
`application/octet-stream` with `Content-Disposition: attachment`, which also
covers existing rows. Import normalizes `content_type` with the same function.
Tests: `TestPublicAssetServesUnlistedContentTypesAsDownloads`,
`TestWorkspaceTransferRoundTrip` (scripted asset case).

### KS-02 — Shared GitHub App installations can be claimed (High)

**Scenario.** With a shared ("global"/SaaS) GitHub App, the installation
callback `GET /workspaces/:slug/github/callback?installation_id=N` bound any
installation of the app that was not already bound, after only fetching its
metadata with the app JWT. `installation_id` comes from the browser redirect
and is not authenticated; the `state` value is the public workspace ID. An
attacker who owns any workspace claims another organization's installation
after it is disconnected in Kuayle (the GitHub installation remains), or when
the victim's own callback never completed. Installation IDs can be probed
through the callback's error responses.

**Impact.** The attacker's workspace receives installation tokens for the
victim organization's repositories: it can list private repositories, link
them and clone them into Dev Machines.

**Fix.** In global mode the callback now requires the OAuth `code` GitHub sends
when "Request user authorization (OAuth) during installation" is enabled,
exchanges it with the app's client ID/secret and requires the installation in
`GET /user/installations`. Otherwise it returns 403
`GITHUB_INSTALLATION_UNVERIFIED`. The settings page forwards the code and no
longer mistakes it for a manifest code. Per-workspace (manifest) apps are
unchanged because only their owner's workspace can resolve their installations.
**Operator action:** enable that GitHub App setting and set
`GITHUB_APP_CLIENT_ID`/`GITHUB_APP_CLIENT_SECRET` (documented in
`BE/docs/github-integration.md`). Tests: `TestUserCanAccessInstallation`,
`TestGlobalInstallationCallbackRequiresInstallingUserPostgres`,
`tests/e2e/github-installation.test.ts`.

### KS-03 — Refresh tokens accepted as access tokens (Medium)

**Scenario.** Access (15 min) and refresh (7 days) tokens were HS256 JWTs with
the same secret and identical claims. `Auth` validated any such token, so a
refresh token worked as a bearer or cookie access token. Logout deleted only the
refresh token's database row, which `Auth` never consults.

**Impact.** A captured refresh token grants seven days of API access that
logout cannot end.

**Fix.** Tokens carry a `typ` claim. `ValidateAccessToken` rejects refresh
tokens and accepts untyped legacy tokens only with an access-token lifetime;
`ValidateRefreshToken` rejects access tokens before the database lookup.
Existing sessions continue through refresh. Tests: `TestAuthRejectsRefreshTokens`,
`TestAuthAcceptsLegacyUntypedAccessToken`, `TestRefreshTokens_RejectsAccessToken`.

### KS-04 — AI base URL SSRF (Medium)

**Scenario.** Any registered user can create a workspace, becoming its owner,
and set the AI provider `base_url` to any http(s) URL. `expand-description`
then POSTs from the API server to `{base_url}/chat/completions`. The default
client followed redirects (turning the POST into a GET), and the handler
returned the transport error text (for example
`dial tcp 172.18.0.5:5432: connection refused`).

**Impact.** Requests to internal services and cloud metadata, plus an oracle
for host and port discovery on the server's networks.

**Fix.** A dial-time guard (`validate.PublicAddressControl`) refuses non-public
resolved addresses, so DNS rebinding cannot bypass it. The client never
follows redirects and transport errors are logged server-side only.
Operators that host a provider on a private network opt in with
`AI_ALLOW_PRIVATE_NETWORK=true`. Tests:
`TestAICompletionRefusesPrivateNetworkBaseURL`,
`TestAICompletionPrivateNetworkOptInDoesNotFollowRedirects`.

### KS-05 — Cross-workspace repository unlink (Medium)

**Scenario.** `DELETE /workspaces/:slug/github/repos/:id` ran
`DELETE FROM github_repos WHERE id = $1`. Any workspace owner (every user can
own one) could delete another workspace's linked repository whose UUID they
know, for example a former member. Pull requests, branches and commits cascade.

**Fix.** The delete is scoped to the route workspace and returns 404 when
nothing matched. Test: `TestUnlinkRepoIsWorkspaceScopedPostgres`.

### KS-06 — Cross-workspace references (Medium)

**Scenario.** With `issue:create`/`issue:update`, a member could attach a user,
label, cycle or status from another workspace by UUID: `assignee_id(s)` on
create/update/bulk update, `label_ids`, bulk `cycle_id`/`status_id`, project
`lead_id` and template `assignee_id`. Insertions checked only that the target
issue was visible. Responses and analytics then returned the foreign user's
email and profile and the foreign label, cycle and status metadata, and the
rows broke workspace export validation.

**Fix.** Repository SQL now requires workspace membership for user references
(`optionalWorkspaceMember`, also inside the shared `issueReferencesVisible`
predicate) and same-workspace labels, cycles and statuses. Invalid labels and
assignees are ignored, consistent with the existing best-effort label and
assignee assignment; invalid single references reject the write. Test:
`TestCrossWorkspaceReferencesRejectedPostgres`.

### KS-07 — Unbounded webhook body (Medium)

**Scenario.** `POST /api/github/webhook` is public and called `io.ReadAll` on
the request body before verifying the signature.

**Impact.** Unauthenticated memory exhaustion of the API process.

**Fix.** The body is limited to GitHub's 25 MB payload cap and larger bodies
return 413. Test: `TestGitHubWebhookBoundsUnauthenticatedBody`.

## Deferred findings

- **KS-08 (Medium, product decision).** The workspace DeepSeek key is delivered
  into every developer container, as TECHNICAL.md describes, so any member who
  can create a machine can read `DEEPSEEK_API_KEY` from a terminal or have an
  agent encode it. The API never returns the key, but members effectively
  possess it. Closing this requires a credential proxy that keeps the key
  outside member-controlled processes; until then, treat the key as shared with
  every member allowed to create Dev Machines. TECHNICAL.md now states this.
- **KS-09 (Low).** `issue:delete_own` is the only PAT delete scope, and
  handlers widen it to "any issue" from the role. An owner/admin token with
  issue write access can delete any issue. Decide whether to split the scope
  (`issue:delete`) or rename it to match the behavior.
- **KS-10 (Low).** `websocket.Accept` uses `OriginPatterns: ["*"]`. `SameSite=Strict`
  cookies and `BrowserOrigin` (same-site mode) mitigate it; restricting origins
  to `FRONTEND_URL` would add defense in depth.
- **KS-11 (Low).** SVG uploads are served inline from the API origin. The API
  CSP blocks inline script and, after KS-01, no stored asset can supply script.
  A `Content-Security-Policy: sandbox` on asset responses would make this
  independent of the CSP.
- **KS-12 (Low).** Five failed logins lock an email for 15 minutes for
  everyone (account lockout DoS). Rate limits key on `c.RealIP()` without an `IPExtractor`, which
  is correct only behind the bundled Caddy (which overwrites
  `X-Forwarded-For`); a directly exposed API could be bypassed with forged
  headers.
- **KS-13 (Low, product decision).** `POST /invite` and workspace import add
  existing users by email without consent (import can also create notifications
  for them). "User not found" and import's missing-user list reveal which
  emails have accounts.
- **KS-14 (Low, latent).** `WebhookService.Dispatch` has no callers. If wired,
  its create-time-only URL check, redirect following and default dialer would
  allow DNS-rebinding SSRF; reuse `validate.PublicAddressControl`.
- **KS-15 (Low).** After demotion, a former admin can run custom providers on
  their own machine (where they already execute code), and a former owner
  demoted to guest can call `POST …/activity` and create orphan terminal-session
  rows (`RequireTokenPermission` checks roles only for PATs). Gateway access
  remains denied.
- **KS-16 (Low).** Collector events/logs are redacted with machine secrets but
  not the workspace model credential; logs are readable only by the machine
  creator, who already holds it (KS-08).
- **KS-17 (Low, documented).** Chrome runs with `--no-sandbox` and shares the
  machine network with code-server (`--auth none`) and ttyd. A renderer escape
  would reach the developer container without the gateway. Consider a network
  policy allowing the browser only its VNC/CDP path.
- **KS-18 (Low, trust assumption).** Environment images built by admins run
  members' machines with members' secrets. Document admins as trusted with
  member secrets or verify images.
- **KS-19 (Low).** Third-party actions (`docker/*`, `aquasecurity/trivy-action`)
  are pinned by tag rather than commit SHA, and `build.yml` interpolates
  `github.event.release.tag_name` into a `run:` script with `contents: write`
  (only maintainers can publish releases). Pass it through `env:` and pin SHAs.
- **KS-20 (Low).** The updater, UI and all-in-one images run as root, most base
  images are pinned by tag rather than digest, and the static SPA is served
  without a CSP.
- **KS-21 (Low, product gap).** There is no password change or reset and no
  "sign out everywhere"; refresh tokens are revoked only per device.

## Refuted second-pass claims

- **Dev Machines survive private-team transitions.** Migration 46's trigger
  refuses to make a team private while any machine, environment or scope
  setting exists, and refuses to create those rows in a private workspace,
  under the workspace advisory lock. Machines and private teams cannot coexist,
  so the gateway, scope settings and machine reads cannot leak private-team
  data.
- **Scope-setting enumeration in private mode.** Same trigger: there are no
  scope settings in privacy mode.
- **`issue:delete_own` scope-ceiling bypass.** `issue:delete` is not a PAT
  scope; see KS-09 for the naming issue.

## Checked and found sound

- **Routing.** Every route is registered through `scoped`/`sessionOnly`/
  `ownerOnly` (or annotated public), so PATs are denied by default and scopes
  intersect RBAC. Workspace membership is re-read per request and PAT workspace
  restrictions apply before handlers.
- **Tenant isolation.** Repositories apply `workspaceVisible`/`teamVisible`/
  `projectVisible` predicates from the request access context, which blocks
  IDOR on teams, projects, cycles, statuses, templates, views, webhooks,
  favorites, invite links, shared links, assets, comments and relations even
  where services skip ID checks (for example shared-link update/delete).
- **Sessions.** Cookies are `HttpOnly`, `SameSite=Strict`, and `Secure`
  outside `ENVIRONMENT=development` (`__Host-` in same-site mode). CORS allows only `FRONTEND_URL`; same-site
  mode adds exact-Origin checks for mutations and WebSockets. JWT validation
  pins HMAC. `JWT_SECRET` (≥ 32 chars), `SYSTEM_UPDATER_TOKEN` (≥ 32) and
  `DEV_MACHINE_ENCRYPTION_KEY` (≥ 32) fail closed; there are no weak fallbacks.
- **Invites and shares.** Invite tokens and PATs are stored as SHA-256 hashes;
  invite links grant only member/guest and never change an existing role.
  Share tokens are 256-bit; public queries can only narrow scope.
- **System and demo.** `/system/update*` requires a sysadmin. Demo mode fails
  closed and the gateway rechecks sysadmin status.
- **Dev Machines.** No Docker socket, host mounts, privileged mode or added
  capabilities; read-only root, `no-new-privileges`, optional seccomp/AppArmor,
  memory/CPU/PID limits, per-machine internal networks and volumes. Secrets go
  through tmpfs files rather than container config, with validated names. The
  gateway ticket/session flow uses hashed one-time host-bound tickets, rechecks
  membership and creator on every request, binds terminal tickets to
  `FRONTEND_URL` Origin, session and directory, and strips credentials and
  upstream cookies. The egress proxy dials only validated public resolved
  addresses on 80/443. Collector ingest uses hashed, scoped, expiring
  per-machine tokens bound to the token's own machine and runs. Shell scripts
  quote variables, validate refs and paths, and write Codex/credential files
  under `umask 077`. The machine-manager runs as a non-root, cap-dropped,
  read-only container; the gateway uses a separate database role.
- **Injection.** SQL uses placeholders; dynamic fragments come from allowlists
  (sort columns, analytics dimensions) or typed UUIDs. Import column names are
  intersected with the catalog and references must resolve inside the archive.
  Zip import rejects absolute/`..` paths and bounds compressed, uncompressed and
  per-entry sizes. Uploads are content-sniffed against an extension allowlist
  with random storage keys. The GitHub webhook verifies HMAC with constant-time
  comparison.
- **UI and WEB.** All `{@html}` content passes DOMPurify with an allowlist and
  URL-scheme filtering, or comes from translations/static data. JSON-LD escapes
  `<`. Tokens live only in HttpOnly cookies, and there are no open redirects.
  The app is a static SPA, so no SSR load functions can leak data.
  `UI/yjs-server.cjs` is a development script that is not deployed.
- **Dependencies.** `govulncheck`: no reachable vulnerabilities (one
  module-level advisory in the unused `golang.org/x/crypto/openpgp`, no fix
  available). `npm audit --omit=dev`: 0 vulnerabilities in UI and WEB.
- **Secrets.** gitleaks over full history found 48 matches, all synthetic test
  fixtures (`0123456789abcdef…` sequences) and the CI test `JWT_SECRET`; no real
  credentials. `.env.example` values are marked development-only.
- **Workflows.** Default `contents: read`, no `pull_request_target`, and PR
  checkouts use `persist-credentials: false`.

## Test-suite notes

- `TestHeadlessStartUsesSharedSetupAndWritesReadyMarker` had a data race in
  its own cleanup (reading `cmd.ProcessState` while another goroutine ran
  `Wait`) and failed under `-race` on origin/main. Fixed in the test.
- `TestIssueContentHistoryPostgres` asserts that a lock wait occurs within two
  seconds and was observed to time out once when every package ran
  concurrently against one database. It passes in isolation and was not
  changed.
