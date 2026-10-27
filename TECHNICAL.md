# Kuayle Dev Machines Technical Specification

Production architecture and implementation contract for isolated, multi-container development environments and agentic coding runs.

## Status

The repository includes the Dev Machines control plane, PostgreSQL schema, lifecycle manager, authenticated gateway, provider adapters, runtime images, activity collector, self-hosted Compose profiles, and product UI. The subsystem is disabled by default and must be configured explicitly by a self-hosted operator.

Dev Machines target trusted self-hosted workspaces. Docker hardening reduces risk, but ordinary containers are not a sufficient isolation boundary for mutually hostile tenants. Operators requiring hostile multi-tenant execution must replace the runtime interface with VM or microVM isolation.

## Principles

- A Dev Machine is a logical workspace composed of cooperating containers. It is never a monolithic IDE/browser/agent container.
- Generic machine creation does not require repository, branch, issue, project, or manual TTL input. Friendly random names are case-insensitively unique per creator within a workspace and can be checked through the availability API.
- Repository and environment defaults resolve from issue, then project, then team, then workspace. One machine has affinity to one repository and can host multiple issue worktrees from that repository; use a separate machine for another repository/environment or concurrent conflicting workload.
- The Kuayle API does not receive the Docker socket.
- Agent, developer, browser, collector, egress, and gateway containers do not receive the Docker socket.
- The Machine Manager is the only Dev Machines runtime component with Docker socket access and is treated as host-privileged. The separately optional system updater also mounts the socket when enabled.
- Machine services never publish host ports. Caddy and the Machine Gateway are the only public ingress path.
- Every machine has an isolated internal Docker network and a dedicated workspace volume.
- code-server and ttyd run in the same developer container with a shared filesystem, processes, `HOME`, tools, and tmux.
- Human browser sessions, machine tokens, and GitHub credentials are distinct credentials with different scopes and lifetimes.
- Top-level browser launches and native terminal WebSockets use distinct one-time ticket flows.
- PostgreSQL is the durable source of truth. Runtime operations are leased, retried, idempotent, and reconcilable after process restarts.
- Agent providers produce a common Kuayle result model regardless of CLI implementation.

## Architecture

```text
                                   Internet
                                      |
                                      v
                              Caddy wildcard TLS
                         app.example.com / *.kuayle-machines.example.net
                            |                         |
                            v                         v
                     Kuayle API + UI          Machine Gateway
                            |                  no Docker socket
                            |                         |
                            +------ PostgreSQL -------+
                                      |
                               durable operations
                                      |
                                      v
                              Machine Manager
                    Dev Machines runtime socket holder
                                      |
                +---------------------+---------------------+
                |                                           |
                v                                           v
       machine-a private network                    machine-b private network
       +----------------------+                    +----------------------+
       | developer            |                    | developer            |
       | agent-*              |                    | agent-*              |
       | browser              |                    | browser              |
       | collector            |                    | collector            |
       | egress proxy         |                    | egress proxy         |
       | workspace volume     |                    | workspace volume     |
       +----------------------+                    +----------------------+
```

### Components

| Component | Trust boundary | Responsibility |
|---|---|---|
| Kuayle API | Application control plane | RBAC, machine and run APIs, policy admission, secret encryption, one-time launch tickets |
| PostgreSQL | Durable control plane | Desired/observed state, operation leases, events, logs, tokens, sessions, results, Git refs |
| Machine Manager | Host privileged | Docker networks, volumes, containers, resource limits, secrets handoff, lifecycle, hard runtime, idle pause, cleanup |
| Machine Gateway | Unprivileged ingress | Host parsing, browser ticket exchange, native terminal ticket exchange, session authorization, header stripping, HTTP/WebSocket proxying, access audit |
| Caddy | Public ingress | Main-domain and wildcard-machine TLS termination |
| Collector | Per-machine data plane | Filesystem, root-checkout Git, browser CDP, and heartbeat events |
| Egress proxy | Per-machine network policy | Public-destination validation, private-address blocking, optional domain allow/deny lists |

## Machine Topology

```text
Dev Machine
├── developer           code-server:8080 (standard) or ttyd:7681 (headless), tmux, Git checkouts
├── agent-*             on-demand container per built-in provider or custom run
├── browser             optional Chrome, CDP, KasmVNC
├── collector           activity and heartbeat collection
├── egress              outbound HTTP/HTTPS policy proxy
├── private network     Docker bridge with internal=true
└── workspace volume    repository worktrees and generated artifacts
```

Every created container has a deterministic name and labels containing `workspace_id`, `machine_id`, and `routing_key`. Runtime discovery never depends on a publicly exposed port. Collector and egress services are always represented; developer/terminal and browser services depend on machine configuration, and agent services are created for individual runs.

The shared workspace is writable by developer and agent containers. The collector receives it read-only. Browser and egress containers do not receive the workspace mount. Per-container home, `/tmp`, `/run`, and secret paths use tmpfs scratch mounts.

Standard machines represent the developer container as separate `ide` and `terminal` service records that point at one container: code-server listens on `8080`, ttyd listens on `7681`, and terminal sessions attach to named tmux sessions under the same `HOME`, process tree, tools, and `/workspace` used by code-server. Headless machines expose only the `terminal` developer-service record plus collector and egress; the terminal uses the headless developer image, which also serves as the autonomous image for the five new built-in CLIs. Development Environment image selection does not replace that headless image. Headless developer containers retain the standard developer security, tmpfs executable scratch space, workspace mount, and terminal-session scripts.

Agent containers are created on demand. Multiple runs are represented separately, although workspaces should default to one mutating autonomous agent at a time to avoid conflicting writes.

## Network Isolation

The manager creates `kuayle-machine-{routing-key}` with Docker's `Internal` option. Only that machine's services and the trusted Machine Gateway join it.

No machine workload joins the PostgreSQL, Redis, API, or Compose control network. The egress container is the only machine service also attached to the shared external `kuayle-machine-egress` network.

Chrome's debugging endpoint binds only to browser-container loopback. Browser-enabled machines receive a dedicated encrypted credential scoped to the browser and collector services; an authenticated relay exposes only the read-only target-list endpoint needed for navigation telemetry and does not proxy CDP control or WebSocket endpoints.

Workloads receive `HTTP_PROXY` and `HTTPS_PROXY` pointing to their machine's egress container. The proxy:

- rejects IP-literal destinations;
- resolves hostnames itself and rejects private, loopback, link-local, multicast, and unspecified addresses;
- permits ordinary HTTP on port 80 and HTTPS on port 443, while allowing CONNECT tunnels only to port 443;
- applies optional suffix-based domain allow and deny lists;
- resolves and dials the validated public address to reduce DNS-rebinding exposure.

An empty domain allowlist permits public Internet destinations while still blocking private address ranges. Operators can fail closed by configuring an explicit allowlist.
An explicit allowlist must include the Kuayle ingestion host plus every Git and provider API hostname required by configured agents.

## Dynamic Routing

The selected ingress design is a hybrid Caddy and Go gateway:

