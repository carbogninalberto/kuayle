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
| KS-08 | Medium | Deferred (product decision) | Workspace model server keys are readable inside every member's Dev Machine | `BE/internal/machine/manager.go` (`modelServerSecrets`) |
| KS-09 | Low | Deferred | `issue:delete_own` PATs of owners/admins delete any issue | `BE/internal/handler/issue.go:348`, `:405` |
| KS-10 | Low | Deferred | Workspace WebSocket accepts any Origin | `BE/internal/handler/ws.go:26` |
| KS-11 | Low | Deferred | SVG uploads render inline from the API origin | `BE/internal/handler/upload.go:36` |
| KS-12 | Low | Deferred | Per-email login lockout (proxy-dependent client IPs fixed by KUA-92) | `BE/internal/middleware/loginthrottle.go:51` |
| KS-13 | Low | Deferred (product decision) | Existing users can be added to workspaces without consent; email enumeration | `BE/internal/service/workspace.go:198` |
| KS-14 | Low | Deferred (latent) | Outgoing webhook delivery would be SSRF-prone if enabled | `BE/internal/service/webhook.go:112` |
| KS-15 | Low | Deferred | Role downgrades leave Dev Machine side effects available | `BE/internal/service/dev_machine.go:642` |
| KS-16 | Low | Fixed (see November 2026) | Collector redaction omits the workspace model credential | `BE/internal/service/dev_machine.go:2324` |
| KS-17 | Low | Deferred (documented limitation) | Browser container compromise reaches unauthenticated IDE/terminal ports | `devmachine/browser/start.sh:30` |
| KS-18 | Low | Deferred (trust assumption) | Admin-built environment images run with members' secrets | Dev Machine environments |
| KS-19 | Low | Deferred | CI: tag-pinned third-party actions and release tag in a `run:` line | `.github/workflows/build.yml:49` |
| KS-20 | Low | Deferred | Images running as root, tag-pinned bases, SPA without CSP | `BE/Dockerfile.updater`, `UI/Dockerfile`, `Dockerfile`, `UI/Caddyfile:4` |
| KS-21 | Low | Partly fixed (see November 2026) | No password change/reset or session revocation | `BE/internal/service/auth.go` |

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

- **KS-08 (Medium, product decision).** Workspace model server keys are
  delivered into developer and agent containers, as TECHNICAL.md describes, so
  any member who can create a machine can read `KUAYLE_MODEL_API_KEY` from a
  terminal or have an agent encode it. The API never returns the keys, but
  members effectively possess them. Closing this requires a credential proxy
  that keeps keys outside member-controlled processes; until then, treat model
  server keys as shared with every member allowed to create Dev Machines.
  TECHNICAL.md states this and the settings form says so next to the key.
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
  everyone (account lockout DoS). Rate limits key on `c.RealIP()`; since KUA-92
  the API and gateway configure an explicit `IPExtractor` (`internal/clientip`)
  that trusts `X-Forwarded-For` only from loopback/private proxy hops, so a
  directly exposed API can no longer be bypassed with forged headers.
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
  restrictions apply before handlers. Saved-view create, update and delete
  require `view:manage`, which guests do not hold.
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
  A password reset deletes the user's refresh tokens and revokes their
  personal access tokens in the same transaction.
- **Invites and shares.** Invite tokens and PATs are stored as SHA-256 hashes;
  invite links grant only member/guest and never change an existing role.
  A link works only while its creator is a member whose role holds
  `member:invite`; removing or demoting the creator revokes their links.
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

---

# Kuayle security audit — November 2026

Audited commit: `4e62f2ed` (`v0.2.24`). Fixes were merged to `main` as one pull
request per step; line references point at the audited commit. This section
continues the finding numbers of the October audit above.

## Scope and method

The same scope as October, with most attention on what changed since `v0.1.31`:
agent tasks, automations and live messages, the assistant and its tools,
conversations, wiki and wiki publishing, voice, Telegram, the GitHub bot,
private teams, invite and shared links.

- Three read-only passes by area (backend authorization and sessions; Dev
  Machines, agents, injection and inbound requests; UI, deployment, CI and
  supply chain). Every candidate was re-verified against the code before it was
  recorded; findings marked Plausible had a complete trace but depended on
  browser or model behaviour that was not run during the audit.
- Each fix started from a regression test that failed for the stated reason.
- Tooling: `govulncheck` (0 reachable vulnerabilities; the unused
  `golang.org/x/crypto/openpgp` advisory remains), `npm audit --omit=dev` for
  UI and WEB (0), `gitleaks` 8.30.1 over every ref (1,297 commits, 102 matches,
  all test fixtures plus the CI test `JWT_SECRET`; no real credential).
