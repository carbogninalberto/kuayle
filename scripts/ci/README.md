# Pull-request CI routing

All four PR workflows start and use [CI Impact](../../.github/workflows/ci-impact.yml).
The shared [planner](impact.mjs) is the source of truth for paths, image definitions
and expected jobs. It reads a NUL-delimited, rename-disabled `git diff base...head`
from a full checkout: earlier PR commits, deletions, both rename paths and diffs
larger than GitHub's path-filter limits are included. Base-only changes since the
branch diverged are excluded. Invalid SHAs or a failed comparison select broad
coverage; a failed detection job fails the workflow's result gate.

## Impact map

| Changed inputs | PR verification |
| --- | --- |
| Root documentation, README files, `BE/docs/`, `docs/`, `assets/`, `AGENTS.md`, `AGENTS/` | Agent Harness only, plus lightweight planning/result jobs. Harness runs on every PR because its links reference all repository areas. |
| `BE/` code, migrations, module dependencies | Backend tests and Go security; Docker lifecycle; API and all-in-one readiness on amd64/arm64; gateway/manager builds and scans on both architectures; updater scan. Backend dependencies are conservatively shared across its Go binaries. |
| `BE/**/*_test.go` | Backend tests and Go security. Docker lifecycle is also selected for its integration test file; no production image builds for test-only changes. |
| `UI/` runtime, translations, assets and dependencies | Frontend type/unit/browser checks and npm audit; web and all-in-one readiness on both architectures. `UI/static/releases.json` also selects WEB, which consumes that manifest. |
| `UI/tests/`, `UI/playwright*.ts` | Frontend checks and audit; no production container checks. |
| `WEB/` source (including Markdown content), assets and dependencies | Marketing type/build/SEO/responsive checks. WEB is not the UI application image. |
| Root `Dockerfile` | All-in-one readiness. |
| `UI/Dockerfile`, `UI/Caddyfile` | Web-image readiness. |
| `BE/Dockerfile*` | The corresponding API, gateway, manager or updater image; gateway/manager also select Docker lifecycle. |
| `devmachine/{ide,browser,collector,egress}/` | That image on both architectures, backend script tests and Docker lifecycle. |
| `devmachine/agent/`, shared agent runner | All three provider images on both architectures, backend tests and Docker lifecycle. |
| Shared service entrypoint | IDE, browser, collector and egress images; backend tests and Docker lifecycle. |
| Shared npm security patch/cache tests | IDE and all three provider images; backend tests and Docker lifecycle. |
| `devmachine/security/` | Backend and Docker lifecycle checks for host policy; no independent image build. |
| `selfhosting/` scripts/configuration/smoke tests | Update-recovery and profile-preflight tests; all three application images' readiness on both architectures. |
| Workflows, CI helpers, shared root configuration and unmapped inputs | Broad verification. Workflow/helper edits exercise the routing tests and actionlint as well. |

Mixed changes take the union. Unknown dev-machine shared files select all runtime
images. New paths must be classified by actual consumers before narrowing their
fallback. Docker context membership alone is not proof of a runtime dependency:
Go test files and README files do not enter the built application.

## One verification owner per PR image

- [Release Readiness](../../.github/workflows/release-readiness.yml) owns application
  image builds, packaged-runtime/proxy smoke tests, vulnerability/secret scans,
  and source/image evidence on both native architectures. The duplicate PR
  `Build Images` and `Container Security Scan` jobs are intentionally skipped.
- [Build & Push](../../.github/workflows/build.yml) owns selected dev-machine image
  builds and HIGH/CRITICAL scans on amd64 and arm64, including IDE smoke tests.
  [Security Scan](../../.github/workflows/security.yml) retains the updater image;
  the nine duplicate PR runtime-image scans are omitted there.
- PR Go race tests run in [Tests](../../.github/workflows/test.yml) with database
  coverage. Frontend browser tests build UI before preview. Security Scan retains
  govulncheck, vet, Go build and npm audit, without repeating those PR tests/builds.
- Docker lifecycle still builds the runtime set it needs on its quota-capable host.
  Those images are inputs to a distinct integration environment, not redundant
  substitutes for architecture scans.

Pushes to main, security tag/schedule runs, releases and manual readiness runs
keep their existing full job/image coverage. Release publishing/version updates
remain release-only. The new self-hosting and CI-validation jobs also run with
full Tests coverage on main. PR execution uses read-only contents permission,
no persisted checkout credentials, and no `pull_request_target` or secrets handoff.
Only the release publishing job retains write permissions.

## Result gates and branch protection

Each workflow ends in an `always()` result job. [check-results.mjs](check-results.mjs)
requires successful detection, successful execution of every selected job, and
no failed/cancelled dependency. An unexpected skip or missing selected job fails;
an intentional omission is permitted. Existing fixed job names remain visible
as skipped when not selected; dynamic matrix names exist only for selected images.

At implementation time main had no enforced protection rules. If checks are made
required, require the four stable contexts **Tests CI Result**, **Security CI
Result**, **Build CI Result**, and **Readiness CI Result**, rather than dynamic
matrix entries. No protection settings are changed by this implementation.
Do not introduce workflow-level path filters that leave required checks pending.
See GitHub's [workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
for job conditions, diff limits and skipped workflow behavior.

## Maintain and verify

Run from the root with Node 22 and the repository's Go toolchain:

```sh
node --test scripts/ci/*.test.mjs
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -shellcheck= -pyflakes=
node AGENTS/TOOLS/check-harness.mjs
git diff --check
```

The Node tests create disposable Git histories without changing git identity and
cover docs/harness, BE/UI/WEB, dependencies, shared inputs, workflows, mixed edits,
renames/deletions, earlier commits, base advancement, >300 files and failed gates.
Actionlint validates workflow syntax/expressions; it does not run shell or Python
linters. Existing self-hosting tests run via `bash selfhosting/update_test.sh` and
`bash selfhosting/profile_preflight_test.sh` (Docker Compose and jq required).

To inspect a local plan, set `GITHUB_EVENT_NAME=pull_request`, `PR_BASE_SHA` and
`PR_HEAD_SHA` to full commit SHAs and run `node scripts/ci/impact.mjs`. With no
GitHub output variables it only prints JSON. In CI it additionally appends the
plan to `GITHUB_OUTPUT` and the selection to `GITHUB_STEP_SUMMARY`. It needs git,
Node and the complete history; no API token or third-party package is required.
The gate reads `NEEDS_JSON` and a workflow argument (`tests`, `security`, `build`,
`readiness`), printing success or exiting nonzero with failing job names.

For a new shared input, image, job or Docker COPY dependency, update the planner,
its scenario tests and this map together. Inspect the actual PR plans and job
results on the final commit; distinguish intentionally omitted jobs from verified
coverage. A routing change itself selects broad checks so it cannot validate only
its cheapest scenario. Compare heavyweight execution counts separately from the
small planning/gate overhead; scenario estimates are not observed wall-clock wins.