```text
Browser request
  -> wildcard DNS
  -> Caddy wildcard TLS
  -> Machine Gateway
  -> host/service lookup in PostgreSQL
  -> current authorization and machine-state check
  -> private container address
```

Required host patterns are:

| Host | Service |
|---|---|
| `{routing-key}.{DEV_MACHINE_DOMAIN}` | IDE |
| `{routing-key}-terminal.{DEV_MACHINE_DOMAIN}` | Native terminal WebSocket |
| `{routing-key}-browser.{DEV_MACHINE_DOMAIN}` | Browser/KasmVNC |
| `{routing-key}-preview.{DEV_MACHINE_DOMAIN}` | Application preview (optional, see below) |

Routing keys are random lowercase hexadecimal strings. The gateway performs an exact configured-domain suffix check and rejects suffix-confusion hosts.

Caddy has one fixed wildcard route. Per-machine Caddy configuration is unnecessary. Teardown removes the database route and private network, so stale hosts return not found without a proxy reload.

The gateway uses Go's reverse proxy with WebSocket upgrades, streaming flushes, unlimited response write duration, bounded dial timeouts, and long-lived connection support. Caddy terminates wildcard TLS and forwards all wildcard machine hosts, including WebSocket upgrades, to the gateway; per-machine Caddy rewrites are not used.

### Application previews

The machine owner can expose one application port of the developer container
(`PUT /dev-machines/:machineId/preview` with `{"port": n}`, session-only; `GET`
reads it, `DELETE` removes it and revokes its sessions). Ports must be 1024–65535
and cannot be any port used by the machine's own services, so a preview host can
never reach code-server or the terminal without their dedicated protections.
Migration 63 stores the port in `dev_machine_previews`, not as a container service
row, so machine lifecycle code is unaffected. The gateway role may read only its
`machine_id`, `workspace_id` and `port` columns (see
[gateway-role.sql](selfhosting/gateway-role.sql)); re-run the role setup after
upgrading.

`POST /dev-machines/:machineId/preview/launch` uses the same browser-cookie
launch path as the IDE: a one-time ticket for the running developer service row,
bound to the preview host, exchanged for a `__Host-` HttpOnly session cookie that
is valid only on that host. IDE and preview sessions never authorize each other,
only the machine creator is admitted, Kuayle credentials and the gateway session
cookie are stripped before proxying, and upstream cookies are limited to the
preview host. WebSockets (for example hot reload) use the gateway's existing
upgrade support. TLS certificates are approved on demand only while the preview
is configured and the machine runs. Previews stop with the machine (idle pause
included). Preview hosts share the machine domain's site with the machine's other
hosts; cross-host requests are subject to the gateway's origin checks, as between
any two machine hosts. The machine page provides the port, open and remove
controls; a task card offers **Open preview** once its task is ready for review or
has stopped and its machine has a preview.

## Authenticated Machine Access

Main Kuayle cookies remain host-only. They are never broadened to the machine domain.

### Browser-cookie launch path

This path is used for top-level code-server and browser launches.

1. An authenticated user calls the service launch endpoint from the Kuayle UI.
2. The API checks workspace membership, Dev Machine permission, machine state, and service eligibility.
3. The API creates a random one-time ticket. Only its SHA-256 hash is stored.
4. The ticket is bound to user, workspace, machine, service, exact host, and a short expiry (default 60 seconds, configurable via `DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS`).
5. The browser follows the returned HTTPS URL.
6. The gateway atomically consumes the ticket and redirects to remove it from the URL.
7. The gateway creates a revocable machine session and sets a host-only `__Host-kuayle-machine` cookie with `Secure`, `HttpOnly`, `Path=/`, and `SameSite=Lax`. Lax permits the initial cross-site top-level launch while the separate registrable domain provides the site boundary.
8. Every proxied request rechecks session expiry, workspace membership, permitted role, machine state, service identity, and exact host.
9. Stop or teardown revokes all tickets and sessions.

Browser-cookie routes strip `Cookie`, `Authorization`, `Proxy-Authorization`, and all incoming `X-Kuayle-*` headers before forwarding. Upstream host-only application cookies are permitted, but parent-domain cookies and the reserved machine-session cookie are dropped. State-changing requests and browser-cookie service WebSocket upgrades require an exact scheme-and-host machine `Origin`; sibling machines cannot submit authenticated requests to each other.

### Native terminal ticket path

The Kuayle UI renders `@xterm/xterm` and speaks ttyd's native WebSocket protocol (`ttyd.v1`). It does not expose ttyd's web page and does not use the machine session cookie for terminal attachment.

1. An authenticated user creates a terminal session with `POST /dev-machines/:machineId/terminal-sessions`, optionally bound to a checkout.
2. The API validates machine state, checkout readiness, and configured `FRONTEND_URL`, then creates a runtime tmux session name and working directory.
3. The API creates a one-use ticket whose stored hash is bound to the raw ticket, exact normalized `FRONTEND_URL` origin, exact terminal host, user, service, tmux session name, working directory, and expiry.
4. The UI opens the returned `wss://{routing-key}-terminal.{DEV_MACHINE_DOMAIN}/ws?...` URL from the main frontend origin.
5. The gateway requires the WebSocket `Origin` to equal the configured `FRONTEND_URL` origin exactly, consumes the ticket atomically, rewrites upstream to ttyd `/ws` with only the internal `arg=session` and `arg=cwd` values, strips credentials before proxying, audits the exchange, and touches machine activity until the socket closes.

Opening a paused machine service queues an idempotent resume operation and returns a `resuming` response with `retry_after_seconds`; the UI polls/retries. Queued or spawning machines return `pending`. Stopped, destroyed, expired, and invalid states do not auto-resume.

Machine access decisions are persisted in `dev_machine_access_logs` without query strings.

The xterm client preserves the PTY byte stream by disabling end-of-line conversion, uses a line height of `1`, and enables the Unicode 11 addon with `allowProposedApi` for full-screen TUIs. End-of-line conversion must remain off because rewriting LF as CRLF returns the cursor to column zero and corrupts TUIs that use LF to move rows while preserving their column. The developer image installs `ncurses-term`, configures tmux for `tmux-256color` with RGB support, focus and extended-key events, passthrough, and a 50,000-line history, and exports `COLORTERM=truecolor` for terminal sessions. tmux keeps its default escape time and mouse mode stays off: escape sequences cross a WebSocket and may arrive split, and a short escape time would turn the tail of a split sequence into typed text.

The terminal dock can cover the viewport as an in-page full-screen layer. The layer refits xterm and restores terminal focus without reserving browser fullscreen. Its button and `Ctrl+Shift+F` toggle the mode; Escape remains available to the attached TUI and does not exit the layer.

## Machine and Collector Tokens

Collectors use random machine tokens rather than user sessions. Token hashes, scopes, expiry, revocation, and last-use timestamps are stored in `dev_machine_tokens`.

Initial collector scopes are:

- `events:write`
- `logs:write`

Heartbeat is submitted through the event-ingestion endpoint. The raw token is encrypted as a collector-only secret and delivered through tmpfs. Event and log ingestion endpoints reject expired, revoked, incorrectly scoped, or destroyed-machine tokens.

## Provider Abstraction

