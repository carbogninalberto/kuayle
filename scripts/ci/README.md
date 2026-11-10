# Pull-request CI routing

All four PR workflows start and use [CI Impact](../../.github/workflows/ci-impact.yml).
The shared [planner](impact.mjs) is the source of truth for paths, image definitions
and expected jobs. It reads a NUL-delimited, rename-disabled `git diff base...head`
from a full checkout: earlier PR commits, deletions, both rename paths and diffs
larger than GitHub's path-filter limits are included. Base-only changes since the
branch diverged are excluded. Invalid SHAs or a failed comparison select broad
coverage; a failed detection job fails the workflow's result gate. Detection,
workflow-contract validation and result scripts use `github.sha` (the workflow
merge revision on PRs), keeping their catalog aligned with the YAML when main
advances. Product checks and images continue to verify the recorded PR head.

## Impact map

| Changed inputs | PR verification |
| --- | --- |
| Root documentation (`*.md`, `LICENSE`, `NOTICE`), README files anywhere, `BE/docs/`, `docs/`, `assets/`, `selfhosting/*.md`, `AGENTS.md`, `AGENTS/`, `.env.example`, `.gitignore`, and `.github/` files outside `workflows/` and `actions/` (funding, templates, CODEOWNERS) | Agent Harness only, plus lightweight planning/result jobs. Harness runs on every PR because its links reference all repository areas. |
| `BE/` code, migrations, module dependencies | Backend tests and Go security; Docker lifecycle; API and all-in-one readiness on amd64/arm64; gateway/manager builds and scans on both architectures; updater scan. Backend dependencies are conservatively shared across its Go binaries. |
| Backend token/permission definitions | Also Frontend Checks: the UI PAT scope contract test reads both backend files. |
| `BE/**/*_test.go` | Backend tests and Go security. Docker lifecycle is also selected for its integration test file; no production image builds for test-only changes. |
| `UI/` runtime, translations, assets and dependencies | Frontend type/unit/browser checks and npm audit; web and all-in-one readiness on both architectures. WEB fetches the release manifest from main at runtime, so a PR manifest edit does not select WEB. |
| `UI/tests/`, `UI/playwright*.ts` | Frontend checks and audit; no production container checks. |
| `WEB/` source (including Markdown content), assets and dependencies | Marketing type/build/SEO/responsive checks. WEB is not the UI application image. |
| Root `Dockerfile` | All-in-one readiness. |
| `UI/Dockerfile`, `UI/Caddyfile` | Web-image readiness. |
| `BE/Dockerfile*` | The corresponding API, gateway, manager or updater image. Docker lifecycle uses separate runtime images and is not selected for these Dockerfile-only edits. |
| `devmachine/{ide,headless,browser,collector,egress,docker}/` | That image on both architectures, backend script tests and Docker lifecycle. `docker` is the per-machine Docker daemon. |
| `devmachine/agent/`, shared agent runner | All three provider images on both architectures, backend tests and Docker lifecycle. |
| Shared live inbox hook and Claude managed settings | All three provider images; backend tests and Docker lifecycle. |
| Shared agent CLI installer | IDE and headless images; backend tests and Docker lifecycle. |
| Shared Docker CLI installer | IDE, headless and all three provider images; backend tests and Docker lifecycle. |
| Shared scoped startup and agent runner logic | IDE, headless, browser, collector, egress and all three provider images; backend tests and Docker lifecycle. |
| Shared service entrypoint | IDE, headless, browser, collector and egress images; backend tests and Docker lifecycle. |
| Shared terminal, checkout and shell scripts | IDE and headless images; backend tests and Docker lifecycle. |
| Shared npm security patch/cache tests | IDE, headless and all three provider images; backend tests and Docker lifecycle. |
| `devmachine/security/` | Backend and Docker lifecycle checks for host policy; no independent image build. |
| `CLI/` (Rust CLI and SDK, installers, `deny.toml`) | CLI Checks on Linux and Windows (`fmt`, `clippy -D warnings` for source and release features, tests; installer tests on Linux) and CLI Security Scan (`cargo deny`: RustSec advisories, licenses, bans, sources). No image consumes the CLI. `CLI/README.md` follows the documentation rule. |
| `selfhosting/` scripts and configuration | Update-recovery and profile-preflight tests only; no image is built from these files. |
| `selfhosting/image_smoke_test.py` | Also all three application images' readiness, which runs this smoke test. |
| `.github/workflows/`, `.github/actions/`, CI helpers, shared root configuration and unmapped inputs | Broad verification. Workflow/helper edits exercise the routing tests and actionlint as well. |

