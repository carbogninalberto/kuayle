# kuayle-cli

English | [中文](README.zh-CN.md)

CLI and Rust SDK for [kuayle](https://github.com/carbogninalberto/kuayle) — the self-hosted, open-source issue tracker. Built for humans **and** coding agents.

Inspired by [lineark](https://github.com/flipbit03/lineark) (Linear CLI), redesigned for kuayle's REST API and self-hosted deployments.

## Why kuayle-cli?

- **Agent-first, token-cheap.** Point your agent at the CLI; it discovers every command at runtime via `kuayle usage` — a complete command reference in **under 1,000 tokens**. No MCP tool schemas eating your context window.
- **Structured failures.** Semantic exit codes (2=auth, 3=not found, 4=validation, 5=forbidden, 6=rate limited, 7=network) and JSON errors in `--format json` mode, so agents can branch on failure modes.
- **Human-friendly identifiers.** `--team Engineering`, `--assignee "Jane Doe"`, `--labels "Bug,P0"` — no UUIDs required. Names resolve in parallel with a short-lived disk cache.
- **Self-hosted native.** Profiles bind instance URL + workspace + credentials; switch instances with `--profile`.

## Requirements

A kuayle server with Personal Access Token support — see [kuayle#51](https://github.com/carbogninalberto/kuayle/issues/51) / [kuayle#55](https://github.com/carbogninalberto/kuayle/pull/55). Until that lands upstream, run a build from the PR branch.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/uiYzzi/kuayle_cli/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/uiYzzi/kuayle_cli/main/install.ps1 | iex
```

Or via cargo: `cargo install --git https://github.com/uiYzzi/kuayle_cli kuayle`

Shell completions: `kuayle completion <bash|zsh|fish>`

## Authenticate

Create a PAT in kuayle (**Settings → API Tokens**; the `Developer` preset covers full CLI functionality), then:

```sh
kuayle auth login --token kuayle_pat_... --url https://kuayle.example.com --workspace acme
kuayle auth status
```

Credentials live in your OS keychain (file fallback with `0600`). Multiple instances via profiles:

```sh
kuayle auth login --token kuayle_pat_... --url https://kuayle.internal.example.com --profile work
kuayle --profile work issues list
```

For CI/agents, environment variables are all you need:

```sh
export KUAYLE_URL=https://kuayle.example.com KUAYLE_WORKSPACE=acme
kuayle auth login --token "$KUAYLE_TOKEN"
```

## Set up your AI agent

Install the bundled agent skill ([skills](https://skills.sh) compatible):

```sh
npx skills add uiYzzi/kuayle_cli
```

Or add these lines to your agent's context file (`CLAUDE.md`, `AGENTS.md`, system prompt, etc.):

```
We track our tickets and projects in kuayle, a self-hosted issue tracker.
We use the `kuayle` CLI to communicate with it. Use your Bash tool to call the
`kuayle` executable. Run `kuayle usage` to see the full command reference
(global flags, name resolution, exit codes). Prefer `--format json` and branch
on exit codes: 2=re-auth needed, 3=not found, 4=invalid input, 5=forbidden,
6=rate limited, 7=network/server error.
```

## What it can do

| Area | Commands |
|------|----------|
| **Auth** | `auth login`, `auth logout`, `auth status`, `whoami` |
| **Issues** | `list`, `read`, `create`, `update`, `delete`, `batch-update`, `batch-delete`, `subscribe`, `unsubscribe`, `history` |
| **Comments** | `list`, `create`, `resolve`, `reopen` |
| **Relations** | `list`, `create`, `delete` |
| **Labels** | `list`, `create`, `update`, `delete` |
| **Teams** | `list`, `read` · **Statuses** `list` |
| **Projects** | `list`, `read` · **Cycles** `list`, `read`, `burndown`, `velocity` |
| **Templates** | `list`, `read`, `create`, `update`, `delete` |
| **Views / Favorites / Notifications / Members** | `list` (read-only) |
| **Assets** | `read`, `upload`, `download` |
| **Meta** | `usage`, `completion`, `self update` |

Every command supports `--help`. Issues are addressed by identifier (`KUA-123`), never UUIDs.

```sh
kuayle issues create --title "Fix login redirect" --team Engineering \
  --assignee "Jane Doe" --labels "Bug,P0" --priority high
kuayle issues list --status in_progress --format json
kuayle comments create ENG-42 --body "Fixed in 3a7d5ee"
```

Some write operations (cycles, views, favorites, notifications) are currently blocked server-side for PATs and will be enabled as kuayle adds permission codes — see [PR #55](https://github.com/carbogninalberto/kuayle/pull/55).

## SDK: kuayle-sdk

The CLI is a thin shell over `kuayle-sdk` (crate `kuayle-sdk` in this workspace):

```rust
use kuayle_sdk::{Client, types::user::UserResponse};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::new("https://kuayle.example.com".parse()?, "kuayle_pat_...".into());
    let me: UserResponse = client.get("/api/auth/me").await?;
    println!("{me:?}");
    Ok(())
}
```

Features: typed error enum, offset-pagination stream, retry with jittered backoff + `Retry-After` handling, and a type-safe `IssueFilter` builder.

## Development

```sh
cargo xtask check    # fmt + clippy + build + test
cargo test           # unit + wiremock integration
KUAYLE_URL=http://localhost:5173 cargo test --test contract   # live-instance contract tests (skipped when unreachable)
```

## License

MIT