Providers are implemented in `BE/internal/agent` and registered at startup.

```go
type Provider interface {
    Metadata() Metadata
    BuildInvocation(RunInput) (Invocation, error)
    ParseEvents([]byte) []Event
    ParseResult(stdout, stderr string, exitCode int) RunResult
}
```

`Invocation` contains an image, argv array, working directory, declared secret names, and interactive flag. Providers do not return shell strings. Generic custom providers reject shell entrypoints and require a pinned image tag or digest.

Initial providers:

| ID | CLI | Secret declaration | Adapter modes |
|---|---|---|---|
| `claude-code` | Claude Code | optional `ANTHROPIC_API_KEY` (or a subscription sign-in) | interactive, autonomous |
| `opencode` | OpenCode | model/provider dependent | interactive, autonomous |
| `codex` | Codex | optional `OPENAI_API_KEY` (or a subscription sign-in) | interactive, autonomous |
| `qwen-code` | Qwen Code | none | interactive, autonomous |
| `crush` | Crush | none | interactive, autonomous |
| `kimi` | Kimi | none | interactive, autonomous |
| `goose` | Goose | none | interactive, autonomous |
| `custom` | Admin-configured argv CLI | explicit configuration | interactive, autonomous |

Provider adapters can construct both invocation modes, but the current product dialogs queue autonomous runs only and there is no UI/gateway endpoint for attaching to an interactive agent container. The IDE developer image pins all seven built-in CLI versions for terminal use. Qwen Code, Crush, Kimi, and Goose use `DEV_MACHINE_HEADLESS_IMAGE` for both developer-terminal and autonomous execution. The existing three built-ins retain their provider-specific agent images. Custom providers must be enabled by workspace policy and are currently launched through the autonomous dashboard flow. Provider-specific agent images remain separately pinned by tag or digest in configuration and runtime Dockerfiles; custom provider images must also be pinned.

Codex and Claude Code runs accept optional DeepSeek `model` and `thinking` overrides. They are rejected for other providers. A model override requires a configured workspace DeepSeek credential; thinking levels are `off`, `low`, `high`, and `max`. Codex receives the override as argv `-c` values. Claude Code receives the model as argv and the thinking level through the validated non-secret `CLAUDE_CODE_EFFORT_LEVEL` container environment; `off` omits that variable.

### Normalized Result

```json
{
  "status": "succeeded",
  "summary": "Implemented authenticated machine routing",
  "changed_files": ["BE/internal/machine/gateway.go"],
  "commits": ["abc123"],
  "branch": "kuayle/kua-50",
  "pull_request_url": "https://github.com/example/repo/pull/42",
  "tests_run": ["go test ./..."],
  "test_status": "passed",
  "risk_notes": [],
  "artifacts": []
}
```

Completed autonomous provider stdout/stderr is collected and redacted before persistence. JSON-line provider events are normalized into `dev_machine_events`.

## Execution Modes

### Human and Agent

The user opens authenticated IDE and browser links from Kuayle and attaches native terminal tabs rendered by the main UI. Terminal tabs speak `ttyd.v1` over the dedicated terminal host and attach to tmux sessions inside the developer container. All seven built-in CLIs are available directly from that terminal. Headless machines omit code-server and browser access and expose only the terminal plus agent workflow. Agent runs started from the machine dashboard use separate containers and run autonomously to completion under a timeout.

### Agent Only

An autonomous run receives:

- optional issue/project and checkout linkage;
- repository, base branch, working branch, and workspace path from the selected checkout or machine repository affinity;
- prompt and acceptance criteria;
- allowed commands and forbidden paths;
- exact test-command argv;
- maximum runtime;
- explicitly allowed secret names;
- push and pull-request policy.

Kuayle adds an execution contract instructing the provider to work only inside the selected workspace path, avoid credential output, run the requested tests, commit results, and use the configured working branch.

## Lifecycle

Machine status and desired status are persisted separately.

```text
queued -> spawning -> running
                       -> paused -> running
                       -> stopping -> stopped -> running
                       -> expired -> tearing_down -> destroyed
spawn/reconcile failure -> failed -> spawning or tearing_down
```

| Status | Meaning |
|---|---|
| `configuring` | Reserved status; current creation writes `queued` directly |
| `queued` | A spawn/start operation is waiting for a manager lease |
| `spawning` | Network, volume, images, and service containers are being created |
| `running` | Services are started and gateway access is permitted |
| `paused` | Docker processes are paused; memory remains allocated |
| `stopping` | Services are being stopped |
| `stopped` | Containers are retained but CPU/RAM are released; start is allowed |
| `tearing_down` | Sessions and Docker resources are being removed |
| `destroyed` | Final lifecycle state after runtime teardown; retained events/results remain queryable |
| `failed` | A lifecycle operation attempt failed and requires retry or teardown |
| `expired` | Maximum runtime elapsed and teardown has been queued |

Lifecycle commands write `dev_machine_operations` with generation and idempotency key. Leasing locks the machine row and selects at most one ordinary operation per machine; teardown and cancellation may preempt a long-running agent operation. The manager cancels local run contexts before destructive cleanup. Every non-destructive state write is conditional on the operation generation, and a stale spawn compensates by tearing down anything it created, so it cannot overwrite or survive a newer desired state. Identical idempotency-key retries return the original operation; reusing a key for another action is rejected.

Every reconciliation pass inspects Docker networks, volumes, gateway attachment, and non-agent service containers. A desired-running machine with missing, stopped, or detached resources is repaired from deterministic names and labels before the database is returned to `running`. Workspace deletion is rejected until all machine runtimes are destroyed, preserving the records needed for cleanup.

Resource names and labels are deterministic. Missing resources during teardown count as success. A partial spawn removes containers, disconnects the gateway, and removes the private network and workspace volume before retrying.

The production repository currently supplies services to the runtime in egress, collector, developer, then browser order; the Docker runtime starts them in caller-provided order rather than computing dependencies itself. Developer and browser containers publish file-based Docker health checks.

Idle pause and hard runtime are separate controls. Workspace policy defaults idle pause to 240 minutes; `keep_running=true` bypasses idle pause for that machine. Gateway requests, native terminal WebSockets, API activity touches, checkouts, and agent runs update `last_activity_at`. The manager queues idempotent pause operations for idle running machines, but it does not automatically delete machines. Maximum hard runtime is policy-controlled through `expires_at`; expired machines transition to `expired` and teardown is queued.

Pause, stop, teardown, and permanent delete are intentionally distinct:

- **Pause** pauses Docker processes and keeps memory allocated.
- **Stop** stops containers while retaining deterministic runtime resources for a later start.
- **Teardown** revokes sessions/tickets and removes containers, the private network, and workspace volume; database history remains.
- **Permanent delete** is admin-only. `DELETE /dev-machines/:machineId` and selected bulk DELETE record `delete_requested_at`; the manager converges runtime teardown first and purges after the operation completes. The `/permanent-delete` endpoint remains an equivalent explicit route, while bulk permanent delete is a guarded purge for old already-safe rows only.

## Agent task integration