**Architectures.** PR image jobs (Release Readiness and the dev-machine image
builds) run on amd64 only, unless the diff touches an input that can behave
differently per architecture: any `Dockerfile*`, `BE/go.mod`/`go.sum`,
`UI/package.json`/`package-lock.json`, `devmachine/` (except `devmachine/security/`),
`.trivyignore.yaml`, `.dockerignore` or `selfhosting/image_smoke_test.py`; those, and
broad-coverage changes, add arm64. The planner exposes this as `architectures`.
Pushes to main, releases and manual runs always build and scan both, so arm64
is still verified before anything is published.

Mixed changes take the union. Unknown dev-machine shared files select all runtime
images. New paths must be classified by actual consumers before narrowing their
fallback. Docker context membership alone is not proof of a runtime dependency:
Go test files and README files do not enter the built application.

## One verification owner per PR image

- [Release Readiness](../../.github/workflows/release-readiness.yml) owns application
  image builds, packaged-runtime/proxy smoke tests, vulnerability/secret scans,
  and source/image evidence on the planned native architectures. The duplicate PR
  `Build Images` and `Container Security Scan` jobs are intentionally skipped.
- [Build & Push](../../.github/workflows/build.yml) owns selected dev-machine image
  builds and HIGH/CRITICAL scans on the planned architectures, including IDE and headless
  smoke tests. The shared steps live in the
  [dev-machine-image action](../../.github/actions/dev-machine-image/action.yml).
  [Security Scan](../../.github/workflows/security.yml) retains the updater image;
  the ten duplicate PR runtime-image scans are omitted there.
- PR Go race tests run in [Tests](../../.github/workflows/test.yml) with database
  coverage. Frontend browser tests build UI before preview. Security Scan retains
  govulncheck, vet, Go build and npm audit, without repeating those PR tests/builds.
- Docker lifecycle still builds the runtime set it needs on its quota-capable host.
  Those images are inputs to a distinct integration environment, not redundant
  substitutes for architecture scans.

Pushes to main, security tag/schedule runs, releases and manual readiness runs
keep their existing full job/image coverage. On a release, `release-source`
commits the version bump and refreshed agent CLIs; `build` (application images)
and the unbounded `dev-machine-publish` matrix then both start from that commit
in parallel. `dev-machine-images` is intentionally omitted: `dev-machine-publish`
runs the same native build, smoke tests and scan for every image once, then
pushes that scanned image as `kuayle-<service>:<tag>-<arch>`;
`dev-machine-manifests` waits for both and combines the architectures into
`:<tag>` and `:latest`. Each planner dev image's
`package` is that GHCR repository and must stay `kuayle-` plus its self-hosting
Compose service name, which the release updater pulls (a workflow test checks it). Release publishing/version updates
remain release-only; publishing does not depend on the change-detection job. The new self-hosting and CI-validation jobs also run with
full Tests coverage on main. PR execution uses read-only contents permission,
no persisted checkout credentials, and no `pull_request_target` or secrets handoff.
Only the release jobs (`release-source`, `build`, `dev-machine-publish`, `dev-machine-manifests`) retain write permissions.

## Result gates and branch protection

