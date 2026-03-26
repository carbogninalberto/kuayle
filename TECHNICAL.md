# Kuayle Dev Machines — Technical Specification

On-demand, single-container development environments with integrated agentic coding, browser access, and automatic work tracking.

## Overview

Kuayle Dev Machines allow users and agents to work inside pre-configured, disposable development environments hosted on a VPS. Each machine runs as a single Docker container with everything needed: an IDE, an agentic coding CLI, a browser, and the project repository — all accessible through auto-generated subdomains.

## Architecture

### System Overview

```
                    *.kuayle.com (wildcard DNS)
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│                   VPS Host                       │
│                                                  │
│  ┌────────────────────────────────┐              │
│  │    Kuayle Application          │              │
│  │                                │              │
│  │  ┌──────────┐  ┌───────────┐  │              │
│  │  │ Auth     │  │ Container │  │              │
│  │  │ Gateway  │  │ Manager   │  │              │
│  │  └────┬─────┘  └─────┬─────┘  │              │
│  └───────┼───────────────┼────────┘              │
│          │               │                       │
│          │     ┌─────────▼──────────┐            │
│          │     │  Docker Engine     │            │
│          │     │                    │            │
│          ▼     │  ┌──────────────┐  │            │
│  ┌─────────┐   │  │ Machine A    │  │            │
│  │ Reverse │◄──┼──│              │  │            │
│  │ Proxy   │   │  │ code-server  │  │            │
│  │ (Caddy/ │   │  │ claude-code  │  │            │
│  │ Traefik)│   │  │ chromium     │  │            │
│  └─────────┘   │  │ repo + deps  │  │            │
│                │  └──────────────┘  │            │
│                │                    │            │
│                │  ┌──────────────┐  │            │
│                │  │ Machine B    │  │            │
│                │  │ ...          │  │            │
│                │  └──────────────┘  │            │
│                └────────────────────┘            │
└──────────────────────────────────────────────────┘
```

### Request Flow

```
User browser
  │
  ├─ abc123.kuayle.com ──────────────────────────────┐
  │                                                   │
  └─ abc123-browser.kuayle.com ───────────────────┐   │
                                                   │   │
                          ┌────────────────────┐   │   │
                          │   Kuayle Auth       │◄──┼───┘
                          │                    │   │
                          │  1. Validate token │   │
                          │  2. Check machine  │   │
                          │     ownership      │   │
                          │  3. Log access      │   │
                          └────────┬───────────┘   │
                                   │               │
                          ┌────────▼───────────┐   │
                          │   Reverse Proxy    │◄──┘
                          │                    │
                          │  Route by subdomain│
                          │  abc123 → :8443    │
                          │  abc123-browser    │
                          │         → :3000    │
                          └────────┬───────────┘
                                   │
                          ┌────────▼───────────┐
                          │   Container        │
                          │                    │
                          │  :8443 code-server │
                          │  :3000 chromium    │
                          │  :5173 dev server  │
                          └────────────────────┘
```

### Container Internals

```
┌─────────────────────────────────────────────┐
│            Single Container                  │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │         Supervisor (s6/runit)         │   │
│  └──────┬──────────┬──────────┬─────────┘   │
│         │          │          │              │
│  ┌──────▼──┐ ┌─────▼────┐ ┌──▼──────────┐  │
│  │ code-   │ │ Chromium  │ │ Dev Server  │  │
│  │ server  │ │ (noVNC)   │ │ (optional)  │  │
│  │ :8443   │ │ :3000     │ │ :5173       │  │
│  │         │ │           │ │             │  │
│  │ ┌─────┐ │ │           │ │             │  │
│  │ │Claude│ │ │           │ │             │  │
│  │ │Code  │ │ │           │ │             │  │
│  │ │CLI   │ │ │           │ │             │  │
│  │ └─────┘ │ │           │ │             │  │
│  └─────────┘ └───────────┘ └─────────────┘  │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │         Shared Filesystem             │   │
│  │                                       │   │
│  │  /workspace/          Project repo    │   │
│  │  /home/coder/.claude/ CLI config      │   │
│  │  /home/coder/.config/ User settings   │   │
│  └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

## Subdomain Routing

Kuayle uses **wildcard DNS** (`*.kuayle.com`) pointing to the VPS. A single reverse proxy routes requests by subdomain to the correct container.

| Subdomain Pattern | Target | Purpose |
|---|---|---|
| `{machine-id}.kuayle.com` | code-server `:8443` | IDE + terminal + Claude Code |
| `{machine-id}-browser.kuayle.com` | Chromium noVNC `:3000` | In-browser Chromium for local navigation |
| `{machine-id}-app.kuayle.com` | Dev server `:5173` | Preview of the running application |

Machine IDs are random, non-guessable strings (e.g., `f8k2m9x3p1`).

**No per-container port management.** The reverse proxy dynamically routes based on subdomain-to-container mapping maintained by Kuayle's container manager.

## Authentication & Authorization

All subdomain requests flow through Kuayle's auth gateway before reaching the container:

```
Request → DNS → Reverse Proxy → Kuayle Auth Middleware → Container
```

1. **Session validation** — The request must carry a valid Kuayle session (cookie or token)
2. **Machine ownership check** — The authenticated user (or agent) must have access to the target machine
3. **Access logging** — Every request is logged with user, machine, timestamp, and path

Agents authenticate using API tokens scoped to specific machines and workspaces.

## Machine Lifecycle

```
  ┌──────────┐     ┌───────────┐     ┌─────────┐     ┌────────────┐
  │ Configure│────▶│  Spawning │────▶│ Running │────▶│ Teardown   │
  └──────────┘     └───────────┘     └─────────┘     └────────────┘
       │                │                 │                 │
  User sets        Container          IDE + Agent      Container
  repo, branch,    pulls image,       accessible,      stopped,
  env vars,        clones repo,       work tracked     snapshot
  tools via UI     installs deps                       saved (opt)