Persistent task execution is being integrated with the existing native run and
machine operation records. The [agent task contract](BE/docs/agent-tasks.md)
documents migration 56, authorization/lease ordering, encrypted per-attempt
recovery credentials, live output capture and conversation events, and the
session-only task routes, worker wiring, independent validation, pull request publication and remaining review work. Manual IDE/terminal
workflows continue to use the existing machine controls.

## Resource Limits and Quotas

**Host admission.** Before spawning or starting a machine, the manager reads the
Docker host's CPU count and memory (`/info`) and admits the machine only if the
sum of live machines' requests (configuring, spawning, running, paused or
stopping; paused machines keep memory) plus this one fits within the host minus
`DEV_MACHINE_HOST_RESERVED_MEMORY_MB` / `DEV_MACHINE_HOST_RESERVED_CPU_MILLIS`.
Memory is never overcommitted; CPU may be, up to `DEV_MACHINE_CPU_OVERCOMMIT`.
A machine that does not fit stays queued: its operation is deferred for 30
seconds with a visible `host_capacity_wait` reason and without consuming a
retry, and agent tasks keep waiting rather than failing. Admission is serialized
within one manager process, which also counts machines admitted but not yet
running; multiple manager replicas on one host are not coordinated. An unknown
host size never blocks. Idle machines are still paused by `IdlePauseMinutes`,
preserving worktrees, and the workspace/user machine quotas below still apply.

Machine sizes are aggregate admission profiles:

| Size | CPU | RAM | Workspace disk hard quota | Maximum runtime |
|---|---:|---:|---:|---:|
| small | 2 vCPU | 4 GB | 20 GB | 2 hours |
| medium | 4 vCPU | 8 GB | 50 GB | 4 hours |
| large | 8 vCPU | 16 GB | 100 GB | 8 hours |

The manager assigns a bounded percentage of the profile to each service and applies Docker `NanoCPUs`, memory, memory-swap, and PID limits. The IDE and browser each receive 25%, an agent run 35%, and the collector and egress 5% each; a headless machine's terminal container receives 50%, the IDE and browser shares it replaces. Non-builder workloads use a read-only root filesystem, tmpfs scratch paths, `CapDrop=ALL`, `no-new-privileges`, the Docker default seccomp profile or a configured replacement, and AppArmor where available. Environment Builder deliberately relaxes the developer container root filesystem so owners/admins can install tooling before snapshot. Docker's `json-file` logs rotate at 10 MiB with three files per container, and removal paths also remove anonymous image volumes.

Workspace policy controls:

- maximum concurrent machines;
- maximum machines per user;
- maximum daily agent runs;
- maximum runtime;
- idle pause interval (default 240 minutes; bypassed by per-machine `keep_running`);
- maximum per-machine workspace-volume size;
- allowed providers;
- allowed repositories;
- whether custom providers are allowed.

### Disk Limitation

Workspace volumes use Docker's built-in `local` driver with its `size` option set to the machine profile's exact byte limit. Docker enforces that option through XFS project quotas. The Docker data root must therefore reside on XFS mounted with `pquota` or `prjquota`, and the daemon must not run in a user namespace. The manager creates and removes a quota-probe volume during startup; if Docker reports that quota support is unavailable, manager startup fails and its readiness endpoint remains unavailable. Existing volumes without the expected local driver, ownership labels, and exact hard limit are rejected before containers start or runtime reconciliation reuses them.

The manager still samples workspace usage, stores resource telemetry, emits `resource.disk_quota_warning` at 90 % of the limit, and queues a stop with `resource.disk_quota_exceeded` at the limit as defense in depth. That delayed monitor is not the primary storage boundary in the default `hard` mode. CPU, memory, process, and workspace disk limits are enforced by the runtime.

#### Monitored mode (hosts without XFS project quotas)

`DEV_MACHINE_DISK_QUOTA_MODE=monitored` is an explicit operator opt-in for hosts whose Docker data root cannot provide project quotas (for example ext4). The manager then skips the startup quota probe and creates workspace volumes without the `size` option, labelled `com.kuayle.workspace-quota-mode=monitored` and with the limit in `com.kuayle.workspace-quota-bytes`. The workspace volume is the only unbounded write surface: root filesystems are read-only, `/tmp` and home directories are size-limited tmpfs, and container logs are capped.

Enforcement becomes detect-and-stop: every 30-second reconciliation measures `/workspace`, warns at 90 % and stops the machine at 100 % (the volume is kept). A host free-space floor protects co-located workloads: when the Docker data-root filesystem (measured with `statfs` on a read-only mount of it in the manager, or through a running collector when no mount is configured) has less free space than `DEV_MACHINE_HOST_DISK_MIN_FREE_GB` or `DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT`, every running machine is stopped with `resource.host_disk_floor` and new starts fail with `host_disk_low` until a fresh measurement shows space has been recovered. A fast writer can exceed its limit between two checks, so the floor must leave enough headroom for that overshoot. The workspace policy API reports `disk_quota_mode`, and the UI labels limits as monitored and warns administrators. Hard mode remains the default and recommended configuration.

## Secrets

`DEV_MACHINE_ENCRYPTION_KEY` is independent from `JWT_SECRET` and is required when Dev Machines are enabled. AES-256-GCM ciphertext and key version are stored in `dev_machine_env_vars`.

Secret flow:

1. The API validates the target service and provider, encrypts the value, and stores only ciphertext.
2. The manager decrypts only active secrets matching the service or agent run.
3. Agent runs receive only names explicitly selected in `allowed_secrets`; required provider names must also resolve to active, unexpired, non-revoked stored secrets before a run is queued.
4. The manager creates a tmpfs at `/run/kuayle-secrets` and copies files after container start, so secret values do not appear in Docker container configuration.
5. A small entrypoint loads values only into the target child process and removes the files.
6. Browser and egress containers receive no workspace secrets.
7. Manager-captured provider logs are redacted with the exact injected values. IDE command telemetry redacts common key/token/secret/password patterns.

Reserved `KUAYLE_*` variables and `GITHUB_TOKEN` cannot be supplied by users.

### DeepSeek workspace configuration

Workspace owners and admins can store one DeepSeek credential per workspace through the Dev Machine settings API. Saving and removing it requires `dev_machine:admin`; members with `dev_machine:read` can read its status (configured, default model, thinking) so the agent-run dialog can offer DeepSeek overrides. The API key is encrypted with `cryptoutil.Encrypt` and `DEV_MACHINE_ENCRYPTION_KEY`; it is never returned, including in error and update responses. The default model is `deepseek-flash` or `deepseek-v4-pro`, and the default thinking level is `off`, `low`, `high`, or `max`.

The credential is workspace data rather than a machine snapshot. On every developer-container start, and for every Codex or Claude Code agent run, the manager resolves and decrypts the current row. It copies the DeepSeek key, model, thinking level, Claude Code DeepSeek endpoint/model aliases, and Codex configuration inputs into `/run/kuayle-secrets`; browser, collector, and egress containers never receive them. Rotating the credential therefore applies the next time the developer or agent container starts.

