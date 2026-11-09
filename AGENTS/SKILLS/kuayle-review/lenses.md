# Review lenses

The checks a [Kuayle peer review](SKILL.md) applies, one section per lens. Each
item is a question to answer against the code, followed where useful by the place
in Kuayle where it usually goes wrong. Paths are relative to this file. A "no"
or "not sure" is a candidate finding to verify, not a finding yet.

## 1. Security and authorization

Authorization lives in the backend. UI visibility, hidden buttons and client
checks are never a boundary.

- **Every entry point checks access.** For each route in
  [routes.go](../../../BE/cmd/server/routes.go), is there middleware or a service
  check for the actor, the workspace and the role? Trace through
  [middleware](../../../BE/internal/middleware) and the
  [access context](../../../BE/internal/domain/access.go). Check owner, admin,
  member, guest and outsider; private teams; public shares; personal access
  tokens; Telegram-bound accounts.
- **Every referenced id is scoped in SQL.** Each user, issue, team, label,
  cycle, status, task, machine, model server or review id in a query must be
  constrained to the request workspace (and team, for private teams) in the
  `WHERE` clause, not only the target row. Look for ids taken from the request
  body and trusted.
- **Reads and writes both.** A list endpoint that filters correctly but a
  get/update/delete by id that doesn't is the classic leak. So is a realtime or
  notification event broadcast to a wider audience than the data's.
- **Stale authorization.** Long operations (agent runs, reviews, merges,
  releases, follow-ups) must re-check permission when they act, not only when
  queued. Approvals must be decided by someone who may still decide
  ([assistant approvals](../../../BE/internal/service/assistant_approvals.go)).
- **Assistant and agents can't escalate.** Tool calls the assistant makes must go
  through the same service checks as the user
  ([assistant tools](../../../BE/internal/service/assistant_tools.go),
  [guard](../../../BE/internal/service/assistant_guard.go)). Text from issues,
  comments, reviews, PRs, web pages and agent output that is fed to another agent
  is untrusted: is it fenced and labelled as data, never as instructions? Can it
  request a tool that changes something without the user's approval?
- **Secrets.** Credentials (sign-ins, model server keys, GitHub tokens, Docker
  TLS keys, machine tokens) are encrypted at rest, decrypted only where used,
  redacted from logs, run output, PR bodies and errors, and never put in argv,
  URLs or query strings. Check the [secrets section](../../../TECHNICAL.md#secrets)
  and that environment injection into containers matches what each service needs
  (a terminal should not get secrets it never uses).
- **Outbound requests.** Any request to a user-configured URL dials through
  `validate.PublicAddressControl` in [pkg/validate](../../../BE/pkg/validate)
  and doesn't follow redirects. Look for new HTTP clients that skip it (webhooks,
  model servers, search, image fetches, release checks).
- **Inbound webhooks and callbacks.** Signature verified before parsing, replay
  protected, delivery idempotent, payload sizes bounded.
- **Input handling.** Size limits on every text field, valid UTF-8, sanitized
  HTML and Markdown ([pkg/sanitize](../../../BE/pkg/sanitize)), no shell command
  built from strings, file paths confined to their root, uploads type- and
  size-checked.
- **Machines and containers.** Network isolation and egress rules, PID and
  memory limits, sudo and Docker access only as the policy allows, no host paths
  mounted that shouldn't be, credentials scoped to the run. See
  [TECHNICAL.md](../../../TECHNICAL.md) sections on isolation, sudo, the machine
  Docker daemon and security limitations.
- **Supply chain.** Images and tools pinned to exact versions; Trivy exceptions in
  [.trivyignore.yaml](../../../.trivyignore.yaml) narrow, justified and
  expiring; no new dependency without a reason; lockfiles changed only on purpose.
- **Rate and abuse limits** on login, invites, uploads, assistant calls and
  anything that costs money (model calls, machines, CI).

## 2. Correctness and reliability

- **State machines.** For each stateful entity (agent task, machine, review,
  merge, release, conversation attempt) list the states and transitions. Is every
  transition guarded against the wrong current state? Can any state be entered
  and never left (stuck "running", "publishing", "awaiting_review")? Is a failure
  visible to the user rather than silently dropped?
- **Leases, fences and idempotency.** Workers that claim work must fence writes
  with the lease and generation; request ids must make retries return the same
  result; replays must not run an action twice. Follow the
  [agent task contract](../../../BE/docs/agent-tasks.md) and the
  [conversation contract](../../../BE/docs/conversations.md).