```

### States

| State | Description |
|---|---|
| **Configuring** | User selects repo, branch, env vars, tools, and machine size from Kuayle UI |
| **Spawning** | Container image pulled, repo cloned, dependencies installed, services started |
| **Running** | Machine accessible via subdomains, work is actively tracked |
| **Paused** | Container stopped but preserved, can resume quickly |
| **Teardown** | Container destroyed, optional snapshot saved for future reference |

### Configuration Sources

Machine configuration is resolved in order (later overrides earlier):

1. **Project defaults** — Defined in the Kuayle project settings (default branch, required env vars, base image)
2. **User preferences** — Stored in Kuayle user settings (editor theme, shell, extensions)
3. **Spawn-time overrides** — Set when creating the machine (specific branch, extra env vars, custom tools)

## Activity Tracking

Since all traffic flows through Kuayle's proxy and the container runs under Kuayle's supervision, activity is tracked automatically:

### What Is Tracked

| Activity | Source | Granularity |
|---|---|---|
| File edits | code-server API / filesystem watcher | File path, timestamp, diff size |
| Terminal commands | Shell history / PTY monitoring | Command, exit code, duration |
| Git operations | Git hooks inside container | Commits, pushes, branch changes |
| Browser navigation | Chromium CDP events | URLs visited, page titles |
| Agent actions | Claude Code CLI telemetry | Tool calls, files read/written, decisions |
| Dev server state | Process monitor | Start, stop, errors, port bindings |

### How It Feeds Back Into Kuayle

```
Container Activity
      │
      ▼
┌─────────────────┐     ┌──────────────────┐
│ Activity Stream  │────▶│ Kuayle Engine    │
│                  │     │                  │
│ - file edits     │     │ - Auto-update    │
│ - git commits    │     │   issue status   │
│ - commands run   │     │ - Log time spent │
│ - URLs visited   │     │ - Create commits │
│ - agent actions  │     │   summary        │
└─────────────────┘     │ - Link artifacts  │
                         └──────────────────┘
```

- **Issue status updates** — When an agent commits with an issue reference, the issue status can auto-advance
- **Time tracking** — Machine uptime and active editing time logged against issues
- **Work summaries** — Auto-generated summaries of what was done in a session
- **Artifact linking** — Commits, branches, and PRs created inside the machine are linked to the originating task

## Operating Modes

### Agent-Only Mode

Kuayle assigns a task to a machine. No human interaction required.

```
Kuayle
  │
  ├─ 1. Create machine with task context
  │     (issue description, repo, branch, acceptance criteria)
  │
  ├─ 2. Claude Code starts autonomously
  │     - Reads task context
  │     - Plans implementation
  │     - Writes code
  │     - Runs tests
  │     - Uses Chromium for UI testing if needed
  │
  ├─ 3. Agent pushes results
  │     - Creates branch + commits
  │     - Opens PR (optional)
  │     - Reports status back to Kuayle
  │
  └─ 4. Machine tears down
        - Activity log saved
        - Issue updated with results
```

### Human + Agent Mode

A developer gets a browser link to a full environment.

```
Developer
  │
  ├─ 1. Clicks "Open Machine" in Kuayle UI
  │
  ├─ 2. Gets two browser tabs
  │     - abc123.kuayle.com       → VS Code (code-server)
  │     - abc123-browser.kuayle.com → Chromium
  │
  ├─ 3. Works interactively
  │     - Writes code in the IDE
  │     - Uses Claude Code in the terminal
  │     - Tests in Chromium (same network as dev server)
  │
  └─ 4. Closes session
        - Work auto-committed or stashed
        - Activity summary posted to the issue
        - Machine paused or destroyed