The IDE and headless start scripts and the autonomous agent runner create `$HOME/.codex/config.toml` (mode 0600) only when the file is absent. They configure the DeepSeek Responses provider, map Codex `off` to `none`, and leave an existing user-managed Codex configuration unchanged. They also generate `$HOME/.codex/model-catalogs/deepseek.json` from the installed Codex's bundled catalog (`codex-deepseek-catalog.cjs`) and reference it with `model_catalog_json`, so Codex's `/model` picker offers only `deepseek-flash` and `deepseek-v4-pro`; if generation fails, the config omits the catalog and the configured model still works.

## Scoped Settings, Worktrees, and Environments

Development defaults are stored in `dev_machine_scope_settings`. A setting may provide a linked GitHub repository, base branch, development environment, test command, preferred coding agent and/or agent instructions (at most 8000 characters) for a workspace, team, project, or issue. Each value is resolved independently in this order: issue, project, team, workspace (migration 65). The preferred agent is the default for task delegation from chat and the task composer when the workspace policy allows it; instructions are snapshotted onto each task when it is accepted or retried and placed ahead of the task in the native prompt. This lets an issue override only the repository while inheriting an environment, or vice versa.

Machine creation accepts optional issue/project/repository information but does not require it. If no repository resolves, legacy repository fields remain empty and the machine starts as a generic developer workspace. When an issue is opened, `POST /dev-machines/:machineId/checkouts` resolves the issue's development settings, enforces the machine's repository affinity, and creates an idempotent issue worktree under `/workspace/tasks/{issue-key}`. A machine can hold multiple ready checkouts from the same repository; checkouts from another repository or environment require a separate machine.

Development Environments are immutable local OCI images created from owner/admin-created Environment Builder machines. Environment Builders run the developer container with a writable root filesystem so tooling can be installed intentionally. Snapshot requests are accepted only while the builder is `paused` or `stopped`; the environment moves `pending -> building -> ready` or `failed`. The snapshot commits the developer container layer with Kuayle environment/workspace labels, may leave a human-readable local tag (`kuayle/dev-environment-{id}:snapshot`), and records the immutable local image ID (`sha256:...`) as both `image_ref` and `image_digest` once ready. Machine creation uses only that immutable local ID, verifies the image labels match the expected workspace/environment, and never registry-pulls a missing local snapshot ID. The repository workspace named volume, `/run/kuayle-secrets`, `/tmp`, and other tmpfs scratch mounts are not part of the image. Selected shell and code-server customization is copied into an image template before commit; files in that template whose names contain `history`, `token`, or `credential` are removed. The complete writable developer layer is still committed, so operators must scrub any other sensitive data before snapshotting.

Environment deletion is two-phase. The API marks an environment `delete_requested`; the manager removes only the immutable local image ID after verifying the Kuayle environment/workspace labels and only then deletes the database record. Operators migrating hosts must move any required local OCI images separately from PostgreSQL and Docker volumes.

## GitHub Integration

Dev Machines use GitHub App installation tokens, not broad personal access tokens. Generic machines may start without a repository; issue checkouts resolve the linked repository through issue, project, team, then workspace development settings. Once a machine has repository affinity, additional issue worktrees must use the same linked repository.

Required GitHub App repository permissions:

| Permission | Level |
|---|---|
| metadata | read |
| contents | read and write |
| pull requests | read and write |

The manager requests a token restricted to the selected linked repository and required permissions for the machine checkout or agent run. The token is short-lived, never stored in plaintext, and injected only into the developer or agent containers that need Git.

The IDE uses an ephemeral askpass helper for clone. Agent containers install an ephemeral Git credential helper. Kuayle resolves the workspace or global GitHub App client again and mints a fresh token before creating a pull request after a successful run.

Branches, commits, and pull requests are persisted in `dev_machine_git_refs` with workspace, machine, run, issue, repository, and URL linkage. Existing GitHub webhooks continue to populate the issue development timeline.

Each issue checkout is an idempotent `dev_machine_checkouts` record with its own working branch and workspace path under `/workspace/tasks/{issue-key}`. Agents and terminal sessions can target a checkout; if multiple checkouts exist, agent runs must name the checkout to avoid ambiguous writes.

## Activity and Logs

| Event | Source |
|---|---|
| file created/modified/deleted | collector filesystem scan |
| command finished and exit code | IDE shell hook |
| root-checkout branch changed | Git post-checkout hook |
| root-checkout commit created | Git post-commit hook and collector HEAD scan |
| root-checkout push started | Git pre-push hook |
| browser navigation | collector polling Chrome CDP, query and fragment removed |
| agent provider event | provider JSON-line parser |
| service and resource state | manager lifecycle and Docker sampling |
| machine opened/denied | gateway access audit |
| lifecycle changes | API and manager |

Activity is operational telemetry, not tamper-proof security auditing. Trusted workspace code can bypass in-container hooks.

Structured events use a monotonic `BIGSERIAL` cursor. Completed autonomous agent stdout/stderr uses cursor-addressable `dev_machine_log_chunks`. The schema reserves a PTY stream, but native terminal and interactive-agent PTY output is not currently persisted. Issue worktrees do not install the root-checkout Git hooks. API reads are workspace-scoped. High-volume log retention should be configured and periodically pruned by operators.

The UI exposes machine status, services, resource samples, cursor-paginated events and logs, checkouts, terminal sessions, agent runs, normalized results, commits, branches, PRs, and lifecycle/delete controls. Native terminal attachment is implemented in the Kuayle UI with `@xterm/xterm`; ttyd's own web page is not exposed.

## Database Schema

Migration `000033_dev_machines` creates:

| Table | Purpose |
|---|---|
| `dev_machines` | Legacy repository fields, issue/project, desired/observed state, size, runtime names, hard runtime, failures |
| `dev_machine_services` | Container image/name/ID, service type, private endpoint, health |
| `dev_machine_volumes` | Workspace/scratch/artifact volume names and monitored usage |
| `dev_machine_env_vars` | Encrypted, target-scoped, provider-scoped values with TTL/revocation |
| `dev_machine_tokens` | Hashed machine tokens, scopes, expiry, revocation |
| `dev_machine_agent_providers` | Enabled providers, pinned images, modes, secret declarations, custom config |
| `dev_machine_agent_runs` | Prompt snapshot, policy, argv, lifecycle, normalized result |
| `dev_machine_agent_run_steps` | Ordered provider/run execution steps |
| `dev_machine_operations` | Durable leased lifecycle and agent operation queue |
| `dev_machine_events` | Normalized machine/run event cursor |
| `dev_machine_log_chunks` | Redacted stdout/stderr chunks plus reserved PTY/system streams |
| `dev_machine_artifacts` | Object-storage artifact metadata |
| `dev_machine_git_refs` | Issue-linked branches, commits, and PRs |
| `dev_machine_access_tickets` | Hashed, one-time, host/service-bound launch and terminal tickets |
| `dev_machine_access_sessions` | Hashed, revocable browser sessions |
| `dev_machine_access_logs` | Allowed and denied gateway requests |
| `dev_machine_resource_samples` | CPU, RAM, disk, PID, and network samples |
| `dev_machine_workspace_policies` | Workspace quota and allowlist configuration |
| `dev_machine_environments` | Immutable local OCI environment images and two-phase deletion state |
| `dev_machine_scope_settings` | Workspace/team/project/issue repository, branch, and environment defaults |
| `dev_machine_checkouts` | Idempotent issue worktrees with repository and branch metadata |
| `dev_machine_terminal_sessions` | User terminal tabs mapped to runtime tmux sessions and optional checkouts |
| `dev_machine_runtime_credentials` | Encrypted, expiring runtime-secret registrations used for telemetry redaction |
| `dev_machine_workspace_model_credentials` | Encrypted workspace DeepSeek API key, default model, and thinking level |