- Not covered: image scans outside CI, assistant tool execution in depth, issue
  create/update/bulk paths beyond their diff since October, the machine Docker
  daemon and TLS setup, terminal and diff rendering in the UI.

## Findings

| ID | Severity | Status | Title | Location |
| --- | --- | --- | --- | --- |
| KS-22 | High | Fixed (#175) | Workspace import stored unsanitized HTML that UI helpers parsed with `innerHTML` | `BE/internal/service/workspace_transfer.go:685`, `UI/src/lib/components/layout/CommandPalette.svelte:134` |
| KS-23 | High | Fixed (#173) | Publication ran git with the agent-writable checkout configuration in the developer container | `BE/internal/machine/docker_publication.go:34` |
| KS-24 | High | Fixed (#172), residual noted | Task previews ran agent-written code in the developer container without approval | `BE/internal/repository/agent_task.go:273` |
| KS-25 | Medium | Fixed (#176) | Public share search matched fields the share hides | `BE/internal/service/shared_link.go:467`, `BE/internal/repository/issue.go:206` |
| KS-26 | Medium | Fixed (#176) | Public query parameters replaced a share's stored filters | `BE/internal/service/shared_link.go:458` |
| KS-27 | Medium | Fixed (#177) | Any member's comment started a follow-up run with the task creator's credentials | `BE/internal/repository/agent_signals.go:137` |
| KS-28 | Medium | Fixed (#182) | The assistant could message a running agent as its owner without approval | `BE/internal/service/assistant_live_tools.go:33` |
| KS-29 | Medium | Fixed (#178) | Placeholder secrets from the example files were accepted in production | `BE/internal/config/config.go:327` |
| KS-30 | Low | Fixed (#181) | Invite links kept working after their creator lost access | `BE/internal/repository/workspace_invite_link.go:112` |
| KS-31 | Low | Fixed (#181) | Password reset left personal access tokens valid | `BE/internal/repository/password_reset.go:76` |
| KS-32 | Low | Fixed (#181) | `view:manage` was never enforced | `BE/cmd/server/routes.go:286` |
| KS-33 | Low | Fixed (#189) | Demo-mode machine restriction covered only the Dev Machine routes | `BE/cmd/server/routes.go:425` |
| KS-34 | Low | Fixed (#180), operator action | Any GitHub user who could see an unbound shared-app installation could bind it | `BE/pkg/github/installation_access.go:41` |
| KS-35 | Low | Fixed (#187, remote images in a follow-up PR) | Sanitizers allowed `class` on every tag; remote images in content | `BE/pkg/sanitize/html.go:36`, `UI/src/lib/security/sanitize.ts:12` |
| KS-36 | Low | Fixed (#184) | Release job upgraded agent CLIs to `latest` with no minimum age | `scripts/ci/upgrade-agent-clis.mjs:35` |
| KS-37 | Low | Fixed (#190) | GitHub webhook had no replay protection; the write check accepted read-only collaborators | `BE/internal/handler/github.go:265`, `BE/internal/service/github_bot_abilities.go:155` |
| KS-38 | Low | Fixed (#183) | Root `.gitignore` did not cover `.env.*` | `.gitignore` |

## Fixed findings

### KS-22 — Import stored unsanitized HTML (High)

**Scenario.** Any signed-in user may import a workspace archive, and the
importer stored rich-text columns as sent while every other write path
sanitizes them. Archive members are matched to existing accounts by email
(KS-13), so the importer chose who would see the content. Four UI helpers
turned stored HTML into plain text by assigning it to a detached element's
`innerHTML`, which still loads images and runs their handlers: script ran in
the viewer's session when they opened a sub-issue, searched, opened the issue
picker or copied the AI prompt.

**Fix.** Import applies, per column, the function the owning service applies on
write (issue title and description, comment body, project name and
description, template description, recorded description versions). The UI
helpers were replaced by one helper that parses in an inert document, and
wiki file links and attachment links accept only `http(s)` and `/api/`
addresses. Wiki pages are not part of the archive. Rows imported before the fix
are not rewritten.

**Tests.** `TestPrepareImportedRowStoresWhatTheWritePathsStore`,
`TestWorkspaceImportSanitizesRichText`, `UI/tests/unit/plain-text.test.mjs`,
`UI/tests/e2e/inert-plain-text.test.ts`.

### KS-23 — Publication ran programs named by the checkout (High)

**Scenario.** After a run, Kuayle syncs, commits and pushes the task checkout
from the developer container, which holds developer-scoped secrets, synced
agent sign-ins and optional sudo that a run is never given. Agent runs can
write the checkout's git directory. Only hooks, fsmonitor and signing were
disabled, so a filter driver, a configuration-defined hook or a URL rewrite
planted by a run executed there during publication.

**Fix.** Git no longer runs in the checkout's git directory. Each step builds a
git directory with an empty configuration that borrows only the checkout's
objects, refs and ignore patterns and works against its working tree, with
only the `file` and `https` transports allowed. A repository that relies on a
filter defined only in its local configuration now publishes raw files; see
"What publication runs" in `BE/docs/agent-tasks.md`.

**Tests.** `TestPublishScriptsNeverRunProgramsNamedByTheCheckout`,
`TestSyncScriptFastForwardsToCommitsOthersPushedAndNeverRewrites`,
`TestPublishScriptsConcludeAMergeLeftUncommitted`.

### KS-24 — Task previews started without approval (High)

**Scenario.** A task reaching review queued the project's preview command in
the developer container. What that command runs is decided by files the agent
just wrote, so agent-written code started there before anyone had seen it.

**Fix.** Reaching review starts nothing; a preview starts only on the task
creator's request or the assistant's approved `start_preview`.

**Residual.** A preview the creator starts still runs in the developer
container. Moving previews to a container without developer secrets needs a
third port-routing target and is tracked as follow-up work.

**Tests.** `TestReachingReviewQueuesNoPreview`,
`TestTaskPreviewsStartOnRequestAndStopWithTheTask`.

### KS-25, KS-26 — Public shares (Medium)

**Scenario.** The public share endpoint reused the member search, which matches
descriptions, people's emails and project and cycle text, without escaping
LIKE wildcards and with a total count in the response: hidden values could be
read out through search. Public `status`, `priority` and `label` parameters
replaced the share's stored filters, a view share ignored view filters the
public path did not know, and unreadable filters meant no filter.

**Fix.** A public search mode covers title and identifier, plus description
text only when the share shows descriptions, with literal wildcards. Public
values are intersected with stored ones. Stored filters are all enforced, and a
share whose filters cannot be read or enforced shows nothing.

**Residual.** `GET /api/public/share/:token` still returns the link's stored
filters as saved.

**Tests.** `TestPublicShareFiltersPostgres`, `TestPublicShareIssuesHTTP`.

### KS-27 — Comment follow-ups ran with another member's credentials (Medium)

**Scenario.** A comment by any non-guest member on an issue whose task awaited
review started a follow-up run as the task's creator, with the creator's agent
sign-in, GitHub access and allowed secrets, guarded only by prompt wording.

**Fix.** Only the creator's own comment starts a follow-up. Other comments are
recorded for the creator and use no budget. Commenter and reviewer names are
fenced as untrusted text.

**Tests.** `TestCommentSignalsAutoStartOnlyForTheTasksCreator`,
`TestFollowUpPromptsFenceCommenterAndReviewerNames`.

### KS-28 — `message_task` needed no approval (Medium)

**Scenario.** Text the assistant read from an untrusted source could lead it to
call `message_task`, which delivered the text to the user's running agent as
its owner's instructions.

**Fix.** `message_task` only prepares an approval bound to the task and the
exact text; the task's creator approves it in Kuayle, and a repeated approval
delivers once (migration 000134). Telegram offers no approve button for it.

**Tests.** `TestMessageTaskAsksForApprovalAndQueuesNothing`,
`TestApprovedTaskMessageIsDeliveredExactlyOnce`,
`TestOnlyTheTaskCreatorApprovesATaskMessage`.

### KS-29 — Placeholder secrets accepted (Medium)

**Scenario.** The server only checked the length of `JWT_SECRET`, and the
self-hosting example ships a long placeholder next to `ENVIRONMENT=production`.
A stack started from the unedited example ran with a publicly known signing
key.

**Fix.** Outside `ENVIRONMENT=development` the backend, the machine gateway,
the dev-machines profile and `update.sh` refuse example placeholders and
development defaults, naming the setting and never the value. Development
Compose ports bind to localhost. An instance that still uses a placeholder
does not start after upgrading; see "Secrets must be generated" in
`docs/self-hosting.md`.

**Residual.** The Compose fallback database password used when
`POSTGRES_PASSWORD` is unset is not refused.

**Tests.** `TestLoadRejectsPlaceholderSecretsOutsideDevelopment`,
`TestLoadMachineGatewayRejectsPlaceholderDatabasePasswordInProduction`,
`selfhosting/profile_preflight_test.sh`, `selfhosting/update_test.sh`.

### KS-30, KS-31, KS-32 — Invite links, reset and views (Low)

An invite link now works only while its creator is a member whose role holds
`member:invite`, and removing or demoting the creator revokes their links. A
password reset revokes the user's personal access tokens with their sessions.
Saved-view create, update and delete require `view:manage`, which guests do
not hold. Tests: the invite-link integration tests,
`auth_password_reset_integration_test.go`, `routes_view_test.go`.

### KS-33 — Demo-mode machine rule (Low)

With `PUBLIC_DEMO_MODE` on, approving an assistant delegation or test-running a
custom tool reached a machine for a non-sysadmin, because the rule was route
middleware on one group. The rule is now one service-level check called by
every path that starts work on a machine, with the middleware kept. Tests:
`TestDemoModeRefusesCustomToolTestRun`, `TestDemoModeRefusesAssistantDelegation`.

### KS-34 — Shared GitHub App installation binding (Low)

The October fix (KS-02) accepted any installation GitHub lists for the
installing user, and GitHub lists an installation to every user who can reach
one repository it covers. An ordinary organization member who knew the id of an
unbound installation could bind it to their own workspace. The installing user
must now be the account the app is installed on, or an active owner of that
organization, compared by numeric id; any failed lookup refuses. **Operator
action:** the shared app needs the organization permission "Members:
Read-only"; until it is added and accepted, new organization installations are
refused, while existing connections and personal-account installations are
unaffected. The requirement follows GitHub's documentation and was not tested
against a live app. Tests:
`TestUserControlsInstallationRequiresOwnerOrOrganizationAdmin`,
`TestGlobalInstallationCallbackRequiresOwnerOrOrganizationAdminPostgres`.

### KS-35 — Sanitizer class allowlist and remote images (Low, fixed)

Both sanitizers kept any `class`, so content posted through the API could
reuse the application's utility classes to cover the page with an invisible
link. They now keep only the classes the editor emits, from one contract file
read by the Go tests, the UI unit test and the browser test. Issue template
descriptions are sanitized on create and update. Tests: `pkg/sanitize`
contract tests, `UI/tests/e2e/sanitizer-classes.test.ts`.

Remote images: content could make a reader's browser fetch an image from any
host, telling that host who looked and when. Rendering now leaves such an
image unloaded behind a placeholder naming the host; the reader loads it, or
trusts the host in that browser. Uploaded assets and same-origin images load
as before, and stored content and the backend allowlist are unchanged. This is
a rendering rule in the UI, not a Content-Security-Policy: an `img-src` policy
for the app is still open. Tests: `UI/tests/unit/remote-images.test.mjs`,
`UI/tests/e2e/remote-images.test.ts`.

### KS-36 — Release-time CLI upgrade (Low)

The release job now pins the newest stable agent CLI version published at
least `DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS` ago (default 48) and never lowers a
pin. npm tarball integrity verification in the installer remains open. Tests:
`scripts/ci/upgrade-agent-clis.test.mjs`.

### KS-37 — Webhook replay and the write check (Low)

The webhook handler verified the signature but ignored the delivery id, and a
signature has no expiry. Replaying a captured delivery was not harmless: a
replayed installation removal disconnected a workspace again and a replayed
pull request event restored an old state. Each signed delivery is now claimed
by installation and delivery id before processing (migration 000135, seven-day
retention); a delivery whose processing fails stays redeliverable, and a
request without a usable delivery id is refused. Mention reviews and review
feedback accepted the author associations `MEMBER` and `COLLABORATOR`, which
can be read-only; those authors are now confirmed against the repository
permission GitHub reports (`write`, `maintain` or `admin`), and a failed lookup
starts nothing. No new app permission is needed. **Residual:** a delivery
captured more than seven days earlier can be replayed once; rotating the
webhook secret invalidates captured deliveries. Tests:
`TestGitHubWebhookProcessesADeliveryOncePostgres`,
`TestGitHubWebhookRejectsSignedDeliveryWithoutDeliveryID`,
`TestMentionAndReviewTriggersRequireWritePermissionPostgres`.

### KS-38 — Environment files (Low)

The root `.gitignore` ignores `.env.*` except `.env.example`.

## October findings revisited

- **KS-16** is fixed: collector redaction includes model server keys and run
  secret snapshots.
- **KS-21** is partly fixed: password reset exists and revokes sessions and,
  since KS-31, personal access tokens. In-session password change and "sign
  out everywhere" remain open.
- **KS-13** remains a product decision and was the delivery path for KS-22.
- **KS-08, KS-09, KS-10, KS-11, KS-12, KS-14, KS-17, KS-18, KS-19, KS-20**
  are unchanged. KS-15 was not re-traced.

## Open follow-ups from this audit

- Run task previews in a container without developer secrets (KS-24).
- Add an `img-src` Content-Security-Policy for the app, now that content no longer loads remote images by itself (KS-35).
- Stop returning stored filters from the public share metadata endpoint (KS-25).
- Refuse the Compose fallback database password outside development (KS-29).
- Verify npm tarball integrity in the agent CLI installer (KS-36).
- Tell the task's creator when another member's comment is waiting (KS-27).