- **Transactions.** Related writes in one transaction; no external side effect
  (GitHub call, notification, realtime event, container action) published before
  the write that records it commits; locks taken in a consistent order.
- **Restarts and partial failure.** What happens if the backend, the machine
  manager or a container restarts mid-operation, or the network drops between
  two steps? Does a reconcile loop recover it, or does the work hang? Is there a
  timeout on every wait?
- **Configuration that changes while running.** When a setting, default, model
  server, sign-in or policy changes, do running machines, terminals and tasks pick
  it up, or do they keep a stale copy baked in at creation? Is that documented?
- **Errors.** Every error is either handled, returned with context, or logged
  once at the right level. No `err` ignored; no error mapped to a success status;
  no user-facing message that leaks internals or secrets.
- **Limits and truncation.** Text cut to a byte limit must cut on a UTF-8
  boundary and must not silently lose the part that matters (findings, the end of
  a log, the failing check). Check that a limit in one layer matches the
  database constraint in the next.
- **Time and concurrency.** Time zones, clock skew, expiry checks, races between
  a user action and a background loop, double-click and double-submit.
- **Migrations.** Next number, up and down, no rewrite of a shipped migration,
  safe on a large existing table (locks, backfills), and tested on a disposable
  database with representative data.

## 3. Architecture and flows

- **Boundaries.** HTTP binding in handlers, decisions in services, SQL in
  repositories, shared types in domain, as the
  [backend skill](../kuayle-backend/SKILL.md) describes. Look for SQL in
  services, business rules in handlers, UI deciding what the API should.
- **One source of truth.** Each fact (a default, a limit, a state list, a
  permission rule, a prompt section) defined once. Duplicates that have already
  drifted are a finding; duplicates that will drift are a maintainability note.
- **End-to-end flows hold together.** For each traced flow, do both ends agree on
  the contract: field names, optional fields, error codes, event names, ordering?
  Do the UI API modules in [UI/src/lib/api](../../../UI/src/lib/api) match the
  handlers?
- **Coupling.** Features reaching into each other's internals, packages importing
  in the wrong direction, a "util" that knows about one feature, a god service.
- **Extension points.** Where the product is going (custom workflows, agent
  profiles, skills, more harnesses, more channels), does the design leave a clean
  seam or force a rewrite? Flag the specific spot, not "consider abstracting".
- **Docs match the system.** [TECHNICAL.md](../../../TECHNICAL.md),
  [BE/docs](../../../BE/docs), the README, [docs](../../../docs/README.md) and the agent harness describe what the
  code actually does. Outdated docs mislead agents and are a finding.

## 4. Maintainability and refactoring

Recommend a refactor only when it removes a real cost: a bug class, drift,
repeated effort, or code nobody can safely change. Keep it simple: the smallest
change that fixes the cause, in the project's existing style.

- **Duplication** that has drifted or will: copy-pasted SQL, parallel switch
  statements over the same states, the same truncation or validation written
  several ways. Point to the helper to keep.
- **Dead and unreachable code**: unused functions, flags, routes, components,
  messages, migrations-era leftovers, feature switches that are always on.
- **Complexity**: functions doing several jobs, deep nesting, boolean parameters
  that change behavior, long parameter lists, unclear ownership of state.
- **Naming and comments**: names say what a thing is in product terms; comments
  explain why, not what; no stale comments describing old behavior.
- **Consistency with neighbors**: new code reads like the code around it (error
  style, logging, test layout, Svelte patterns). Inconsistency for no reason is a
  finding only when it causes confusion or bugs.
- **Safe to change**: is the code covered by tests that would catch a mistake in
  a refactor? If not, the refactor recommendation must include those tests first.

## 5. UI

Measured against the [frontend skill](../kuayle-frontend/SKILL.md).

- **shadcn-svelte foundation.** Controls come from installed primitives or
  project components; no new handcrafted buttons, inputs, selects or dialogs.
  Run the [control audit](../../../UI/scripts/audit-controls.mjs) and check
  [documented exceptions](../../../docs/kua-62-controls.md).
- **States.** Loading, empty, error, denied, partial and success states exist and
  look intentional. Failed items stay visible with their error; nothing
  disappears because it failed.
- **Responsive.** Works at 320px phone width with a 16px gutter and no horizontal
  scroll; tables and panels adapt; touch targets are usable.
- **Themes.** Light and dark both correct; semantic tokens, no hardcoded colors;
  hover and focus visible in both.
- **Accessibility.** Labels on every input, names on icon buttons, keyboard
  reachable and operable, visible focus, Escape and focus return in dialogs,
  `aria-expanded` and live regions where state changes, no nested interactive
  elements, sufficient contrast.