The migration creates the final scoped-workspace schema directly. Composite foreign keys bind workspace, machine, service, checkout, run, session, and environment tuples so cross-workspace identifiers are rejected by PostgreSQL rather than relying only on application checks. Rolling migration `000033` back is destructive for all Dev Machine control-plane records.

Migration `000051_dev_machine_agents_headless` adds the `dev_machines.profile` value (`standard` or `headless`) and expands only the default workspace-policy provider allowlist to all seven built-ins. Existing workspace policies are not rewritten, so administrators retain their current allowlists until they opt in. The down migration removes the profile column and restores the previous default allowlist.

Migration `000052_dev_machine_deepseek` adds workspace model credentials and persists each agent run's non-secret command environment so a Claude Code effort override survives queueing.

All user-facing repository access is constrained by both `workspace_id` and resource ID. Internal manager lookups are limited to the trusted manager process.

## API

All user APIs are under `/api/workspaces/:slug` and use existing JWT, membership, and RBAC middleware.

```text
GET    /api/workspaces/:slug/dev-machines
POST   /api/workspaces/:slug/dev-machines
DELETE /api/workspaces/:slug/dev-machines/bulk
POST   /api/workspaces/:slug/dev-machines/bulk/permanent-delete
GET    /api/workspaces/:slug/dev-machine-names/suggestion
GET    /api/workspaces/:slug/dev-machine-names/availability

GET    /api/workspaces/:slug/dev-machine-policy
PATCH  /api/workspaces/:slug/dev-machine-policy
GET    /api/workspaces/:slug/dev-machine-scope-settings
GET    /api/workspaces/:slug/dev-machine-scope-setting
PUT    /api/workspaces/:slug/dev-machine-scope-setting
DELETE /api/workspaces/:slug/dev-machine-scope-setting
GET    /api/workspaces/:slug/dev-machine-environments
POST   /api/workspaces/:slug/dev-machine-environments
GET    /api/workspaces/:slug/dev-machine-environments/:environmentId
DELETE /api/workspaces/:slug/dev-machine-environments/:environmentId
GET    /api/workspaces/:slug/dev-machine-providers
GET    /api/workspaces/:slug/dev-machine-model-providers/deepseek
PUT    /api/workspaces/:slug/dev-machine-model-providers/deepseek
DELETE /api/workspaces/:slug/dev-machine-model-providers/deepseek

GET    /api/workspaces/:slug/dev-machines/:machineId
PATCH  /api/workspaces/:slug/dev-machines/:machineId
DELETE /api/workspaces/:slug/dev-machines/:machineId
POST   /api/workspaces/:slug/dev-machines/:machineId/permanent-delete
POST   /api/workspaces/:slug/dev-machines/:machineId/start
POST   /api/workspaces/:slug/dev-machines/:machineId/stop
POST   /api/workspaces/:slug/dev-machines/:machineId/pause
POST   /api/workspaces/:slug/dev-machines/:machineId/teardown
POST   /api/workspaces/:slug/dev-machines/:machineId/activity
GET    /api/workspaces/:slug/dev-machines/:machineId/checkouts
POST   /api/workspaces/:slug/dev-machines/:machineId/checkouts
GET    /api/workspaces/:slug/dev-machines/:machineId/events
GET    /api/workspaces/:slug/dev-machines/:machineId/logs
GET    /api/workspaces/:slug/dev-machines/:machineId/services
GET    /api/workspaces/:slug/dev-machines/:machineId/providers
GET    /api/workspaces/:slug/dev-machines/:machineId/resource-usage
POST   /api/workspaces/:slug/dev-machines/:machineId/services/:service/launch
GET    /api/workspaces/:slug/dev-machines/:machineId/terminal-sessions
POST   /api/workspaces/:slug/dev-machines/:machineId/terminal-sessions
POST   /api/workspaces/:slug/dev-machines/:machineId/terminal-sessions/:sessionId/close

GET    /api/workspaces/:slug/dev-machines/:machineId/agent-runs
POST   /api/workspaces/:slug/dev-machines/:machineId/agent-runs
GET    /api/workspaces/:slug/agent-runs
GET    /api/workspaces/:slug/agent-runs/:agentRunId
POST   /api/workspaces/:slug/agent-runs/:agentRunId/cancel
```

Collector-only endpoints use scoped machine tokens:

```text
POST /api/dev-machine-ingest/events
POST /api/dev-machine-ingest/logs
```

Lifecycle APIs return `202 Accepted` with an operation record. `/teardown` retains history; admin DELETE and `/permanent-delete` routes record a purge request and return `202 Accepted` while the manager performs the safe hard purge. Selected bulk deletion deduplicates machine IDs and returns per-item `accepted`, `not_found`, `conflict`, or `failed` results. Create returns the queued machine. Service launches and terminal sessions may return `ready`, `pending`, or `resuming` with `retry_after_seconds`. Events and logs use `after_id` cursors.

Example create payload:

```json
{
  "size": "medium",
  "profile": "standard",
  "services": { "ide": true, "browser": true },
  "agents": [{ "provider": "opencode", "mode": "autonomous" }],
  "env_vars": [],
  "keep_running": false
}
```

Create requests accept `profile: "standard" | "headless"`; omission means standard. Headless requests must omit both `services.ide` and `services.browser`.

## Failure Handling

| Failure | Behavior |
|---|---|
| API or manager restart | Pending/expired leases remain in PostgreSQL and are re-leased; active operations renew their lease every 30 seconds |
| Image pull or container creation failure | Bounded retry; partial resources removed before retry |
| Docker unavailable | Manager readiness fails; operations remain durable |
| PostgreSQL unavailable | Running containers continue; manager/gateway report degraded health |
| Service unavailable | Gateway returns 502 and records an access/proxy error |
| Hard runtime expiry | Machine becomes `expired`; idempotent teardown is queued; records are not automatically deleted |
| Idle interval reached | A non-keep-running machine receives an idempotent pause operation |
| Opening paused service | API queues an idempotent resume and returns `resuming` with retry guidance |
| Permanent delete | Admin request is persisted; manager queues teardown if needed, then deletes the machine row and child history only after resources are safe |
| Disk threshold reached | Resource event recorded and stop operation queued |
| Teardown repeated | Missing containers/networks/volumes are treated as already removed |
| Agent timeout | Container stopped; run result normalized to `timeout` |
| Agent cancellation | Cancel operation removes the run container |

The manager exposes `/health`, `/ready`, and Prometheus-text `/metrics`. Structured logs include workspace, machine, run, operation/provider where available, event type, and duration. Secret values and full environment maps are not logged.

