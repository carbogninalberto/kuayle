# Dev Machines (agentic coding)

Dev Machines are an opt-in multi-container subsystem for self-hosted Kuayle, included in releases since v0.1.13. Machines can be created generically, then attach issue worktrees when work starts. Every machine has collector and egress services; it may also include a shared code-server/native-terminal developer container and a browser, while agent containers are created on demand for runs. PostgreSQL-backed operations provide durable reconciliation and authenticated wildcard routing.

> See [`TECHNICAL.md`](../TECHNICAL.md) for the full specification, architecture diagrams, schema, and API reference.

## Architecture

```
You (browser)
  │
  ├─ 0123456789abcdef0123.kuayle-machines.example.net         → code-server
  ├─ 0123456789abcdef0123-terminal.kuayle-machines.example.net → native xterm over ttyd.v1
  ├─ 0123456789abcdef0123-browser.kuayle-machines.example.net → Chrome via KasmVNC (in-browser web navigation)
  └─ 0123456789abcdef0123-3000.kuayle-machines.example.net → your app's dev server (optional app ports)
        │
        └── All routed through Machine Gateway auth — no public ports, no port management
```

The implementation:

1. **Orchestrate cooperating containers** per machine. Collector and egress services are always represented. The optional developer service runs code-server on `8080` and ttyd on `7681` in one container (`/workspace`, `HOME`, tools, processes, and tmux); browser and on-demand agent services run separately on the isolated bridge network. The Kuayle UI renders `@xterm/xterm` natively and speaks `ttyd.v1`; it does not expose ttyd's web page.
2. **Support multiple agent providers** — Claude Code, Codex, OpenCode, Qwen Code, Crush, Goose and Kimi, or admin-configured generic CLIs. The shared developer image includes pinned versions of these seven CLIs for direct interactive terminal use. Kuayle's dashboard launches bounded autonomous runs in separately pinned provider images and normalizes their results.
3. **Assign random subdomains** through a separate registrable wildcard domain (`*.kuayle-machines.example.net`) with launch-ticket auth and host-restricted machine session cookies. Use a separate registrable domain for browser site isolation by default. Personal/trusted installations may explicitly enable `DEV_MACHINE_ALLOW_SAME_SITE=true`; see the [same-site setup](self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials).
4. **Authenticate** through a dedicated unprivileged Machine Gateway; the privileged Machine Manager is the only Dev Machines runtime component with Docker socket access. The separately optional system updater also mounts the socket when enabled.
5. **Prepare issue worktrees** from issue, project, team, then workspace repository/environment defaults. One machine has affinity to one repository and can hold several independent issue worktrees from that repository; use a separate machine for another repository/environment or concurrent conflicting work.
6. **Record best-effort activity** from filesystem, shell, root-checkout Git, browser, agent, gateway, and lifecycle telemetry and attach it to project work

## Modes

| Mode              | Who                             | What happens                                                              |
| ----------------- | ------------------------------- | ------------------------------------------------------------------------- |
| **Agent-only**    | A user starts a bounded run      | Agent works autonomously, records a normalized result, and optionally pushes a branch or opens a pull request with a repository-scoped GitHub App token |
| **Human + Agent** | Developer opens an issue workspace | Uses code-server or the dedicated persistent terminal while launching and monitoring bounded agent runs from Kuayle |

## Configuration

Machines are created without requiring a repository, branch, issue, project, or manual TTL. Friendly random names are case-insensitively unique per creator within a workspace and have an availability API. Opening an issue resolves its repository and environment using issue, project, team, then workspace defaults and creates an isolated Git worktree. Workspace policies control concurrency, per-machine disk size, providers, repositories, and custom CLI access. Machines have no lifetime: a booted machine stays up until its user pauses, stops, tears down or deletes it, and agent tasks reuse their creator's running idle machines before creating new ones. Owners and admins can create a writable Environment Builder, pause or stop it, and save selected tooling/home customization as an immutable local OCI Development Environment image.

| Size   | CPU     | Memory | PID basis | Disk (hard quota) |
| ------ | ------- | ------ | --------- | ----------------- |
| Small  | 2 cores | 4 GB   | 512       | 20 GB             |
| Medium | 4 cores | 8 GB   | 512       | 50 GB             |
| Large  | 8 cores | 16 GB  | 512       | 100 GB            |

Per-container PID limits are derived from the profile basis; 512 is not one aggregate machine-wide process limit. Per-machine workspace-volume limits use Docker local-volume project quotas. Self-hosted Dev Machines require Docker's data root on XFS mounted with `pquota` or `prjquota`; the manager fails its startup quota probe instead of running an unbounded workspace when the host does not support this boundary.

## Setup

To enable Dev Machines on a self-hosted instance, see [Enabling Dev Machines](self-hosting.md#enabling-dev-machines). For a personal instance on one domain, see [Personal instances](self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials).