- **Svelte 5 practice.** Runes used as in neighboring code; no `$effect` that
  merely copies derived state; no stores or global state where props work.
- **Translations.** Every user-facing string goes through the
  [message catalogs](../../../UI/messages) in all four languages (en, it, zh-CN,
  zh-TW); no hardcoded copy; no generated Paraglide files edited by hand.
- **Visual verification.** Judge layouts from screenshots taken with headless
  Playwright at desktop and phone widths in both themes, not from markup alone.

## 6. UX

Kuayle is agent-first and chat-first: the person describes what they want, agents
do the work, and the person stays in control of anything that changes the world.

- **Discoverability.** Can a user find the action they need at the moment they
  need it? A key step that exists only in a menu or a chat command nobody knows
  about is a finding (for example a review that never starts because "Request
  review" is hidden).
- **Clear status.** At any time the user can tell what is happening, what is
  waiting on them, and what failed and why, in plain words. Statuses use the
  product's vocabulary ("Iteration 2", "waiting for review") rather than internal
  state names.
- **Errors that help.** Every failure says what went wrong and what to do next,
  without stack traces or internal ids, and offers the retry or fix in place.
- **Control over consequential actions.** Merges, releases, deletes, upgrades and
  anything that costs money ask for confirmation in the product's own approval
  flow, show exactly what will happen, and can't be triggered by injected text.
- **Configuration from the UI.** Instance and workspace settings an admin needs
  are editable in the UI with sensible defaults, not only through `.env` or the
  database.
- **Noise.** History compressed, long output collapsed, raw logs one toggle
  away, repeated notifications merged. The important thing is the first thing
  seen.
- **Copy.** Short, specific, plain-language text; consistent terms across the
  app; no jargon the user didn't introduce.
- **Flows walked end to end.** Walk the main journeys with Playwright: sign in,
  create an issue, delegate to an agent, follow its iterations, review, merge,
  release; set up a machine; configure agents and sign-ins. Note every dead end,
  confusing step or missing feedback.

## 7. Performance and cost

- **Queries.** N+1 loops, missing indexes for new filters and sorts, unbounded
  `SELECT` without `LIMIT`, large rows fetched for a count.
- **Polling and loops.** Background loops with sane intervals and backoff; UI
  polling replaced by realtime events where they exist; no tight retry loops.
- **Payloads and bundles.** Large responses paginated; heavy UI dependencies
  loaded lazily; images sized.
- **Machines and models.** Containers and images cleaned up; resources released
  on failure; model calls (assistant, agents, reviews) not repeated needlessly,
  since they cost money or subscription quota.
- **CI time.** Slow or redundant jobs, missed caching, work that the
  [change-impact map](../../../scripts/ci/README.md) could skip.

## 8. Tests and CI

Measured against the [testing skill](../kuayle-testing/SKILL.md).

- **Meaningful tests.** Tests assert observable outcomes, not source text or
  markup details. Each behavior change has a test that fails without it.
- **Coverage of risk.** Authorization paths (owner/member/guest/outsider, private
  teams), failure and retry paths, restarts, migrations and limits are tested,
  not only the happy path. Integration tests that skip without a database are not
  evidence.
- **Flaky tests.** Tests that pass on re-run are findings with their failure
  output: name the race or the pixel or timing assumption and the deterministic
  fix (wait on state, not time; compare with tolerance where layout rounds).
- **CI gates.** Selected jobs match the change; the
  [change-impact map](../../../scripts/ci/README.md) and its scenario tests
  stay correct; result gates can't pass with a skipped required job; workflows
  in [.github/workflows](../../../.github/workflows) pin actions.

## 9. Operations and self-hosting

- **Upgrades.** A release upgrades cleanly from the previous one through the
  [release updater](../../../selfhosting/RELEASE_UPDATER.md): migrations apply,
  services restart healthy, running agent tasks survive or are recovered, old
  images are cleaned up, disk doesn't fill.
- **Rollback.** What happens if an upgrade must be rolled back after a migration?
  Is that documented?
- **Observability.** Operations log enough to diagnose a failure (ids, operation,
  error) without secrets; failures surface in the product, not only in logs.
- **Defaults.** A fresh self-hosted install works with documented defaults; new
  settings have safe defaults and appear in [.env.example](../../../.env.example)
  only when they truly belong to the operator.
- **Harness freshness.** When paths, commands or workflows changed, the
  [agent harness](../../../AGENTS.md) and its skills say so; run
  `node AGENTS/TOOLS/check-harness.mjs`.
