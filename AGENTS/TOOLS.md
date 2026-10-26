# Tool index

Run commands from the repository root unless a working directory is specified.
The [testing skill](SKILLS/kuayle-testing/SKILL.md) owns the verification matrix.

| Resource | Purpose and contract |
| --- | --- |
| [CI planner and result gates](../scripts/ci/README.md) | Reusable full-PR change detection, image selection and result validation. The linked guide owns inputs, outputs, commands, prerequisites, failure behavior and the impact map. |
| [check-harness.mjs](TOOLS/check-harness.mjs) | Run `node AGENTS/TOOLS/check-harness.mjs` with Node 22. No packages, arguments, credentials, or services required. Locates the checkout relative to itself, reads root instructions and Markdown under `AGENTS/`, and checks local inline link targets, required entrypoints, skill metadata, and index discovery. Prints success or file-specific errors; exits 0/1. Writes nothing. |
| [check-harness.test.mjs](TOOLS/check-harness.test.mjs) | Run `node --test AGENTS/TOOLS/check-harness.test.mjs` to exercise valid and broken fixture trees. Uses Node's standard library and temporary directories, removed after each test. Emits test results; exits nonzero on failure. |
| [UI/package.json](../UI/package.json), [WEB/package.json](../WEB/package.json) | Authoritative npm scripts. Run in the corresponding package after `npm ci`; output/build directories and browser prerequisites are described in the testing skill. Failures propagate through npm. |
| [UI control audit](../UI/scripts/audit-controls.mjs) | Run `node scripts/audit-controls.mjs` from UI after `npm ci`. Uses the Svelte parser to scan both UI and WEB templates, rejects direct application controls outside `components/ui/`, and reports native primitive internals. Writes nothing; exits nonzero on violations or parse errors. Imperative editor/terminal DOM requires manual review; see [KUA-62 audit](../docs/kua-62-controls.md). |
| [WEB SEO validator](../WEB/scripts/validate-seo.mjs) | Reuse via `npm run validate` in WEB; builds first, then validates generated HTML, metadata, images, sitemap and internal links. Requires installed WEB dependencies; exits nonzero for violations. |
| [Release updater](../selfhosting/RELEASE_UPDATER.md) | Opt-in published-release updates for an explicit production Compose project; the linked guide owns setup, prerequisites, backups, failure recovery and validation. `python3 -m unittest discover -s selfhosting/tests -v` runs isolated command-mocked regression tests; also included in `bash selfhosting/update_test.sh`. |
| [Makefile](../Makefile), [scripts](../scripts) | Existing development/service/migration helpers. Read the target/script and [.env.example](../.env.example) before using; some mutate databases or start Docker services. `make test`, `make test-frontend`, and `make lint` currently reference absent UI scripts; use the testing skill's package commands instead. |

The harness checker supports inline relative Markdown links, optional fragments,
and fenced examples. It does not fetch external links, validate anchors, execute
documented commands, parse arbitrary Markdown/YAML, or prove instruction quality.
Use inline links for repository references; review commands and behavior manually.
Keep new tools small, dependency-light, and tied to a demonstrated repeated task.