## Self-Hosted Deployment

The optional [release updater](selfhosting/RELEASE_UPDATER.md) supports detached release checkouts and digest-pinned production Compose deployments. It exports release source without changing the checkout, prepares images before downtime, stops application/control-plane writers before a database backup and migrations, and waits for health before removing maintenance mode. Failure after migrations begin leaves writers stopped; it never restarts old binaries against a potentially changed schema.

Dev Machines are an opt-in Compose profile.

Prerequisites:

- Linux Docker Engine and Compose v2;
- Docker's data root on XFS mounted with project quotas (`pquota` or `prjquota`); rootless or user-namespace Docker is not supported because the local volume driver cannot apply project quotas there. Hosts without project quotas may opt into `DEV_MACHINE_DISK_QUOTA_MODE=monitored` (see Disk Limitation);
- `FRONTEND_URL` set to the exact public Kuayle origin, including scheme and any non-default port, for native terminal WebSocket `Origin` validation;
- a separate registrable domain for machine workloads by default (e.g. `kuayle-machines.example.net`), or the explicit personal/trusted `DEV_MACHINE_ALLOW_SAME_SITE` opt-in;
- wildcard DNS for `*.${DEV_MACHINE_DOMAIN}`;
- production TLS via DNS-01 wildcard certificates, an operator-provided wildcard certificate, or restricted on-demand individual certificates (see the README setup);
- sufficient host CPU, memory, and disk for configured concurrency;
- a GitHub App with scoped write permissions for autonomous push/PR workflows;
- runtime images built locally or available from a trusted registry.

