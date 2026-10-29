---
name: kuayle-testing
description: Choose meaningful regression tests and run Kuayle frontend, backend, website and harness checks with the actual package and CI commands.
---

# Testing and verification

Define an observable acceptance criterion, add the smallest meaningful failing
test, and run it to establish the failure before implementing behavior. Refactor
after it passes, then broaden to affected suites. Assert outcomes rather than
source text or incidental markup. For docs/config changes, validate references,
syntax and the workflow they describe; do not invent behavioral tests for prose.

## Command matrix

Use Node 22 (a current patch supporting the locked packages), npm lockfiles, and
the Go version from [BE/go.mod](../../../BE/go.mod). Install UI/WEB dependencies
with `npm ci` in each package. Commands below run in the stated directory.

| Scope / cwd | Commands | Prerequisites and coverage |
| --- | --- | --- |
| CI routing / root | `node --test scripts/ci/*.test.mjs` and `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -shellcheck= -pyflakes=` | Full-diff selection and result-gate tests; workflow syntax/expressions. See [CI routing](../../../scripts/ci/README.md). |
| Harness / root | `node --test AGENTS/TOOLS/check-harness.test.mjs` and `node AGENTS/TOOLS/check-harness.mjs` | Node only; fixtures and instruction integrity. Also inspect command accuracy and realistic task routing. |
| UI types / UI | `npm run check` | Compiles Paraglide declarations, syncs SvelteKit, runs svelte-check. |
| UI logic / UI | `npm run test:unit` | Node test runner over `tests/unit/*.test.mjs`; no Vitest/Jest setup. |
| UI browser / UI | `npx playwright install chromium`, then `npm run test:e2e` | Config builds UI and starts its own preview on port 4174; leave it free. Follow existing API/WebSocket mocks. |
| UI build / UI | `npm run build` | Static application output in `build/`; also exercised by browser tests. |
| Backend / BE | `go test ./... -race -count=1`, `go vet ./...`, `go build ./...` | Unit checks work without services, but integration tests may skip. See database requirements below. |
| WEB / WEB | `npm run check`, `npm run validate` | Types, production build, SEO/internal-link validation. Output in `build/`. |
| Voice service / voice | `python -m pytest` (with `pytest` from [requirements-dev.txt](../../../voice/requirements-dev.txt)); `docker build -t kuayle/voice:dev .` | Fake engines, no model downloads; ffmpeg decoding cases skip without ffmpeg. A real smoke test needs the image and network access to the pinned model sources. See [voice transcription](../../../TECHNICAL.md#voice-transcription). |
| WEB browser / WEB | `npx playwright install chromium`, then `npm run test:responsive` | Run `npm run validate` first; preview uses existing build on port 4175. Tests visit sitemap routes at 320px. |

On supported Linux runners, Playwright installation may need `--with-deps` as in
[test CI](../../../.github/workflows/test.yml). Do not assume another worktree's
preview server or build belongs to this task. Coordinate occupied ports instead
of killing unrelated processes. Review relevant desktop/mobile views and keyboard
flows for visual changes; a successful build is not visual verification.

No npm `lint`, `format`, or generic `test` scripts are currently defined in
[UI](../../../UI/package.json) or [WEB](../../../WEB/package.json).
`make test`, `make test-frontend`, and `make lint` therefore are not complete working
check entrypoints. Match adjacent formatting; use `gofmt` on changed Go files.
Do not add a formatter/linter migration just to complete a feature.

## Backend integration coverage

Inspect the affected test's environment guards before claiming it ran. Most DB
tests use `DATABASE_URL`; [issue integration tests](../../../BE/internal/repository/issue_integration_test.go)
use `ISSUE_TEST_DATABASE_URL` plus `ISSUE_TEST_WORKSPACE_ID`, `ISSUE_TEST_TEAM_ID`
and `ISSUE_TEST_IDENTIFIER` identifying an existing fixture. Point database URLs
at disposable test databases and prepare the required fixtures as needed.
CI uses PostgreSQL 16, Redis 7, `REDIS_URL` and a test `JWT_SECRET` (at least 32
characters), applies migrations with `go run ./cmd/server migrate up`, then runs
`go test ./... -race -coverprofile=coverage.out -covermode=atomic -count=1`.
Some migration tests need permission to create isolated databases. Do not source
an arbitrary `.env` and assume its database is safe to modify.

Use nearby service/handler unit tests for business and HTTP contracts. Run real
repository/migration tests for SQL, persistence and authorization races. For a
focused Go regression, use `go test ./internal/service -run '<TestName>' -count=1`
with the actual test name. For a focused browser regression, use
`npm run test:e2e -- tests/e2e/<file>.test.ts` in UI.

Docker lifecycle tests additionally require `KUAYLE_DOCKER_INTEGRATION=1`, built
runtime images and quota-capable storage; follow the Docker Runtime Integration
job in test CI. Do not reproduce its host storage reconfiguration on a developer
machine. Let the dedicated runner exercise that environment.
The machine Docker daemon test (`TestDockerDaemonRunsComposeProjects`) needs
`KUAYLE_DOCKER_DAEMON_INTEGRATION=1`, network access and built images named by
`KUAYLE_TEST_{DOCKER,HEADLESS,EGRESS,COLLECTOR}_IMAGE`; it runs on a developer
machine with `KUAYLE_TEST_DISK_QUOTA_MODE=monitored` (set `DOCKER_HOST` when the
socket is not at `/var/run/docker.sock`).
The machine sudo test (`TestSudoMachinesGetRootWithoutLeavingTheSandbox`) needs
`KUAYLE_SUDO_INTEGRATION=1`, the same quota mode and socket settings, network
access for npm and `KUAYLE_TEST_{HEADLESS,COLLECTOR,EGRESS}_IMAGE`.
The resize and out-of-memory detection test (`TestDockerResizeAndMemoryLimitDetection`)
needs only `KUAYLE_DOCKER_INTEGRATION=1` and a local `alpine:3` image, so it also
runs on a developer machine; CI's Docker job runs only the lifecycle test.

## Completion evidence

Run `git diff --check`. Report commands, results, skipped tests and environment
limitations. For changed scripts, exercise success and failure paths. For harness
changes, walk through a frontend control, a backend regression, and a WEB page
task and verify the linked sources and commands support each decision.

PR checks also include [security](../../../.github/workflows/security.yml),
[image builds](../../../.github/workflows/build.yml), and
[release readiness](../../../.github/workflows/release-readiness.yml). These run
on PRs according to the [change-impact map](../../../scripts/ci/README.md), even
without a release request. Check each workflow's plan and stable result gate on the
final commit. Intentional omissions are not evidence that a suite ran; unexpected
skips, failures and cancellations fail the gate. Update routing and scenario
tests whenever shared inputs, images or verification jobs change.