Each workflow ends in an `always()` result job. [check-results.mjs](check-results.mjs)
requires successful detection, successful execution of every selected job, and
no failed/cancelled dependency. An unexpected skip or missing selected job fails;
an intentional omission is permitted. The full job catalog must match the gate's
dependencies; unknown jobs cannot silently skip. Workflow contract tests also
check catalog/needs completeness and planner selectors. Existing fixed job names remain visible
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
renames/deletions, earlier commits, base advancement, >300 files, CLI output,
unreachable comparison SHAs, cross-package scope contracts and failed gates.
Workflow contract tests inspect our simple job-block/inline-needs conventions;
actionlint supplies full YAML/expression validation.
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

## CLI releases

[CLI Release](../../.github/workflows/cli-release.yml) is separate from the server
release. Pushing a `cli-vX.Y.Z` tag that matches `CLI/Cargo.toml` builds, tests and
packages the CLI for `x86_64`/`aarch64` Linux (musl), `aarch64` macOS and `x86_64`
Windows; the publish job writes `checksums.txt`, re-verifies it with
`sha256sum -c`, runs the installer tests and creates a release marked not
"latest". Running it manually is a dry run that uploads the same files as an
artifact. A `cli-v*` release event selects no jobs in the planner and
`release-source` skips it, so it never bumps the server version or publishes
images. Installers and `kuayle self-update` download only from this repository
and fail closed on a missing or mismatched checksum.

## Release agent CLI refresh

On `release.published`, Build & Push runs `node scripts/ci/upgrade-agent-clis.mjs`
before its existing release commit and image publishing. Node 22 and network
access to npm and GitHub are required; `GH_TOKEN` authenticates only GitHub
metadata requests. The helper picks, for Claude Code, OpenCode, Codex and Qwen
Code (npm) and for Crush, Goose and Kimi (GitHub releases), the newest stable
version published at least `DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS` ago (default 48,
range 0-720, the same setting and default as the runtime refresh), so a
compromised release has time to be withdrawn before it reaches published images.
It updates the shared installer, all provider Dockerfile pins, and Goose/Kimi
SHA-256 pins for amd64 and arm64 from the chosen release's asset metadata.
Missing assets/digests, invalid versions, HTTP failures or unexpected pin layouts
stop the refresh before any files are written. Run it only when an update is
intended: it writes four tracked runtime files, prints the resolved versions as
JSON on stdout and one line per CLI on stderr saying what it chose and why.

Selection rules:

- npm: the full package document. Candidates are stable `X.Y.Z` versions that
  are not deprecated, not above the `latest` dist-tag (other dist-tags are
  pre-release lines) and whose `time` entry is old enough.
- GitHub: the first 100 entries of the releases list. Drafts, prereleases and
  tags that are not `X.Y.Z` or `vX.Y.Z` are skipped; `published_at` must be old
  enough. A list without any stable release fails the refresh.
- A pin is never lowered. If no newer version is old enough, the pin and its
  checksums stay as they are and the output says so, naming the newer version
  that is still inside the waiting period.
- Setting the variable to 0 for the release step turns the waiting period off;
  an invalid value fails the refresh. The workflow does not set it, so releases
  use 48 hours.

npm CLIs are installed by pinned version; the installer does not yet verify
their tarball integrity hashes (Goose and Kimi archives are checksum-verified).

The existing release commit persists these pins on main. Release dev-machine
builds wait for publishing and check out that exact refreshed commit, so their
native architecture builds/scans validate the updated CLIs. PR builds retain the
PR SHA and run despite the intentionally skipped release job. No result gates
or image selections are removed; workflow/helper edits select broad verification.
This refresh follows the existing main-based release workflow and does not move
the published release tag or change running machines. Tests use isolated fixtures
and mocked metadata (`node --test scripts/ci/upgrade-agent-clis.test.mjs`); they
require no network and cover success, repeated runs, the minimum age, skipped
prereleases, kept pins and failure without edits.

Upstream contracts: [npm registry metadata](https://github.com/npm/registry/blob/main/docs/responses/package-metadata.md)
and [GitHub release assets](https://docs.github.com/en/rest/releases/releases).