```

## Container Image

The dev machine runs a single Docker image based on Ubuntu with all tools pre-installed.

### Base Image Contents

| Component | Version | Purpose |
|---|---|---|
| Ubuntu | 24.04 LTS | Base OS |
| code-server | latest | VS Code in the browser |
| Claude Code CLI | latest | Agentic coding |
| Chromium | via [linuxserver/docker-chromium](https://github.com/linuxserver/docker-chromium) approach | In-browser web navigation |
| Node.js | 22.x | Frontend tooling |
| Go | 1.25.x | Backend development |
| Docker CLI | latest | Container-in-container (optional) |
| Git | latest | Version control |
| s6-overlay | latest | Process supervisor |

### Process Supervision (s6-overlay)

The container runs multiple services managed by s6-overlay:

```
s6-overlay
  ├── code-server        (always)
  ├── chromium + noVNC    (always)
  ├── dev-server          (on-demand, project-specific)
  └── activity-tracker    (always, reports to Kuayle)
```

## Networking

All services run inside a single container and communicate over `localhost`:

```
┌─────────────────────────────────────────┐
│              Container Network           │
│                                          │
│  code-server (:8443)                     │
│       │                                  │
│       ├── Claude Code CLI                │
│       │     └── talks to Chromium        │
│       │         via CDP (localhost:9222)  │
│       │                                  │
│  Chromium (:3000 noVNC, :9222 CDP)       │
│       │                                  │
│       └── can reach dev server           │
│           at localhost:5173              │
│                                          │
│  Dev Server (:5173)                      │
│       └── accessible from Chromium       │
│           and via subdomain externally   │
└─────────────────────────────────────────┘
```

Claude Code communicates with Chromium via Chrome DevTools Protocol (CDP) on `localhost:9222`, enabling browser automation for UI testing, screenshot capture, and web navigation.

## Security

### Isolation

- Each machine runs in its own container with resource limits (CPU, memory, disk)
- Containers use a non-root user (`coder`)
- Network egress can be restricted per machine configuration
- Filesystem is isolated; only the project repo is mounted

### Authentication Chain

```
User → Kuayle Auth (JWT) → Machine Access Check → Reverse Proxy → Container
```

- No direct container access — all traffic routed through Kuayle's auth layer
- Machine IDs are cryptographically random (not sequential or guessable)
- Session tokens are scoped to specific machines
- Agent API tokens have explicit machine and workspace scopes

### Secrets Management

- Environment variables injected at spawn time, not baked into the image
- Secrets stored encrypted in Kuayle's database, decrypted at container start
- Claude Code API keys managed by Kuayle, passed as env vars

## Resource Limits

| Size | CPU | Memory | Disk | Use Case |
|---|---|---|---|---|
| Small | 2 cores | 4 GB | 20 GB | Quick fixes, small tasks |
| Medium | 4 cores | 8 GB | 50 GB | Feature development |
| Large | 8 cores | 16 GB | 100 GB | Full-stack work, large repos |

Resource limits enforced via Docker's `--cpus`, `--memory`, and disk quota options.

## API

### Machine Management

```
POST   /api/workspaces/:slug/machines           # Create machine
GET    /api/workspaces/:slug/machines           # List machines
GET    /api/workspaces/:slug/machines/:id       # Get machine details
PATCH  /api/workspaces/:slug/machines/:id       # Update config
DELETE /api/workspaces/:slug/machines/:id       # Destroy machine
POST   /api/workspaces/:slug/machines/:id/pause # Pause machine
POST   /api/workspaces/:slug/machines/:id/resume # Resume machine
```

### Activity & Tracking

```
GET    /api/workspaces/:slug/machines/:id/activity      # Activity stream
GET    /api/workspaces/:slug/machines/:id/summary       # Work summary
GET    /api/workspaces/:slug/machines/:id/artifacts      # Linked artifacts
```

### Machine Configuration (Create/Update Payload)

```json
{
  "repo_url": "https://github.com/org/repo",
  "branch": "feature/my-task",
  "size": "medium",
  "issue_id": "KUA-42",
  "mode": "human+agent",
  "env_vars": {
    "DATABASE_URL": "...",
    "API_KEY": "..."
  },
  "tools": {
    "claude_code": true,
    "chromium": true,
    "dev_server": {
      "command": "npm run dev",
      "port": 5173
    }
  },
  "ttl_hours": 24
}
```

## Future Considerations

- **Snapshots** — Save and restore machine state for long-running work
- **Collaborative machines** — Multiple users sharing the same machine (pair programming)
- **Custom images** — User-defined Dockerfiles for specialized environments
- **GPU support** — For ML/AI workloads
- **Cost tracking** — Per-machine resource usage and billing
- **Machine templates** — Pre-configured environments for common project types