The checked-in Caddy wildcard block uses `tls internal` (Caddy's local CA), which is suitable for local or internal-network installations. For a public production machine domain, the profile prerequisite inspects the mounted Caddyfile and fails before grants or control-plane startup while an active `tls internal`, `issuer internal`, or `local_certs` directive remains. Replace it with a DNS-01 issuer supported by a custom Caddy build or mount an operator-provided wildcard certificate. `machines.localhost` is the local-development exception.

Build and start:

```sh
docker compose --profile dev-machine-images build
docker compose run --rm backend /app/server migrate up
docker compose --profile dev-machines run --rm machine-gateway-db-provision
docker compose --profile dev-machines up -d
```

Required configuration when enabled:

| Variable | Purpose |
|---|---|
| `FRONTEND_URL` | Exact public frontend origin; the gateway requires native terminal WebSocket `Origin` to match this value exactly |
| `DEV_MACHINES_ENABLED=true` | Enables API configuration validation and routes |
| `DEV_MACHINE_DOMAIN` | Machine base domain; separate registrable domain by default (`machines.localhost` for local development) |
| `DEV_MACHINE_ALLOW_SAME_SITE` | Default false. Explicit personal/trusted opt-in requiring an exact HTTPS frontend origin; enables host-prefixed app cookies and exact-origin browser request protection. Switching requires a new login. |
| `DEV_MACHINE_ENCRYPTION_KEY` | Independent secret-encryption passphrase, at least 32 characters |
| `DEV_MACHINE_INGEST_URL` | Public HTTPS collector ingestion base URL |
| `DEV_MACHINE_GATEWAY_CONTAINER` | Stable gateway container name attached to private networks |
| `DEV_MACHINE_GATEWAY_DB_USER` | Dedicated PostgreSQL login provisioned for the Internet-facing gateway |
| `DEV_MACHINE_GATEWAY_DB_PASSWORD` | Independent gateway database password used by the provisioning job |
| `DEV_MACHINE_GATEWAY_DATABASE_URL` | Optional explicit URL override; required in production outside Compose and must use a different login from `DATABASE_URL` |
| `DEV_MACHINE_SESSION_TTL_MINUTES` | Browser machine-session lifetime |
| `DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS` | One-time launch ticket lifetime |
| `DEV_MACHINE_EGRESS_ALLOWLIST` | Optional comma-separated public domain suffixes |
| `DEV_MACHINE_EGRESS_DENYLIST` | Optional denied domain suffixes |
| `DEV_MACHINE_SECCOMP_PROFILE` | Docker default, unconfined, or an inline JSON profile |
| `DEV_MACHINE_APPARMOR_PROFILE` | Docker default or an operator-loaded host profile name |
| `DEV_MACHINE_DOCKER_HOST` | Docker daemon address (default: unix:///var/run/docker.sock) |
| `DEV_MACHINE_DISK_QUOTA_MODE` | `hard` (default; XFS project quotas) or `monitored` (measured limits plus host free-space floor) |
| `DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT` | Monitored mode: minimum free percentage of the Docker data root (default 15) |
| `DEV_MACHINE_HOST_DISK_MIN_FREE_GB` | Monitored mode: minimum free GiB of the Docker data root (default 25) |
| `DEV_MACHINE_DOCKER_ROOT` | Host path of Docker's data root, mounted read-only into the manager (default `/var/lib/docker`) |
| `DEV_MACHINE_HOST_DISK_PATH` | In-container path measured for host free space (Compose sets the mount point; falls back to a running collector when empty) |
| `DEV_MACHINE_HOST_RESERVED_CPU_MILLIS`, `DEV_MACHINE_HOST_RESERVED_MEMORY_MB`, `DEV_MACHINE_CPU_OVERCOMMIT` | Host admission for spawning/starting machines (defaults 1000, 2048, 2): Docker host CPU/memory minus these reserves bounds the sum of live machines' requests; memory is never overcommitted, CPU up to the factor. Machines that do not fit wait in the queue without consuming retries |
| `DEV_MACHINE_AGENT_GIT_NAME`, `DEV_MACHINE_AGENT_GIT_EMAIL` | Commit author for agent task publications (default `Kuayle Agent <agent@kuayle.invalid>`); pull requests are opened by the GitHub App installation. Control characters are rejected |
| `DEV_MACHINE_DOCKER_GID` | Numeric host group owner of the mounted Docker socket; obtain it on Linux with `stat -c '%g' /var/run/docker.sock` (Docker Desktop commonly uses `0`) |
| `DEV_MACHINE_HEADLESS_IMAGE` | Headless developer and new built-in provider image (default `ghcr.io/kuayle/dev-machine-headless:0.1.0`) |
| `DEV_MACHINE_*_IMAGE` | Other pinned runtime image references; the IDE developer image contains all built-in CLIs and the existing provider-specific agent images remain separately pinned |

Compose passes the same `DEV_MACHINES_ENABLED` value to the backend, gateway, and manager. The `dev-machines` profile's database-provisioning prerequisite exits before modifying grants or starting the control plane unless that value is exactly `true`; the gateway and manager also retain their own fail-closed startup checks.

The manager image and Compose service run the process as UID/GID 1000 with all Linux capabilities dropped and only the Docker socket's numeric host group added. Docker socket access remains host-root-equivalent despite the non-root UID: it is a host-administrator boundary that must not be reused for general API traffic or exposed to any machine workload.

Compose places the Internet-facing gateway only on an internal `machine-control` network shared with Caddy and PostgreSQL; it does not join the backend/Redis application network or receive the application's database-owner URL. `machine-gateway-db-provision` creates or re-hardens a `NOINHERIT` login after migrations, revokes broad schema/table/sequence/function privileges and role memberships, and grants only route reads, ticket/session transitions, activity timestamp updates, and access-log inserts. The gateway refuses production startup without a separate URL and rejects roles with superuser, role/database creation, replication, row-security bypass, object-creation, or inherited-role powers. Rerun provisioning after schema changes before restarting the gateway; `selfhosting/update.sh` does this automatically while the profile is active.

Valid-looking unknown wildcard hosts are bounded before they can exhaust the database: route lookups use a 60-per-second source limit, a 500-per-second process limit, and a 32-query concurrency cap; missing routes are cached for 30 seconds. Attacker-controlled source and negative-route state is capped, and unknown-route audits are limited to one per source per five minutes and ten total writes per minute. The manager deletes access logs older than 90 days in batches of 1,000. Malformed or out-of-domain hosts are rejected before any database operation.

The self-hosting `dev-machine-images` profile builds the IDE, headless developer, browser, collector, egress, and built-in provider agent images locally. The headless image combines the ttyd developer terminal with the autonomous agent-run entrypoint, while the IDE image retains code-server and ttyd. Environment Builder snapshot bytes are additional local OCI images in the host Docker image store, while their references and lifecycle metadata are stored in `dev_machine_environments`; snapshots are not Compose volumes. Backups and host migrations must account for PostgreSQL, Docker named volumes, and any environment images that should survive a host replacement.

`selfhosting/update.sh` detects an active Dev Machines profile without enabling it on other deployments. When active, it rebuilds runtime images before stopping the old gateway and manager, applies migrations with the new backend image, and starts the updated application and control plane only after the schema is current. If migration, role provisioning, or a later update step fails after the stop, its EXIT trap removes the upgrade marker, preserves the original failure status and container logs, and uses `docker compose start` to revive the same previous control-plane containers rather than recreating them from new images.

## Security Limitations

- Docker hardening is not hostile multi-tenant isolation.
- Pinned Chrome Stable packages for amd64 and arm64 run through KasmVNC in a separately hardened non-root container. The browser's internal sandbox is disabled because Docker namespace restrictions conflict with its setup. The browser must not receive provider or GitHub secrets.
- Domain egress controls cannot prevent exfiltration to an explicitly allowed destination.
- The workspace DeepSeek credential is delivered into every developer container, so any member who can create a machine can read it from a terminal or have an agent reveal it. The API never returns it, but treat it as shared with those members (SECURITY_AUDIT.md KS-08).
- Command and filesystem collection is best-effort and can be bypassed by trusted workspace code.
- Provider adapters can construct interactive invocations, but current product dialogs queue autonomous agent runs and no interactive-agent attach endpoint is exposed. Direct interactive use of the built-in CLIs is available through Kuayle's native xterm terminal; ttyd's web page is not exposed.
- Workspace hard quotas require Docker local-volume project-quota support; the manager intentionally fails startup rather than run machines with an unbounded workspace.
- A separate registrable domain provides browser site isolation by default. Personal/trusted installations can explicitly opt into same-site machines with host-prefixed app cookies and exact-origin mutation/WebSocket checks. This reduces defense in depth and logs a startup warning. Localhost development remains exempt.

These limitations are deployment facts and must not be represented as stronger guarantees in product or security documentation.

### Platform controls outside Dev Machines

The October 2026 audit ([SECURITY_AUDIT.md](SECURITY_AUDIT.md)) added these invariants:

- Session JWTs carry `typ` (`access` or `refresh`). The API authenticates only access tokens; untyped legacy tokens are accepted only with the 15-minute access lifetime.
- Assets are served with their stored content type only when it is in `domain.AssetContentTypeExtensions`; any other type, including one restored from a workspace import, is served as `application/octet-stream` attachment. Import normalizes the type too.
- Outbound requests to workspace-chosen URLs (AI provider `base_url`) dial through `validate.PublicAddressControl`, never follow redirects, and keep transport errors server-side. `AI_ALLOW_PRIVATE_NETWORK=true` lets operators use a private-network provider, which trusts every workspace owner with internal access.
- With the shared GitHub App, installation callbacks require the installing user's OAuth code and the installation in `GET /user/installations`; enable "Request user authorization (OAuth) during installation" and set `GITHUB_APP_CLIENT_ID`/`GITHUB_APP_CLIENT_SECRET`.
- Issue, project and template writes reject users who are not workspace members and labels, cycles and statuses from other workspaces; repository unlinks are workspace-scoped; the public GitHub webhook body is capped at 25 MB.

The gateway also serves a private HTTP certificate-approval listener on port 8091.
It validates the exact requested hostname against a live route and checks running
status, desired status and expiry with a two-second database deadline and bounded
concurrency. This listener has no Docker or application credential access. It must
not be published; Caddy uses it to restrict on-demand issuance. Public workload
traffic remains on port 8090. See README for a stock-Caddy setup without DNS API keys.

IDE dependency refreshes are pinned in `devmachine/ide/Dockerfile` and
`devmachine/ide/patch-security.cjs`. The latter installs body-parser with its
complete nested dependency tree and Zod 4.6.7, preserves v3 root entrypoints for
v3 consumers, and backports the upstream early collection upper-bound rejection
to the still-unpatched bundled v3 implementation (GHSA-mrhx-c7xc-m3w5; upstream
commit a0ebf970f2d752e5f58527cd2942ff48be4f46bd). Both CommonJS and ESM are tested
during the build, including async parsing and valid inputs; parser smoke tests
cover normal, malformed, oversized and aborted requests. No scanner exclusion is
used. Remove/review these overrides when vendor packages incorporate the fixes.

Migration 49 preserves machine access audit rows when machines or workspaces are
permanently deleted. BEFORE DELETE triggers detach dependent workspace/machine/
service references atomically, before the existing SET NULL/CASCADE actions run;
the composite ownership foreign keys and null-dependency checks remain enforced.
Deleting only a service retains the machine/workspace audit context. Rolling back
removes the triggers but cannot restore references to parents already deleted.

## Persistent assistant chat

The Global Assistant reuses workspace AI settings, PostgreSQL and the existing
HTTP/WebSocket stack. Conversations and incremental events survive navigation
and API restarts. Generation attempts are leased and idempotent, with explicit
retry after interruption. Issue details embed a creator-private chat with a fresh,
bounded issue snapshot and original-team visibility checks. Orchestrator tools let
the assistant list teams and search, read and create issues with the user's own
permissions; it cannot change Dev Machine lifecycles. See [conversation architecture and API](BE/docs/conversations.md)
for scope, authorization, bounds, UI component sources, recovery and checks.

### Telegram assistant channel

Workspace Settings → Telegram configures an encrypted bot token and numeric user-ID
allowlist. Users explicitly link their own Telegram identity to their Kuayle account;
private bot messages and Web chat share conversation records. See
[the Telegram channel contract](BE/docs/telegram.md) for setup, authorization,
webhook and outgoing delivery behavior, migrations and verification.
