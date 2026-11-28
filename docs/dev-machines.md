# Dev Machines (agentic coding)

Dev Machines are an opt-in multi-container subsystem for self-hosted Kuayle, included in releases since v0.1.13. Machines can be created generically, then attach issue worktrees when work starts. Every machine is the same: a developer terminal on the single machine image beside an egress proxy (and, when the workspace allows it, its own Docker daemon), while agent containers are created on demand for runs from that same image. PostgreSQL-backed operations provide durable reconciliation and authenticated wildcard routing.

> See [`TECHNICAL.md`](../TECHNICAL.md) for the full specification, architecture diagrams, schema, and API reference.

## Architecture

```
You (browser)
  │
  ├─ 0123456789abcdef0123-terminal.kuayle-machines.example.net → native xterm over ttyd.v1
  └─ 0123456789abcdef0123-3000.kuayle-machines.example.net → your app's dev server or a task preview (app ports)
        │
        └── All routed through Machine Gateway auth — no public ports, no port management
```

The implementation:

1. **Run one machine image.** The developer terminal runs ttyd on `7681` (`/workspace`, `HOME`, tools, processes and tmux); the egress proxy and on-demand agent, check and preview containers run separately on the isolated bridge network. The image is a regular Ubuntu with common tools, a fixed font set, Node.js, Go, Python, every built-in agent CLI and Playwright with Chromium. The Kuayle UI renders `@xterm/xterm` natively and speaks `ttyd.v1`; it does not expose ttyd's web page.
2. **Support multiple agent providers** — Claude Code, Codex, OpenCode, Pi, Qwen Code, Crush, Goose and Kimi, or admin-configured generic CLIs. The machine image includes pinned versions of these eight CLIs, used both in the terminal and for the bounded autonomous runs Kuayle launches and normalizes. Between releases the manager keeps Claude Code, OpenCode, Codex, Pi and Qwen Code current on a shared read-only volume; machines use those only while they fit the installed release (at least its pinned versions, passing its flag check), which the manager checks on every start, so a deploy never leaves an older CLI ahead of the image's. Upgrades replace only CLIs; workspaces, sessions, sign-ins, notebooks and settings stay ([Agent CLI updates](../TECHNICAL.md#sudo-in-machines)).
3. **Let agents set up and check the project.** Agent containers may use `sudo` to install the system packages a project needs (they last for that container; language dependencies stay on the workspace volume). Agents start the app, exercise it with Playwright and save screenshots with `kuayle-screenshot`, which renders the same way on every machine.
4. **Assign random subdomains** through a separate registrable wildcard domain (`*.kuayle-machines.example.net`) with launch-ticket auth and host-restricted machine session cookies. Use a separate registrable domain for browser site isolation by default. Personal/trusted installations may explicitly enable `DEV_MACHINE_ALLOW_SAME_SITE=true`; see the [same-site setup](self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials).
5. **Authenticate** through a dedicated unprivileged Machine Gateway; the privileged Machine Manager is the only Dev Machines runtime component with Docker socket access. The separately optional system updater also mounts the socket when enabled.
6. **Prepare issue worktrees** from issue, project, team, then workspace repository defaults. One machine has affinity to one repository and can hold several independent issue worktrees from that repository; use a separate machine for another repository or concurrent conflicting work.

## Modes

| Mode              | Who                             | What happens                                                              |
| ----------------- | ------------------------------- | ------------------------------------------------------------------------- |
| **Agent-only**    | A user starts a bounded run      | Agent works autonomously, records a normalized result, and optionally pushes a branch or opens a pull request with a repository-scoped GitHub App token |
| **Human + Agent** | Developer opens an issue workspace | Uses the persistent terminal while launching and monitoring bounded agent runs from Kuayle, and opens the app through app ports or task previews |

## Configuration

Machines are created without requiring a repository, branch, issue, project, or manual TTL. Friendly random names are case-insensitively unique per creator within a workspace and have an availability API. Opening an issue resolves its repository using issue, project, team, then workspace defaults and creates an isolated Git worktree. Workspace policies control concurrency, per-machine disk size, providers, repositories, and custom CLI access. Machines have no lifetime: a booted machine stays up until its user pauses, stops, tears down or deletes it, and agent tasks reuse their creator's running idle machines before creating new ones.

| Size   | CPU     | Memory | PID basis | Disk (hard quota) |
| ------ | ------- | ------ | --------- | ----------------- |
| Small  | 2 cores | 4 GB   | 512       | 20 GB             |
| Medium | 4 cores | 8 GB   | 512       | 50 GB             |
| Large  | 8 cores | 16 GB  | 512       | 100 GB            |

Per-container PID limits are derived from the profile basis; 512 is not one aggregate machine-wide process limit. Per-machine workspace-volume limits use Docker local-volume project quotas. Self-hosted Dev Machines require Docker's data root on XFS mounted with `pquota` or `prjquota`; the manager fails its startup quota probe instead of running an unbounded workspace when the host does not support this boundary.

## Setup

To enable Dev Machines on a self-hosted instance, see [Enabling Dev Machines](self-hosting.md#enabling-dev-machines). For a personal instance on one domain, see [Personal instances](self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials).

## Upgrading to the single machine image

This release removes the code-server IDE, the remote browser service, the activity collector, the provider-specific agent images and Development Environments. Nothing needs to be done by hand; migration 215 and the manager convert existing machines:

- Every machine becomes a terminal machine. A machine that had the IDE keeps its workspace volume; its terminal gets its own container from the single image at the next start (a running machine is converted by the idle image update). The IDE, browser and collector containers are removed then.
- Variables for the IDE apply to the terminal; an existing terminal variable of the same name wins.
- Development Environments, Environment Builders and the environment development default are deleted. The manager removes their snapshot images from the host when it starts. Agents install what a project needs themselves.
- Remove `DEV_MACHINE_IDE_IMAGE`, `DEV_MACHINE_BROWSER_IMAGE`, `DEV_MACHINE_COLLECTOR_IMAGE`, `DEV_MACHINE_CLAUDE_IMAGE`, `DEV_MACHINE_OPENCODE_IMAGE`, `DEV_MACHINE_CODEX_IMAGE` and `DEV_MACHINE_INGEST_URL` from `.env`; they are ignored. All agents use `DEV_MACHINE_HEADLESS_IMAGE`.
- File-change, Git-hook, browser-navigation and shell-command activity events are no longer recorded.

Rolling back the migration restores the schema, not the removed data: machines stay terminal-only and no environment exists.
