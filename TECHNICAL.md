# Kuayle Dev Machines Technical Specification

Production architecture and implementation contract for isolated, multi-container development environments and agentic coding runs.

## Status

The repository includes the Dev Machines control plane, PostgreSQL schema, lifecycle manager, authenticated gateway, provider adapters, runtime images, activity collector, self-hosted Compose profiles, and product UI. The subsystem is disabled by default and must be configured explicitly by a self-hosted operator.

Dev Machines target trusted self-hosted workspaces. Docker hardening reduces risk, but ordinary containers are not a sufficient isolation boundary for mutually hostile tenants. Operators requiring hostile multi-tenant execution must replace the runtime interface with VM or microVM isolation.

## Principles

- A Dev Machine is a logical workspace composed of cooperating containers. It is never a monolithic IDE/browser/agent container.
- Generic machine creation does not require repository, branch, issue, project, or manual TTL input. Friendly random names are case-insensitively unique per creator within a workspace and can be checked through the availability API.
- Repository and environment defaults resolve from issue, then project, then team, then workspace. A manually used machine has affinity to one repository and can host multiple issue worktrees from it. A machine that agent tasks use serves several repositories: each repository is cached once on the machine and every task gets its own worktree. A different environment, or a machine busy with another active task, requires another machine.
- The Kuayle API does not receive the Docker socket.
- Agent, developer, browser, collector, egress, and gateway containers do not receive the host Docker socket. A machine may have its own sandboxed Docker daemon (see [Machine Docker daemon](#machine-docker-daemon)), never the host's.
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
├── docker              optional rootless Docker daemon (workspace policy allow_docker)
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

A machine's own Docker daemon (the `docker` service) is only on the machine network; the containers it runs live in its namespaces and leave the machine network only through the egress proxy (see [Machine Docker daemon](#machine-docker-daemon)).

Compose services for an independent check (migration 68) never join the machine network. They run on a per-check internal network, `kuayle-{routing-key}-check-{run}`, which only the check container also joins, so they have no egress and cannot reach the gateway, other services or the control plane. See [Compose services for checks](BE/docs/agent-tasks.md#compose-services-for-checks).

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
| `{routing-key}-{port}.{DEV_MACHINE_DOMAIN}` | App port of the developer container or of the machine's Docker daemon, 1024–65535 (optional, see below) |

Routing keys are random lowercase hexadecimal strings. The gateway performs an exact configured-domain suffix check and rejects suffix-confusion hosts.

Caddy has one fixed wildcard route. Per-machine Caddy configuration is unnecessary. Teardown removes the database route and private network, so stale hosts return not found without a proxy reload.

The gateway uses Go's reverse proxy with WebSocket upgrades, streaming flushes, unlimited response write duration, bounded dial timeouts, and long-lived connection support. Caddy terminates wildcard TLS and forwards all wildcard machine hosts, including WebSocket upgrades, to the gateway; per-machine Caddy rewrites are not used.

### App ports

A machine exposes any number of TCP ports of its developer container (or,
since migration 89, ports published by containers of its Docker daemon) as
**app ports**, the reviewer's entrypoints for manual QA. Each is served on its own
host, `{routing-key}-{port}.{DEV_MACHINE_DOMAIN}`, with a canonical port number
(1024–65535, no leading zero). Migration 85 stores them in `dev_machine_ports`
(`port`, optional `label` of at most 40 `[A-Za-z0-9 ._-]` characters, `source`
`user`/`agent`/`task`, optional `agent_task_id`), unique per machine and copied
from the earlier one-port `dev_machine_previews`, which it drops. Ports are not
container service rows, so machine lifecycle code is unaffected, and they
outlive tasks. Ports used by any of the machine's own services are refused when
exposed and never routed, so an app port host can never reach code-server or
the terminal without their dedicated protections.

Session-only routes, for the machine's creator (workspace administrators may
also read the list):

- `GET /dev-machines/:machineId/ports` → `{ports, listening, scanned_at}`;
  each port is `{id, port, label, source, agent_task_id, task_identifier,
  host, listening, created_at}`. `task_identifier` is shown only to the task's
  creator when they may see its issue.
- `POST /dev-machines/:machineId/ports` `{port, label?}` → 201; 409
  `PORT_EXISTS`, 422 `PORT_RESERVED` (a service port). The number of ports is
  not limited (migration 89 removed the ten-port limit and `PORT_LIMIT`).
  Each port has a `target`, `developer` or `docker`.
- `PATCH /dev-machines/:machineId/ports/:portId` `{label}` renames it (`null`
  or `""` clears); `DELETE` removes it and revokes that host's sessions.
- `POST /dev-machines/:machineId/ports/:portId/launch` returns the launch
  response of the IDE path (`status`, `launch_url`, 201 ready or 202 starting).

Launch uses the IDE's browser-cookie path: a one-time ticket for the running
developer service row, bound to the port's host, exchanged for a `__Host-`
HttpOnly session cookie valid only on that host. IDE sessions and other ports'
sessions never authorize each other, only the machine creator is admitted,
Kuayle credentials and the gateway session cookie are stripped before proxying,
and upstream cookies stay host-only. A direct visit without a session gets 401;
people open ports from Kuayle. WebSockets (for example hot reload) use the
gateway's upgrade support. TLS certificates are approved on demand only while
the port is exposed and the machine runs. App port hosts share the machine
domain's site with the machine's other hosts; cross-host requests are subject
to the gateway's origin checks, as between any two machine hosts. The gateway
role may read only `machine_id`, `workspace_id`, `port` and `target` of
`dev_machine_ports` (see [gateway-role.sql](selfhosting/gateway-role.sql));
re-run the role setup after upgrading.

The machine manager scans each running machine's developer container every 15
seconds (and around task previews): LISTEN sockets of `/proc/net/tcp` and
`/proc/net/tcp6`, without service ports, are stored in
`dev_machines.listening_ports` and `listening_ports_at`. Loopback listeners
(127.0.0.0/8, `::1`, `::ffff:127.x`) are left out because the gateway reaches
the container over its network address; `kuayle-ports` lists them as "localhost
only" so the app can be restarted bound to `0.0.0.0`. The state file also lists
the machine's service ports as `reserved`, which `kuayle-ports` hides.
On a machine with a Docker daemon the scan also reads the docker service
container's LISTEN sockets, where published container ports listen: they are
stored in `dev_machines.docker_listening_ports` (and in `listening_ports`) and
listed as `docker` in the state file. Each scan moves an exposed port to the
container now listening on it (`target` `docker` when the daemon publishes it,
else `developer`); a new port starts on the daemon when only it publishes the
port. The gateway then proxies to `{routing-key}-docker:{port}`, while tickets
and sessions keep naming the developer service row, so authorization is
unchanged.

Agents and people in the developer container use `kuayle-ports` (IDE and
headless images): `kuayle-ports [list]` shows listening ports and their URLs,
`kuayle-ports expose PORT [--label TEXT]` and `kuayle-ports unexpose PORT`
write a request to `~/.kuayle/ports/requests/` and wait up to 30 seconds for the
answer in `~/.kuayle/ports/state.json` (`{ports:[{port,label,url,source}],
listening, refused:[{port,reason}], updated_at}`). The scan takes the requests
as untrusted input: regular files only (never symlinks), at most 20 per scan and
1 KiB each, strict JSON whose port matches the file name, the same port and
label rules. Every request entry is deleted. Exposed
ports are `agent` ports owned by the machine creator, linked to the task whose
worktree the request came from when known; agents may remove only `agent`
ports. The start scripts install the Kuayle-owned `kuayle-qa` skill for Claude
Code and Codex (`~/.claude/skills/kuayle-qa/SKILL.md`,
`~/.codex/skills/kuayle-qa/SKILL.md`), refreshed on every start, and the
`kuayle-agents` messaging skill, which the agent runner also installs in each
autonomous attempt's container. Autonomous
agent attempts run in separate, temporary agent containers whose servers stop
with the run, so their prompt points the reviewer at the task preview below or
at `kuayle-ports` in the machine terminal instead.

Task previews (see the [agent task contract](BE/docs/agent-tasks.md#app-ports-and-qa))
add `task` ports. Stopping or re-provisioning a machine ends their processes:
task ports are removed and their previews marked stopped, while user and agent
ports are kept. Pausing freezes the processes, which resume with the machine.
The machine page lists ports, detected listeners, and expose, rename, open and
remove controls; a task card starts, stops and opens its app.

## Sudo in machines

Workspace administrators can allow passwordless `sudo` in the developer
containers (IDE and terminal) of new machines, for example to `apt-get install`
system packages. It is off by default: migration 93 adds
`dev_machine_workspace_policies.allow_sudo` (Settings → Dev Machines → policy)
and `dev_machines.sudo_enabled`, which records the switch when a machine is
created. A policy update that omits `allow_sudo` keeps the current value, and a
workspace import resets it to off. Once it is turned off, spawning or starting
a machine created with sudo fails with `sudo_not_allowed` (the machine must be
recreated): its containers keep the configuration they were created with.

**Containers.** Both developer images install `sudo` with an
`/etc/sudoers.d/kuayle` rule (`NOPASSWD:ALL` for UID 1000), inert under the
default `no-new-privileges` and `CapDrop: ALL`. For `sudo_enabled` machines,
`secureHostConfig` gives `ide` and `terminal` containers a writable root
filesystem, no `no-new-privileges`, and Docker's default capability set minus
`NET_RAW`. They are never privileged, get no `NET_ADMIN`/`SYS_ADMIN`/`CapAdd`,
no host mounts or Docker socket, keep the configured seccomp/AppArmor profiles,
resources and machine network, so egress still goes through the machine's
proxy. Root-filesystem changes last until the container is recreated; only
`/workspace` (and the in-memory home until a restart) is kept otherwise.

**Agent attempts stay locked.** Agent containers, collector, browser and egress
never get these relaxations. Autonomous attempts run unattended on prompts that
can include untrusted issue, comment and repository content; root there would
widen what a prompt injection can do (install persistent tooling, read every
file regardless of ownership) without a person to notice, while agents already
have everything they need in their images. The agent images ship no `sudo`;
attempts of the providers that run on the headless image keep its sudoers rule
inert under the locked configuration, and their `PATH` omits the workspace npm
prefix below, so they run the image's pinned CLIs. Anything an agent writes to
`/workspace` (including that prefix and Git hooks) can still run later in a
person's terminal, with sudo if the machine has it; review agent changes as
before.

**Upgrading CLIs without sudo.** Both developer images point npm's global
config at `/etc/npmrc` (`NPM_CONFIG_GLOBALCONFIG`), which sets
`prefix=/workspace/.kuayle/npm-global` and lets Claude Code's install script
run (`allow-scripts=@anthropic-ai/claude-code`), and put
`/workspace/.kuayle/npm-global/bin` first on `PATH`. `npm i -g
@anthropic-ai/claude-code` or `claude update` therefore works on every machine,
with or without sudo, and the upgrade survives restarts on the workspace volume
(the terminal, IDE and agent containers of a machine share it; agents keep
using their images' pinned CLIs). Project and `~/.npmrc` settings override `/etc/npmrc`.

`TestSudoMachinesGetRootWithoutLeavingTheSandbox`
(`KUAYLE_SUDO_INTEGRATION=1`, `KUAYLE_TEST_DISK_QUOTA_MODE=monitored` on a
developer machine, and the `KUAYLE_TEST_{HEADLESS,COLLECTOR,EGRESS}_IMAGE`
images) spawns a real headless machine and checks root, the capability set,
that root cannot bypass the egress proxy, global npm installs, a restart, and
that agent and non-sudo containers cannot use sudo.

## Machine Docker daemon

Workspace administrators can give machines their own Docker daemon so people
and agents run real `docker compose` (builds, volumes, `env_file`, profiles,
any number of services). It is off by default: migration 89 adds
`dev_machine_workspace_policies.allow_docker` (Settings → Dev Machines →
policy). Machines created while it is on get a `docker` service; once it is
turned off, spawning or starting such a machine fails with `docker_not_allowed`
(the machine must be recreated), and a workspace import resets it to off. A
policy update that omits `allow_docker` keeps the current value.

**Container.** `kuayle-{routing-key}-docker` runs `DEV_MACHINE_DOCKER_IMAGE`
(`devmachine/docker`: the official `docker:29.9.2-dind-rootless` image pinned by
digest, plus `kuayle-docker-start` and a health check). The image takes Alpine
security updates, drops the client plugins and `ctr` it does not need, and
replaces runc and the containerd runc shim with builds from the same release
commits whose `golang.org/x/net` is raised past published advisories. dockerd runs rootless as
UID 1000 under rootlesskit (slirp4netns networking, builtin port driver,
overlayfs on its data volume). The container is never privileged and has no
host mounts, socket or published host ports, a read-only root filesystem,
tmpfs `/run`, `/tmp`, `/certs` and home, 40% of the machine's CPU and memory (at
least 512 MB) and at least 1,024 PIDs. Rootless Docker needs, and gets only for
this container:

- `CAP_SETUID` and `CAP_SETGID` (all others dropped) and no `no-new-privileges`,
  because the setuid `newuidmap`/`newgidmap` helpers map its 65,536 subordinate
  IDs (host UIDs 100000–165535, never root);
- the `/dev/net/tun` device for slirp4netns;
- `seccomp=unconfined`, `apparmor=unconfined` and unmasked `/proc`/`/sys`
  paths (`systempaths=unconfined`), because it creates user, mount and network
  namespaces and mounts `proc`/`sys` inside them, which the default profiles
  deny. Each relaxation was verified necessary with Docker 29.

`DEV_MACHINE_DOCKER_SECCOMP_PROFILE` and `DEV_MACHINE_DOCKER_APPARMOR_PROFILE`
replace the two profiles (for example with an AppArmor profile allowing
`userns`, which Ubuntu 23.10+ hosts restricting unprivileged user namespaces
require). With `DEV_MACHINE_DOCKER_RUNTIME=sysbox-runc` (preferred where Sysbox
is installed) the container runs under that runtime and none of the profile or
path relaxations apply; that path has not yet been exercised on a Sysbox host.

**Network and TLS.** The daemon is only on the machine network, as `docker`
and `{routing-key}-docker`. It never joins the egress or control-plane
networks, so neither it nor its containers have a direct route out: registry
pulls use `HTTP(S)_PROXY` (the machine's egress proxy, with its private-address
blocking and allow/deny lists), and the Docker CLI's `~/.docker/config.json`
gives builds and containers the same proxy (`noProxy` covers localhost, `.local`
and `.internal`). A Compose service calling another one by name through a
proxy-aware HTTP client needs that name in its own `NO_PROXY`. The API listens
only on 2376 with mutual TLS: at machine creation the API issues a per-machine
ECDSA CA (its key is discarded), a server certificate for the two daemon names
and one client certificate, valid one day beyond the machine's expiry, stored
encrypted in `dev_machine_env_vars` (targets `docker` and `docker-client`) and
delivered as scoped secrets. Only the daemon gets the server key; developer
containers and agent attempts get the client material, which the start scripts
write to the in-memory `~/.docker` (mode 0600) before unsetting it; agent
output redacts the client key. Developer containers have `DOCKER_HOST`,
`DOCKER_TLS_VERIFY=1` and `DOCKER_CERT_PATH`; the browser, collector and egress
containers get nothing. Port 2376 is a service port, never an app port.

**Storage.** The daemon's data root is a dedicated volume,
`kuayle-docker-{routing-key}`, created with the workspace volume's quota mode
and size (a hard XFS quota of the machine's disk size, or monitored). Its usage
is measured at most every two minutes by a short-lived helper container (the
daemon image's `du`, the volume read-only, no network, only
`CAP_DAC_READ_SEARCH`, default seccomp), because files inside belong to the
daemon's user namespace, which machine users control. The size is added to the
workspace usage, so warnings and stops at the machine's disk limit count both
volumes. The workspace volume is mounted at `/workspace`, so build contexts and
bind mounts use the developer container's paths; files written by containers
running as non-root users are owned by subordinate IDs.

**Lifecycle.** Pause, stop and start include the daemon container; a start
reinstalls its TLS secrets and dockerd keeps images, volumes and containers
(containers return per their restart policy). A start with a missing daemon
volume provisions the machine again. Teardown removes the container and the
volume. The Docker CLI 29.9.2 (`devmachine/common/install-docker-cli.sh`, SHA-256
checked) and the Compose 5.6.1 and Buildx 0.38.1 plugins
(`devmachine/common/build-docker-plugins.sh`, built from pinned release commits
with dependency floors) are installed in the IDE, headless and provider agent
images. Advisories with no fixed release are accepted only through
`.trivyignore.yaml`, per package version and with an expiry date.

Independent checks keep the restricted Compose subset (see agent tasks): a
check container cannot join the daemon's networks, and service names resolve
only inside the daemon.

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

The xterm client preserves the PTY byte stream by disabling end-of-line conversion, uses a line height of `1` and letter spacing `0`, and enables the Unicode 11 addon with `allowProposedApi` for full-screen TUIs. End-of-line conversion must remain off because rewriting LF as CRLF returns the cursor to column zero and corrupts TUIs that use LF to move rows while preserving their column. It renders with `@xterm/addon-webgl` and `customGlyphs`, so box-drawing, block and Powerline characters are drawn by xterm and borders join regardless of the font; on WebGL context loss or without WebGL2 it falls back to the DOM renderer (xterm 6 has no canvas renderer), and the terminal container's `data-renderer` attribute records which one is active. The font is JetBrains Mono, self-hosted from `@fontsource/jetbrains-mono` (OFL-1.1) with a system monospace and emoji fallback stack; the client waits up to three seconds for its regular and bold faces before xterm measures cells. Layout-driven fits are debounced (50 ms) and a resize frame reaches ttyd only when columns or rows change, so a dock drag produces one PTY/tmux resize rather than a storm; ttyd resizes the PTY and tmux follows. Binary output frames are passed to xterm as `Uint8Array` views, coalesced into one write per animation frame, and governed by ttyd flow control ([terminal-io.ts](UI/src/lib/features/dev-machines/terminal-io.ts)): every 100 KB a write carries a parse callback, more than 10 outstanding callbacks send ttyd `PAUSE` and fewer than 4 send `RESUME`, so neither the client queue nor xterm's buffer grows without bound; scrollback is 5,000 lines. The developer image installs `ncurses-term`, ttyd runs with `TERM=xterm-256color`, and tmux uses `tmux-256color` with RGB, synchronized-output, styled-underline, hyperlink, strikethrough and overline features for that outer terminal, focus and extended-key events, passthrough, and a 50,000-line history; terminal sessions export `COLORTERM=truecolor`. tmux keeps its default escape time (10 ms in the image's tmux 3.6) and mouse mode stays off: escape sequences cross a WebSocket and may arrive split, and a shorter escape time would turn the tail of a split sequence into typed text.

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

Codex and Claude Code runs accept an optional `model_server_id`, `model` and `thinking`. They are rejected for other providers. A run uses the chosen model server, or the workspace default when it speaks the harness's protocol (OpenAI-compatible for Codex, Anthropic-compatible for Claude Code); without one the harness uses its own sign-in. A model must be offered by the server when its model list is known; thinking levels are `off`, `low`, `high`, and `max`. The server is recorded on the run (`model_server_id`). Codex receives the server and model entirely as argv `-c` overrides (`model_provider="kuayle"`, its `base_url`, `env_key="KUAYLE_MODEL_API_KEY"`, `wire_api="responses"`), so a user-managed `config.toml` cannot redirect a run; model names and URLs are restricted to characters that need no TOML quoting. Claude Code receives the model as argv and the thinking level through the validated non-secret `CLAUDE_CODE_EFFORT_LEVEL` container environment; `off` omits that variable.

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

The user opens authenticated IDE and browser links from Kuayle and attaches native terminal tabs rendered by the main UI. Terminal tabs speak `ttyd.v1` over the dedicated terminal host and attach to tmux sessions inside the developer container. All seven built-in CLIs are available directly from that terminal. Headless machines omit code-server and browser access and expose only the terminal plus agent workflow. Agents run only as [agent tasks](BE/docs/agent-tasks.md) started from an issue chat (or an automation); each attempt is a `dev_machine_agent_runs` record that runs in a separate container autonomously to completion under a timeout. The manual "Run agent" dialog, its run list and trace sheet, and the `agent-runs` HTTP routes were retired; existing run records stay as task attempts.

### Agent Only

An autonomous run receives:

- optional issue/project and checkout linkage;
- repository, base branch, working branch, and workspace path from the selected checkout or machine repository affinity;
- prompt and acceptance criteria;
- allowed commands and forbidden paths;
- exact test-command argv;
- maximum runtime;
- explicitly allowed secret names;
- whether to push the working branch.

Pull requests are opened only by task publication (`publish_agent`), never by the run itself.

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

Idle pause and hard runtime are separate controls. Workspace policy defaults idle pause to 240 minutes; `keep_running=true` bypasses idle pause for that machine. Gateway requests, native terminal WebSockets, API activity touches, checkouts, and agent runs update `last_activity_at`. The manager queues idempotent pause operations for idle running machines, but it does not automatically delete machines. Machines that agent tasks have used stay running while the creator's and the workspace's running machines are within `max_machines_per_user` and `max_concurrent_machines`: ranked by recent activity, only those beyond a cap (for example after an admin lowered it) are idle-paused, least recently active first. Under host pressure a task machine may still be stopped: when a spawn or start waits for host CPU or memory, the manager stops the least recently active machine on the host that has been idle for at least 10 minutes and is not kept running, has no active run, active task or pending operation (operation key `host-capacity:<generation>`, event `machine.reclaimed_for_capacity`), one machine at a time; the host disk floor stops running machines as before. Maximum hard runtime is policy-controlled through `expires_at`; expired machines transition to `expired` and teardown is queued.

Starting a stopped or paused machine is idempotent. When its workspace volume was never recorded (its first spawn failed), the volume no longer exists, or a service container is missing, the start provisions the machine again from its deterministic names and reuses whatever still exists. The developer start scripts accept a workspace volume that already holds Kuayle state (`.kuayle/`, `repos/`, `tasks/`) or the user's work: a repository machine's root checkout is cloned into `.kuayle/root-checkout`, its files are moved into `/workspace` without replacing existing paths, and `.git` is moved last, so an existing `/workspace/.git` means the root is prepared and an interrupted preparation completes on the next start. The working branch is selected only when the root checkout is created; a restart keeps the current branch and local commits.

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

Every task attempt runs inside a tracked [agent session](BE/docs/agent-tasks.md#agent-sessions)
(migration 84, `agent_sessions`): an implementing task keeps one session across
its follow-ups, and its state follows the task's transitions in the same
transaction. Agent reviews of a published pull request run only when requested
from chat, as separate review tasks that never validate, publish or receive a
push credential; see [requested agent reviews](BE/docs/agent-tasks.md#requested-agent-reviews).

Agents of related tasks can [message each other](BE/docs/agent-tasks.md#agent-messaging)
(migration 88): they write `.kuayle/outbox/*.json` files in their worktree, which
the manager takes during its port scan and at the end of each attempt; the API
validates them as untrusted input, records every message (including refused
ones) in `agent_messages`, delivers queued ones in the recipient's next prompt
and posts `comment` messages as issue comments marked with the sending task.
Admins and task creators can hold, release or drop messages and hold whole
conversations before delivery; every action is audited.

A task also has a [live channel](BE/docs/agent-tasks.md#live-channel)
(migration 95, `agent_live_messages`) between its creator (or the orchestrator
acting for them) and its running agent. Queued messages reach a running
attempt within seconds as `<seq>.json` files in a per-run Docker volume mounted
read-only at `/run/kuayle/inbox` in the agent container, outside the worktree;
the manager fills it through a writer container that is never started. Claude
Code receives them after each tool call (and before it stops) through a
`PostToolUse`/`Stop` hook installed in the agent image's managed settings.
Without a running attempt they go into the next attempt's prompt. During an
attempt the manager also collects the outbox every 5 seconds, and files
addressed `{"to":"orchestrator"}` (optionally `"kind":"question"`) are recorded
in the same channel as untrusted text.

[Automations](BE/docs/agent-tasks.md#automations) (migration 69) delegate matching issues on a label or status trigger, optionally on a UTC schedule. Runs need approval unless a rule is unattended and the workspace policy allows unattended automations; every start acts for a recorded user under that user's current permissions.

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

A `custom` size picks CPU (1, 2, 4, 6, 8, 12 or 16 vCPU) and memory (2, 4, 8, 12, 16, 24, 32, 48 or 64 GB) from the offered options with at least 1 GB per vCPU, and any disk size in 2 GB steps from 20 GB up to the workspace `max_disk_gb` (itself an even number from 20 to 2048); its maximum runtime is 8 hours, capped by the policy. The policy API returns these options as `size_options`.

**Task machines.** Machines Kuayle creates itself for agent tasks (issue delegations, the assistant's delegate tool and automations all go through agent tasks) are headless custom machines sized by the workspace policy's `task_machine_cpu_millis`, `task_machine_memory_mb` and `task_machine_disk_gb` (Settings → Dev Machines → Policy → Task machines). The defaults are 4 vCPU, 8 GB and 20 GB; migration 90 gives existing workspaces the same values (task machines were previously `small`, whose 4 GB left an agent such as Claude Code 1.4 GB and was OOM-killed under builds). The same size rules apply, and the task disk must stay within `max_disk_gb`. Changes affect task machines created afterwards; reused task machines keep their size. The Environment Builder dialog starts with the task machine CPU and memory.

The manager assigns a bounded percentage of the profile to each service and applies Docker `NanoCPUs`, memory, memory-swap, and PID limits (`containerSharePercent` in `BE/internal/machine/docker.go`). On standard machines the IDE (which also serves the terminal) and browser each receive 25%, an agent run 35%, and the collector and egress 5% each. Headless machines (task machines among them) have no IDE or browser, so the agent receives 50% and the terminal container 40%; with a Docker daemon, which takes 40%, the agent and the terminal receive 35% each. Shares are per-container ceilings, not reservations: without Docker the headless shares add up to the machine, and with Docker the agent and terminal ceilings overlap (120%) so the terminal people use by hand is not squeezed below the agent. An 8 GB task machine therefore gives its agent 4 GB (2.8 GB with Docker). Validation checks keep the 35% agent share because their compose services share another 30%. Non-builder workloads use a read-only root filesystem, tmpfs scratch paths, `CapDrop=ALL`, `no-new-privileges`, the Docker default seccomp profile or a configured replacement, and AppArmor where available. Environment Builder deliberately relaxes the developer container root filesystem so owners/admins can install tooling before snapshot, and a workspace that allows sudo relaxes its developer containers (see [Sudo in machines](#sudo-in-machines)). Docker's `json-file` logs rotate at 10 MiB with three files per container, and removal paths also remove anonymous image volumes.

**Live resize.** `PATCH /dev-machines/:machineId` with any of `cpu_millis`,
`memory_mb` and `disk_gb` (omitted values keep the current size; `keep_running`
may be sent alongside) resizes the creator's machine while it is running, paused
or stopped and not changing state (`dev_machine:manage`; 409 `INVALID_OPERATION`
otherwise, including while another resize or a lifecycle operation is queued).
The size is validated like a custom create size (`domain.DevMachineCustomSize`,
1 GiB per vCPU) and the disk against the workspace `max_disk_gb`. Disk changes
need monitored disk quotas (a hard XFS quota is fixed when the volume is
created); a smaller disk is accepted only when the latest measured usage is
below 90 % of it. The API stores the request as `pending_cpu_millis`,
`pending_memory_mb` and `pending_disk_gb` (migration 91) and queues a `resize`
operation at the machine's current generation, so queued work stays current.
The manager leases a resize beside a long agent run or check (which may need the
memory) but never beside lifecycle work. For a running or paused machine it
admits the new size like a start; a size the host cannot fit is rejected with a
reason instead of waiting. It then applies `docker update` (CPU, memory,
memory-swap and PID limits) to every machine container, running or stopped,
with the shares below, restores the previous limits if any update fails, and
moves the pending size into `cpu_millis`/`memory_mb`/`disk_gb` (size `custom`)
and the volumes' `size_limit_bytes`, which the disk monitor reads. A stopped
machine's containers take the new limits at their next start, which admits them.
A rejected resize clears the pending size and records `resize_error`. Events:
`machine.resize_requested` and `machine.resized`.

**Resource limit visibility.** An exited agent or check container is inspected
for Docker's `State.OOMKilled` (set for any out-of-memory kill in its cgroup, a
killed compiler included); the manager also follows Docker `oom` events for
Kuayle containers (terminal, editor, Docker daemon, check services) as they
happen, reporting each container once per minute. A kill records
`resource_limit_kind=memory` with a message such as "Out of memory: the agent
container reached its 1.4 GiB memory limit. Resize the machine or raise the task
machine memory." on the machine (shown as an alert on the machine page until a
resize clears it), the `resource.memory_limit` event, a `Kuayle:` note in the run
or check log, the run's `error_message` and, for a failed task attempt, the task
reason (kept as the final failed reason). Exit code 137 without an
out-of-memory event is reported as a killed process, never as out of memory. A
machine stopped at its monitored disk limit records `resource_limit_kind=disk`.
Process (PID) limit hits are not reported: Docker exposes no event for them.

Workspace policy controls:

- maximum concurrent machines (default 20);
- maximum machines per user (default 20; migration 79 raised existing policies to at least 20/20, admins can lower them);
- maximum daily agent runs;
- maximum runtime;
- idle pause interval (default 240 minutes; bypassed by per-machine `keep_running`);
- maximum per-machine workspace-volume size (2 GB steps up to 2048 GB; migration 90 rounded an odd limit down);
- task machine CPU, memory and disk (default 4 vCPU, 8 GB, 20 GB; older clients that omit them keep the current values);
- allowed providers;
- allowed repositories;
- whether custom providers are allowed;
- whether new machines get their own Docker daemon (`allow_docker`, default off);
- whether the developer containers of new machines allow sudo (`allow_sudo`, default off).

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

### Agent model servers

Settings → Agent configuration manages workspace model servers and which coding agents members may start. A model server has a name, an OpenAI-compatible base URL (used by Codex, which needs the Responses API) and/or an Anthropic-compatible base URL (used by Claude Code), an API key, a model list, a default and optional fast model, a thinking level, and whether it is the workspace default (one per workspace; the first server becomes the default). Creating, updating, deleting and model discovery require `dev_machine:admin`; members with `dev_machine:read` can list servers (never keys) to choose one when starting work. The key is encrypted with `cryptoutil.Encrypt` and `DEV_MACHINE_ENCRYPTION_KEY` and is never returned; an update without `api_key` keeps the stored key.

Model discovery calls `GET <openai>/models` with a bearer key, or `GET <anthropic>/v1/models` with `x-api-key`, through a client that dials only public addresses unless `AI_ALLOW_PRIVATE_NETWORK` is set and never follows redirects. A saved server's key is only sent to the URLs saved with it. Model names are limited to `[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}`; at most 500 are kept.

Servers are workspace data rather than machine snapshots. The IDE and terminal containers receive the default server; an agent run receives only the server recorded on it. The manager copies `KUAYLE_MODEL_API_KEY`, `KUAYLE_MODEL`, `KUAYLE_MODEL_THINKING`, `KUAYLE_MODEL_LIST`, `KUAYLE_MODEL_OPENAI_BASE_URL` and, for an Anthropic-compatible server, `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` and the Claude Code model aliases into `/run/kuayle-secrets`; browser, collector and egress containers never receive them. Rotating a key therefore applies the next time the developer or agent container starts.

The IDE and headless start scripts and the autonomous agent runner create `$HOME/.codex/config.toml` (mode 0600) for the default server only when the file is absent, map Codex `off` to `none`, and leave an existing user-managed configuration unchanged. They also generate `$HOME/.codex/model-catalogs/kuayle.json` from the installed Codex's bundled catalog (`codex-model-catalog.cjs`) for the server's models, so Codex's `/model` picker offers only those; if generation fails, the config omits the catalog and the configured model still works. Because the home directory is a small in-memory tmpfs, the same scripts link `~/.codex/packages` (Codex's managed app-server, about 400 MB per version) to `/workspace/.kuayle/codex-packages`, so the download lives on the machine's workspace volume and is shared by its terminal and agent containers.

### Agent sign-in

A Codex or Claude Code run without a model server (none serves the harness's protocol) uses the agent's own account. An API key secret on the machine (`OPENAI_API_KEY`/`CODEX_API_KEY`, or `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`/`CLAUDE_CODE_OAUTH_TOKEN`) still takes precedence. Otherwise the manager uses the requesting user's stored sign-in, in this order: a saved Claude Code token (Claude Code only), then the credentials file captured from the terminal. With neither, it stops the run with the terminal error `sign_in_required` before starting a container. The task then fails with `sign_in_required=true` (migration `000077_agent_sign_ins`). For Claude Code the task panel first offers a field to paste a token and then, as an alternative, the terminal sign-in; for Codex only the terminal sign-in. The terminal sign-in opens the machine's terminal with `claude /login` or `codex login --device-auth`. Once the user has signed in, `POST /agent-tasks/:taskId/sign-in` (`request_id` makes repeats converge) queues a `capture_sign_in` operation: only the task's creator may request it, only while the task is stopped for sign-in. The manager reads `~/.claude/.credentials.json` or `~/.codex/auth.json` from the machine's terminal (then IDE) container, accepts only a JSON object of at most 64 KiB, encrypts it with `DEV_MACHINE_ENCRYPTION_KEY` into `agent_sign_ins` keyed by workspace, user and agent, and the request starts a new attempt. A missing sign-in answers `409 SIGN_IN_NOT_FOUND`.

Later runs of that user receive the sign-in as the scoped secret `KUAYLE_CLAUDE_CREDENTIALS` or `KUAYLE_CODEX_AUTH`; its token strings are added as further secrets so output redaction covers them. The runner writes the file into the run's in-memory home (mode 0600) and unsets the variable. Because agents may rotate refresh tokens, the runner then runs the harness as a child (forwarding stop signals and keeping its exit status) and afterwards copies the possibly refreshed file to `/workspace/.kuayle/sign-in-return/<run-id>.json` (mode 0600); the manager reads and deletes it right after the run and stores it when it changed. The sign-in is therefore briefly on the user's own machine volume at the end of each such run. Sign-ins are never returned by the API.

**Claude Code token.** `claude setup-token` prints a one-year OAuth token for a Claude Pro, Max, Team or Enterprise subscription; it can only make model requests. A user saves it with `PUT /agent-sign-ins/claude-code/token` (`{"token": "..."}`; one line of 20-1024 printable ASCII characters, surrounding whitespace trimmed; the `sk-ant-oat01-` prefix is not required). It is encrypted with `DEV_MACHINE_ENCRYPTION_KEY` into `agent_sign_ins` with `kind='token'` next to any captured `kind='credentials'` file (migration `000081_agent_sign_in_tokens`; a token is allowed only for `claude-code`; rolling back deletes saved tokens). `expires_at` is set to one year after saving: never later than the real expiry, so a token past it is skipped (the run falls back to the captured file, or asks to sign in again). A run receives it as the scoped secret `CLAUDE_CODE_OAUTH_TOKEN`, redacted from output; nothing is written to the run's home and nothing is handed back. When the task's creator has saved a token, `POST /agent-tasks/:taskId/sign-in` skips the terminal capture and starts the new attempt directly.

`GET /agent-sign-ins` lists the caller's own status per agent (`token` with `saved_at`, `expires_at`, `expired`; `terminal` with `saved_at`), and `DELETE /agent-sign-ins/:provider[?kind=token|credentials]` removes the caller's token, terminal sign-in, or both. These routes are session-only, refused for guests, and always act on the signed-in user: admins cannot read or remove anyone else's sign-ins. Removing a token deletes only Kuayle's encrypted copy; the token stays valid until it expires unless the user revokes it in their claude.ai settings. Codex has no equivalent token and keeps the terminal capture of `~/.codex/auth.json`.

Setting up the Claude Code token (also shown under Settings → Agent configuration → Your sign-ins):

1. On any computer where you are signed in to Claude Code with your subscription, run `claude setup-token` and complete the browser sign-in it opens.
2. Copy the token it prints (it starts with `sk-ant-oat01-`).
3. In Kuayle, open Settings → Agent configuration → Your sign-ins, paste it into **Claude Code token** and choose **Save token**. A Claude Code task stopped for sign-in also accepts it directly: paste it there and choose **Save token and continue**.
4. Renew it yearly: when the card shows **Token expired** (or a task asks you to sign in again), run `claude setup-token` again and paste the new token; it replaces the old one.
5. To stop Kuayle from using it, choose **Remove token**. To revoke the token itself, revoke it in your claude.ai settings.

Migration `000076_agent_model_servers` converts an existing workspace DeepSeek credential into a default server named DeepSeek with the same encrypted key, models and thinking level; rolling back restores it.

## Scoped Settings, Worktrees, and Environments

Development defaults are stored in `dev_machine_scope_settings`. A setting may provide a linked GitHub repository, base branch, development environment, test command, Compose file for checks (migration 68), preview command and preferred port (migrations 71 and 85), preferred coding agent and/or agent instructions (at most 8000 characters) for a workspace, team, project, or issue. Each value is resolved independently in this order: issue, project, team, workspace (migration 65). The preferred agent is the default for task delegation from chat and the task composer when the workspace policy allows it; instructions and the Compose file path are snapshotted onto each task when it is accepted or retried and placed ahead of the task in the native prompt. This lets an issue override only the repository while inheriting an environment, or vice versa.

`GET /dev-machine-repository-defaults` (`dev_machine:read`, optional `issue_id` or `project_id`) reports the effective repository with the scope that supplied it, the value inherited without the most specific scope's own setting, and the linked repositories the policy allows. Issues and projects are read with machine-creation visibility (public teams of the request workspace). A refused default is reported with `allowed: false` rather than replaced. The create-machine dialog preselects the effective repository and sends `repo` only for an explicit choice, so the server keeps applying the configured repository and base branch; the issue defaults dialog names the inherited repository. The assistant's `get_issue` reports an issue's `development_repository`, and `delegate_issue` resolves it when approval is requested: it asks the user only when no scope configures one, refuses a repository the policy disallows, and shows the resolved repository on the approval (the task resolves it again when it runs). A task with no resolvable repository is blocked with a reason naming where to set one.

`GET /dev-machine-check-suggestion` (`dev_machine:manage`, workspace owners and admins) proposes a check command for `repository_id`, or for the repository a `project_id` or `team_id` inherits (404 when none). It lists the root and first-level folders of the linked repository's default branch through the workspace's repository-scoped GitHub App token, skipping hidden, vendored, sample and docs folders and folder names unsafe in a shell, and reads at most 16 `package.json`/`Makefile` files of 256 KiB. Each recognised folder adds one step: `go build ./... && go vet ./...` for `go.mod`; the lockfile's package manager install and the first of the `check`, `typecheck`, `lint`, `test` (not watch or the npm placeholder) and `build` scripts for `package.json`, unless root npm workspaces cover it; `cargo build && cargo test`; `make check`/`make test` when no other manifest is present; `python3 -m compileall -q .` for Python. Steps are joined into `["sh","-c",...]` with `(cd <folder> && ...)`, and the response lists the manifests used (`basis`); it is `null` when nothing is recognised, and GitHub failures return 502 `REPOSITORY_UNREADABLE` without GitHub's message. Nothing is saved: the workspace, team and project development settings show a *Suggest a check* button that fills the field for the admin to review and save.

Machine creation accepts optional issue/project/repository information but does not require it. If no repository resolves, legacy repository fields remain empty and the machine starts as a generic developer workspace. When an issue is opened, `POST /dev-machines/:machineId/checkouts` resolves the issue's development settings, enforces the machine's repository affinity, and creates an idempotent issue worktree under `/workspace/tasks/{issue-key}`. A machine can hold multiple ready manual checkouts from the same repository. Task checkouts (`/workspace/tasks/{issue-key}-{task-id}`) do not use or change the machine's repository affinity, so one task machine serves several repositories; the environment must still match.

Task reservation reuses one of the creator's own machines with the same environment before creating a new one: running machines first, then paused, then stopped ones (the task starts a paused or stopped machine through the ordinary lifecycle checks, including the machine limits), preferring a machine that already caches the task's repository or carries its agent. Machines of other users, environment builders, machines being deleted or changing state, and machines with an active run or another active task are never chosen.

Every repository a machine checks out is cloned once as a bare cache, `/workspace/repos/{owner}/{repo}.git`, and each checkout is a `git worktree` of that cache; earlier machines may also hold flat `/workspace/repos/{owner}-{repo}.git` caches, which existing worktrees keep using. Preparing an existing checkout path again fetches it and keeps its work, and is refused when the path holds another repository. The caches store no credential: each checkout authenticates with an installation token scoped to that one repository, passed through an ephemeral askpass helper, and worktrees use the token-less credential helper that reads the per-run `GITHUB_TOKEN`. All repositories on one machine share its workspace volume, so they share the creator's trust boundary; the repository allowlist is checked for every checkout.

Development Environments are immutable local OCI images created from owner/admin-created Environment Builder machines. Environment Builders run the developer container with a writable root filesystem so tooling can be installed intentionally. Snapshot requests are accepted only while the builder is `paused` or `stopped`; the environment moves `pending -> building -> ready` or `failed`. The snapshot commits the developer container layer with Kuayle environment/workspace labels, may leave a human-readable local tag (`kuayle/dev-environment-{id}:snapshot`), and records the immutable local image ID (`sha256:...`) as both `image_ref` and `image_digest` once ready. Machine creation uses only that immutable local ID, verifies the image labels match the expected workspace/environment, and never registry-pulls a missing local snapshot ID. The repository workspace named volume, `/run/kuayle-secrets`, `/tmp`, and other tmpfs scratch mounts are not part of the image. Selected shell and code-server customization is copied into an image template before commit; files in that template whose names contain `history`, `token`, or `credential` are removed. The complete writable developer layer is still committed, so operators must scrub any other sensitive data before snapshotting.

Environment deletion is two-phase. The API marks an environment `delete_requested`; the manager removes only the immutable local image ID after verifying the Kuayle environment/workspace labels and only then deletes the database record. Operators migrating hosts must move any required local OCI images separately from PostgreSQL and Docker volumes.

## GitHub Integration

Dev Machines use GitHub App installation tokens, not broad personal access tokens. Generic machines may start without a repository; issue checkouts resolve the linked repository through issue, project, team, then workspace development settings. Once a machine has repository affinity, additional manual issue worktrees must use the same linked repository; task worktrees may use any allowed linked repository. Agent runs and publication mint tokens for the checkout's repository; the IDE and terminal receive a token for the machine's root repository.

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

Dev Machines live in Settings → Dev Machines, which has two tabs: **Machines** (the user's machines, manual creation for hand-run development and the admin cleanup of old machines; members and above) and **Policy** (the workspace policy, environments, defaults and automations; editable by owners and admins). The sidebar has no Dev Machines entry. `/:slug/machines` redirects there, keeping an `issue_id` filter, and `/:slug/machines/:machineId` stays the machine page that task cards and issues link to. The machine page exposes status, services, resource samples, cursor-paginated events and logs, checkouts, terminal sessions, app ports, lifecycle/delete controls and **Tasks on this machine**: the agent tasks that used it (active first, then the most recently updated, at most 30), each with its issue, state, agent and a link to its chat. It reads `GET /dev-machines/:machineId/tasks`, a session-only route that answers only the machine's owner (others get 404) and applies the same workspace-membership, task-creator and issue-team checks as the agent queue. Native terminal attachment is implemented in the Kuayle UI with `@xterm/xterm`; ttyd's own web page is not exposed.

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
| `agent_model_servers` | Workspace model servers: protocol URLs, encrypted API key, models, default and fast model, thinking level, default flag |

The migration creates the final scoped-workspace schema directly. Composite foreign keys bind workspace, machine, service, checkout, run, session, and environment tuples so cross-workspace identifiers are rejected by PostgreSQL rather than relying only on application checks. Rolling migration `000033` back is destructive for all Dev Machine control-plane records.

Migration `000051_dev_machine_agents_headless` adds the `dev_machines.profile` value (`standard` or `headless`) and expands only the default workspace-policy provider allowlist to all seven built-ins. Existing workspace policies are not rewritten, so administrators retain their current allowlists until they opt in. The down migration removes the profile column and restores the previous default allowlist.

Migration `000052_dev_machine_deepseek` adds workspace model credentials and persists each agent run's non-secret command environment so a Claude Code effort override survives queueing.

Migration `000089_dev_machine_docker` adds `dev_machine_workspace_policies.allow_docker` (default false), the `docker` service type, `dev_machine_ports.target` (`developer` or `docker`) and `dev_machines.docker_listening_ports`. Its rollback deletes `docker` service rows (tear such machines down first; their containers and volumes are otherwise left for manual removal) and port targets; ports are kept and stay unlimited in number.

Migration `000090_dev_machine_task_resources` adds `dev_machine_workspace_policies.task_machine_cpu_millis`, `task_machine_memory_mb` and `task_machine_disk_gb` (defaults 4000, 8192, 20, applied to existing workspaces), at least 1 GB per vCPU, an even task disk within `max_disk_gb`, and an even `max_disk_gb` (odd limits are rounded down, irreversibly). Its rollback drops the columns and constraints; rounded limits keep their even values.

Migration `000093_dev_machine_sudo` adds `dev_machine_workspace_policies.allow_sudo` and `dev_machines.sudo_enabled` (both default false). Its rollback drops both columns; containers already created with sudo keep their configuration, so delete those machines first.

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
GET    /api/workspaces/:slug/dev-machine-repository-defaults
GET    /api/workspaces/:slug/dev-machine-check-suggestion
PUT    /api/workspaces/:slug/dev-machine-scope-setting
DELETE /api/workspaces/:slug/dev-machine-scope-setting
GET    /api/workspaces/:slug/dev-machine-environments
POST   /api/workspaces/:slug/dev-machine-environments
GET    /api/workspaces/:slug/dev-machine-environments/:environmentId
DELETE /api/workspaces/:slug/dev-machine-environments/:environmentId
GET    /api/workspaces/:slug/dev-machine-providers
GET    /api/workspaces/:slug/agent-model-servers
POST   /api/workspaces/:slug/agent-model-servers
POST   /api/workspaces/:slug/agent-model-servers/discover
PUT    /api/workspaces/:slug/agent-model-servers/:serverId
DELETE /api/workspaces/:slug/agent-model-servers/:serverId

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

GET    /api/workspaces/:slug/dev-machines/:machineId/tasks   (session only)
```

Agent runs are created, read and cancelled only through the session-only agent task routes (see the [agent task contract](BE/docs/agent-tasks.md)); the former manual `agent-runs` routes return 404.

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

## Installable app

The UI is an installable web app (PWA) in production. `UI/static/manifest.webmanifest` names it Kuayle (`start_url` and `scope` `/`, `display: standalone`, the default dark theme's `#111111` as theme and background colour) with 192/512 px icons, a 512 px maskable icon and `apple-touch-icon.png`. The PNGs are generated from `UI/static/favicon.svg` by `UI/scripts/generate-app-icons.mjs` (Playwright's Chromium, deterministic) and committed. `UI/src/app.html` adds the manifest and Apple touch icon links and the iOS home-screen tags; the `theme-color` meta follows the active theme's `--color-bg`.

`UI/src/service-worker.ts` precaches only the built immutable app chunks and public shell files (favicon, manifest, icons, `offline.html`) and serves them cache-first. API requests, WebSockets, requests with credentials headers, other origins, release data and page HTML are never cached; navigations always go to the network, falling back to the static offline page only when it fails ([policy](UI/src/lib/features/pwa/service-worker-policy.ts)). The worker is registered by the app itself (`kit.serviceWorker.register` is off), only in production builds and never under browser automation unless a test sets the `kuayle-service-worker-tests` localStorage key. A new deployment installs in the background and waits: an "update available" toast reloads into it on request, otherwise it activates once every Kuayle tab has closed. The UI Caddyfile serves the manifest as `application/manifest+json` and `service-worker.js` with `Cache-Control: no-cache`.

Launched as the installed app (display mode `standalone`, `fullscreen` or `window-controls-overlay`, or iOS `navigator.standalone`), the page locks zoom: [installed-viewport](UI/src/lib/features/pwa/installed-viewport.ts) adds `maximum-scale=1, user-scalable=no` to the viewport meta (which also stops iOS focus zoom on small inputs), sets `data-installed-app` on `<html>` so `app.css` applies a zero-specificity `touch-action: pan-x pan-y` (no pinch or double-tap zoom; explicit `touch-*` utilities still win), and cancels Safari pinch `gesturestart`/`gesturechange` events. Browser tabs keep the default viewport and normal zoom.

Installation requires HTTPS (or `localhost`). Chrome and Edge offer Install in the address bar or menu; on iOS use Safari's Share → Add to Home Screen.

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
| `DEV_MACHINE_AGENT_GIT_NAME`, `DEV_MACHINE_AGENT_GIT_EMAIL` | Default commit author for agent task publications (default `Kuayle Agent <agent@kuayle.invalid>`). A workspace can override it with a username and verified email in its coding-agent policy (migration 70), so GitHub attributes agent commits to that account. Pull requests are opened by the GitHub App installation. Control characters and angle brackets are rejected |
| `DEV_MACHINE_DOCKER_GID` | Numeric host group owner of the mounted Docker socket; obtain it on Linux with `stat -c '%g' /var/run/docker.sock` (Docker Desktop commonly uses `0`) |
| `DEV_MACHINE_HEADLESS_IMAGE` | Headless developer and new built-in provider image (default `ghcr.io/kuayle/dev-machine-headless:0.1.0`) |
| `DEV_MACHINE_DOCKER_IMAGE` | Machine Docker daemon image (default `ghcr.io/kuayle/dev-machine-docker:0.1.0`), used when a workspace policy allows Docker |
| `DEV_MACHINE_DOCKER_RUNTIME` | Optional OCI runtime for daemon containers, e.g. `sysbox-runc`; then no profile relaxations are applied |
| `DEV_MACHINE_DOCKER_SECCOMP_PROFILE`, `DEV_MACHINE_DOCKER_APPARMOR_PROFILE` | Daemon container profiles without a runtime (default `unconfined`; `default` keeps Docker's, which breaks rootless Docker) |
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
- A machine's own Docker daemon (opt-in, `allow_docker`) runs with relaxed seccomp/AppArmor, unmasked proc/sys and `SETUID`/`SETGID` unless `DEV_MACHINE_DOCKER_RUNTIME` selects Sysbox. Machine users control a rootless daemon in its own user namespace, so a kernel user-namespace or mount vulnerability is more exposed from that container than from the other, fully confined, machine containers. It is never privileged and reaches nothing beyond the machine network and the egress proxy.
- With sudo allowed (opt-in, `allow_sudo`), machine users are root in their IDE and terminal containers, with Docker's default capabilities minus `NET_RAW`, a writable root filesystem and no `no-new-privileges`. It is never privileged or given `NET_ADMIN`/`SYS_ADMIN`, and egress still goes through the proxy, but root in a container exposes more kernel surface (and anything readable inside the container regardless of file ownership) than the confined default. Agent attempts never get sudo.
- Compose support for checks is a constrained subset run by the manager, not `docker compose`. Builds, mounts, privileges, host networking and published ports are rejected, because arbitrary untrusted Compose execution needs an isolated Docker daemon. Image pulls run on the host daemon, outside the egress proxy, so `DEV_MACHINE_COMPOSE_IMAGES` restricts them to official Docker Hub images by default.
- The default model server's key is delivered into every developer container, and a run's server key into its agent container, so any member who can create a machine can read it from a terminal or have an agent reveal it. The API never returns it, but treat model server keys as shared with those members (SECURITY_AUDIT.md KS-08).
- Command and filesystem collection is best-effort and can be bypassed by trusted workspace code.
- Provider adapters can construct interactive invocations, but agent tasks queue autonomous agent runs and no interactive-agent attach endpoint is exposed. Direct interactive use of the built-in CLIs is available through Kuayle's native xterm terminal; ttyd's web page is not exposed.
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
bounded issue snapshot and original-team visibility checks. Conversations are
never shared; private folders and visible projects only tag and filter the
creator's list, and issue chats follow their issue's project. Orchestrator tools let
the assistant list teams, search, read, create and update issues, read and list
agent work, link an issue or project to a repository and request approval for
delegation, an agent review of a published pull request, a follow-up on a task
or a merge, start or stop a task's preview and expose or list its machine's app
ports (start and expose need approval), with the user's own permissions; it
cannot change Dev Machine lifecycles. One reply runs up to 10 minutes, 400 tool calls and 10 issue
changes, with a bounded per-round context. Comments by workspace members on an
issue whose task awaits review queue a bounded follow-up attempt
([agent tasks](BE/docs/agent-tasks.md#kuayle-issue-comments)). See [conversation architecture and API](BE/docs/conversations.md)
for scope, authorization, bounds, UI component sources, recovery and checks.
Settings → Assistant lists every built-in tool (`GET /assistant-capabilities`,
derived from the tool registry) with its description, approval requirement,
availability on the server and workspace switch. Owners and admins turn tools
off (`PUT /assistant-capabilities/:name`, stored in `assistant_tool_settings`);
a disabled tool is not offered to the model, is refused if called anyway, and a
pending approval for it fails when approved. Members see the list read-only.
See [tool switches](BE/docs/conversations.md#tool-switches-settings--assistant).

### Images in conversations

Users attach PNG, JPEG, WebP or GIF images (sniffed, at most 10 MB each and 6
per message) by button, paste or drag-and-drop; Telegram photos are stored the
same way. Images live in the configured upload storage, are recorded by
migration 82 and are served only to the conversation's readers through an
authenticated, `nosniff`, script-sandboxed endpoint. With the AI setting
"Model can see images" (off by default, with a probe) the newest images are sent
as OpenAI-compatible `image_url` parts; otherwise the model gets a text note.
The `attach_images_to_issue` tool copies them into issue assets of the issue's
team, and task attempts copy the issue's images into
`.kuayle/attachments/` in their worktree (excluded through the repository's
`info/exclude`) and list them in the prompt. The machine manager therefore
needs read access to upload storage: the Compose files mount the `uploads`
volume read-only and pass the storage settings. See
[conversation images](BE/docs/conversations.md#images) and
[task images](BE/docs/agent-tasks.md#issue-images).

### Telegram assistant channel

Workspace Settings → Telegram configures an encrypted bot token and numeric user-ID
allowlist. Users explicitly link their own Telegram identity to their Kuayle account;
private bot messages and Web chat share conversation records. See
[the Telegram channel contract](BE/docs/telegram.md) for setup, authorization,
webhook and outgoing delivery behavior, migrations and verification.

### Voice transcription

Transcription only (no translation or speech output). The optional `voice`
Compose profile runs [an internal Python service](voice/kuayle_voice/server.py)
with faster-whisper (CTranslate2, int8 on CPU) and Vosk, decoding with ffmpeg.
The workspace owner picks one model in **Settings → Voice** (migration 83,
`workspace_voice_settings`; no row means off): Off, faster-whisper base, small,
medium or large-v3-turbo, or Vosk small English / Italian. Members read
`GET /workspaces/:slug/voice` (whether the microphone works; the owner also
gets download progress and errors); `PUT /voice` is owner-only and session-only
and asks the service to download the model.

Models come only from their publishers, pinned in one place,
[`voice/kuayle_voice/catalog.py`](voice/kuayle_voice/catalog.py): the
`Systran/faster-whisper-*` repositories and the large-v3-turbo conversion
faster-whisper maps its name to (served by Hugging Face as
`dropbox-dash/faster-whisper-large-v3-turbo`), each at a fixed commit, and the
Vosk archives on alphacephei.com. Every file is checked against its pinned size
and digest: SHA-256 for Hugging Face LFS files and Git blob SHA-1 for small
files (the hashes Hugging Face publishes), and a SHA-256 recorded from the
official Vosk archive, which publishes no checksum. Redirects are followed only
to HTTPS hosts under `huggingface.co`, `hf.co` and `alphacephei.com`; archives
are extracted without links or paths outside the model directory. Downloads run
one at a time into a partial directory that becomes the model only after
verification; a failed one can be retried from the settings page.

`POST /workspaces/:slug/conversations/:id/transcriptions` (session-only) takes
one raw recording (the browser's WebM/Opus or, on iOS Safari, MP4/AAC). It
authorizes the conversation before reading the body, accepts audio content
types only, bounds the body to 20 MB and returns `{text}` for the composer. The
user reviews the transcript and sends it as an ordinary message; nothing is
stored. The composer microphone uses MediaRecorder, shows a timer, stops by
itself at 5:00 and can be cancelled; it is hidden unless the workspace model is
ready. Telegram voice notes and audio messages are covered by the
[Telegram contract](BE/docs/telegram.md#voice-notes-and-audio-messages).

The service identifies the container from its first bytes (OGG, WebM/Matroska,
MP4/M4A, MP3, AAC, WAV, FLAC), decodes with an ffmpeg demuxer and protocol
whitelist to 16 kHz mono and measures the decoded length, refusing anything
over 5 minutes (one second of encoder padding tolerated) even when headers
carry no duration. One transcription runs at a time with at most four waiting
(`VOICE_QUEUE`); further requests get `503 busy`. Audio lives in memory and a
private temporary file only while it is decoded; neither audio nor transcripts
are logged. One engine is loaded at a time; switching models reloads it.

| Variable | Service | Purpose |
|---|---|---|
| `VOICE_INTERNAL_TOKEN` | backend, voice | Shared bearer token, at least 32 characters. The service refuses to start without it; voice stays off without it. |
| `VOICE_URL` | backend | Internal service address (Compose default `http://voice:8000`). Operator setting, so private addresses are allowed; redirects are not followed. |
| `VOICE_CPU_THREADS` | voice | Threads for faster-whisper (0 = automatic). |
| `VOICE_MAX_SECONDS`, `VOICE_MAX_BYTES`, `VOICE_QUEUE` | voice | Optional lower limits (seconds cannot exceed 300) and queue length. |

In self-hosting Compose the service has no published port: it shares the
internal `voice-control` network with the backend only and uses `voice-egress`
to download models into the `voice_models` volume; it runs read-only as a
non-root user with all capabilities dropped. The development Compose file
publishes it on loopback port 8765 for a backend running on the host. The
backend's client uses a 5-second status timeout and a 5-minute transcription
timeout covering the queue. Tests: `cd voice && python -m pytest` (fake engines,
no model downloads; ffmpeg cases skip without ffmpeg), Go service/HTTP/Telegram
tests with a fake voice server, and Playwright with a fake MediaRecorder.
