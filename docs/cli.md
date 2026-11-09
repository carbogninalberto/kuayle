# Kuayle CLI

`kuayle` is the official command-line interface and Rust SDK for Kuayle, for
people and coding agents. The source is in [`CLI/`](../CLI/).

Originally created by [@uiYzzi](https://github.com/uiYzzi) as
[kuayle_cli](https://github.com/uiYzzi/kuayle_cli); contributed to and now
maintained in the Kuayle repository. The CLI keeps its MIT license
([`CLI/LICENSE`](../CLI/LICENSE)); see [`NOTICE`](../NOTICE).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/carbogninalberto/kuayle/main/CLI/install.sh | sh
```

On Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/carbogninalberto/kuayle/main/CLI/install.ps1 | iex
```

Both installers download only from this repository's `cli-v*` releases, check the
archive against the release's `checksums.txt` (SHA-256) and stop without
installing on a missing entry or a mismatch. Set `KUAYLE_VERSION=cli-vX.Y.Z` to
pin a release and `KUAYLE_INSTALL_DIR` to choose the directory. `kuayle self-update`
does the same checks. Prebuilt targets: Linux x86_64 and arm64 (static, musl),
macOS arm64 and Windows x86_64. Elsewhere, build from source:

```sh
cargo install --git https://github.com/carbogninalberto/kuayle kuayle
```

## Sign in

Create a personal access token in **Settings → API Tokens**. The **Developer**
preset covers the CLI, including wiki editing (`wiki:write`); **Read only** is
enough for reading. Then:

```sh
kuayle auth login --url https://kuayle.example.com --workspace acme --token kuayle_pat_...
kuayle auth status
```

The token is stored in the OS keychain (or a `0600` file). `--workspace` is saved
to the profile; `--workspace` or `KUAYLE_WORKSPACE` overrides it per command.
Commands that need a workspace fail with exit code 4 when none is set.

The CLI only calls the REST API with the token. What it can do is the token's
scopes intersected with your workspace role, exactly as in the web app.

## Use

```sh
kuayle usage                                   # every command, for people and agents
kuayle issues list --team ENG --format json
kuayle issues create --title "Fix login redirect" --team ENG --priority 2
kuayle wiki list --team ENG
kuayle wiki update <page-id> --team ENG --file notes.md
kuayle wiki history <page-id> --team ENG
kuayle wiki restore <page-id> --team ENG --version 3
kuayle registry settings --project Launch --enable   # the registry is off by default
kuayle registry append --project Launch "Deployed v1.2 to staging"
kuayle registry feed --team ENG --limit 3
```

Wiki and registry commands take `--team` (name, key or ID) or `--project`
(name or ID). Page content may be HTML or Markdown. `wiki update` sends the
version it is based on; if someone saved in between, it fails with
`WIKI_VERSION_CONFLICT` instead of overwriting their change.

Output is a table on a terminal and JSON when piped (`--format human|json`).
Exit codes: 2 authentication, 3 not found, 4 invalid input, 5 forbidden,
6 rate limited, 7 network or server error.

Agent tasks, workflows, agent profiles and issue subscriptions are not in the CLI:
their API accepts only signed-in browser sessions, not access tokens.

## Agents

Install the bundled skill with `npx skills add carbogninalberto/kuayle`, or add
to your agent instructions: "Use the `kuayle` CLI to work with our Kuayle
tracker. Run `kuayle usage` for the command reference and prefer `--format json`."

## Develop

From `CLI/`, with the toolchain pinned in `rust-toolchain.toml`:

```sh
cargo fmt --all --check
cargo clippy --all-targets -- -D warnings
cargo clippy --all-targets --features binary-release -- -D warnings
cargo test --features binary-release
sh scripts/install_test.sh
cargo deny check        # needs cargo-deny
```

Live contract tests run every command against a server with seeded data
(`make dev` after `make seed`). Create a token with the Developer preset plus
`issue:delete_own`, optionally a read-only one, then:

```sh
KUAYLE_CONTRACT_URL=http://localhost:8080 KUAYLE_CONTRACT_WORKSPACE=acme \
KUAYLE_CONTRACT_TOKEN=... KUAYLE_CONTRACT_READONLY_TOKEN=... \
cargo test --test contract
```

They create and delete their own issues, labels, templates, pages and an asset,
and append a registry entry (which cannot be deleted), so use a disposable
database. They are skipped when the variables are unset.

Releases: bump `version` in `CLI/Cargo.toml`, merge, then push a tag
`cli-vX.Y.Z`. [CLI Release](../.github/workflows/cli-release.yml) builds every
target, writes and verifies `checksums.txt` and publishes a release that is not
marked "latest". Run it manually for a dry run. See
[CI routing](../scripts/ci/README.md#cli-releases).
