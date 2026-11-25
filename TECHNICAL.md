# Kuayle Dev Machines Technical Specification

Production architecture and implementation contract for isolated, multi-container development environments and agentic coding runs.

## Status

The repository includes the Dev Machines control plane, PostgreSQL schema, lifecycle manager, authenticated gateway, provider adapters, runtime images, activity collector, self-hosted Compose profiles, and product UI. The subsystem is disabled by default and must be configured explicitly by a self-hosted operator.

Dev Machines target trusted self-hosted workspaces. Docker hardening reduces risk, but ordinary containers are not a sufficient isolation boundary for mutually hostile tenants. Operators requiring hostile multi-tenant execution must replace the runtime interface with VM or microVM isolation.

## Principles

- A Dev Machine is a logical workspace composed of cooperating containers. It is never a monolithic IDE/browser/agent container.
- Generic machine creation does not require repository, branch, issue, project, or manual TTL input. Friendly random names are case-insensitively unique per creator within a workspace and can be checked through the availability API.
- Repository and environment defaults resolve from issue, then project, then team, then workspace. A manually used machine has affinity to one repository and can host multiple issue worktrees from it. A machine that agent tasks use serves several repositories: each repository is cached once on the machine and every task gets its own worktree. Tasks in the same environment can share a suitable running machine under the [task machine reuse](#task-machine-reuse) rules.
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
| Machine Manager | Host privileged | Docker networks, volumes, containers, resource limits, secrets handoff, lifecycle, cleanup |
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

Agent containers are created on demand, one per run (`kuayle-<routing key>-agent-<run>`). Tasks can share a machine up to its [task limit](#task-machine-reuse), each in its own worktree. A user's own (non-task) agent run keeps the machine to itself (partial unique index `idx_dev_machine_agent_runs_one_active_manual`).

## Network Isolation

The manager creates `kuayle-machine-{routing-key}` with Docker's `Internal` option. Only that machine's services and the trusted Machine Gateway join it.

No machine workload joins the PostgreSQL, Redis, API, or Compose control network. The egress container is the only machine service also attached to the shared external `kuayle-machine-egress` network.

Chrome's debugging endpoint binds only to browser-container loopback. Browser-enabled machines receive a dedicated encrypted credential scoped to the browser and collector services; an authenticated relay exposes only the read-only target-list endpoint needed for navigation telemetry and does not proxy CDP control or WebSocket endpoints.

Workloads receive `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, and their lowercase spellings (curl, wget and apt read only `http_proxy` for plain HTTP), pointing to their machine's egress container. The developer images keep these variables through `sudo` (`env_keep` in `/etc/sudoers.d/kuayle`), so `sudo apt-get update` and package installs on a machine that allows sudo reach the distribution mirrors through the proxy. The proxy:

- rejects IP-literal destinations;
- resolves hostnames itself and rejects private, loopback, link-local, multicast, and unspecified addresses;
- permits ordinary HTTP on port 80 and HTTPS on port 443, while allowing CONNECT tunnels only to port 443;
- applies optional suffix-based domain allow and deny lists;
- resolves and dials the validated public address to reduce DNS-rebinding exposure.

An empty domain allowlist permits public Internet destinations while still blocking private address ranges. Operators can fail closed by configuring an explicit allowlist.
An explicit allowlist must include the Kuayle ingestion host plus every Git and provider API hostname required by configured agents; for OpenCode with an OpenCode API key (`OPENCODE_API_KEY`) that includes `opencode.ai`.

A machine's own Docker daemon (the `docker` service) is only on the machine network; the containers it runs live in its namespaces and leave the machine network only through the egress proxy (see [Machine Docker daemon](#machine-docker-daemon)).

Compose services for a manual check (migration 68) never join the machine network. They run on a per-check internal network, `kuayle-{routing-key}-check-{run}`, which only the check container also joins, so they have no egress and cannot reach the gateway, other services or the control plane. See [Compose services for checks](BE/docs/agent-tasks.md#compose-services-for-checks).

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
since migration 89, ports published by containers of its Docker daemon, and
since migration 136 the port of a task's preview container) as
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
  Each port has a `target`: `developer`, `docker` or, for a task preview,
  `preview`.
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
unchanged. A task's preview port (`source` `task`) is the exception: its
`target` is `preview` and the gateway proxies to the task's preview container,
`{routing-key}-preview-{port}`, a name derived from the port alone, so the
gateway role needs no further column. The scan asks each running preview
container only whether its own port listens (never for port requests) and adds
it to `listening_ports`. Such a port moves only between `preview` and `docker`
(a preview command may publish it with `docker compose up`), never to
`developer`: a process in the developer container on the same number does not
receive the preview's traffic. Tickets and sessions are unchanged here too.

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
Code, Codex and OpenCode (`~/.claude/skills/kuayle-qa/SKILL.md`,
`~/.codex/skills/kuayle-qa/SKILL.md`, `~/.config/opencode/skill/kuayle-qa/SKILL.md`), refreshed on every start, and the
`kuayle-agents` messaging skill, which the agent runner also installs in each
autonomous attempt's container. Autonomous
agent attempts run in separate, temporary agent containers whose servers stop
with the run, so their prompt points the reviewer at the task preview below or
at `kuayle-ports` in the machine terminal instead.

Task previews (see the [agent task contract](BE/docs/agent-tasks.md#app-ports-and-qa))
add `task` ports. A preview runs agent-written code, so it runs in a container
of its own, `kuayle-{routing-key}-preview-{taskId}`, created like a check
container: the developer image and the workspace volume, but none of the
developer container's secrets, agent sign-ins or sudo (read-only root, no
capabilities, `no-new-privileges` even where the workspace allows sudo), and
only `PORT`, `HOST`, the proxy and its temporary directory in its environment
(plus the Docker daemon client on a machine with a daemon, as attempts get).
It still serves that code to the creator's browser, so it never starts by
itself: only the task's creator starts it, from the task card or by approving
the assistant's `start_preview`. Stopping or re-provisioning a machine removes
its preview containers:
task ports are removed and their previews marked stopped, while user and agent
ports are kept. Pausing pauses the containers, which resume with the machine.
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
before. A task preview is not such a run: Kuayle executes the worktree's
preview command in the task's own preview container, which is never relaxed
for sudo and holds none of the developer container's secrets (see
[App ports](#app-ports)), and only when the task's creator asks, never when a
task reaches review.

Assistant command runs (`run_command`, `run_command_write`) use that same
locked container configuration, so a machine's sudo setting never applies to
them. Packages installed with sudo in the terminal stay in that container and
do not reach command runs; bake them into a Development Environment instead
(see [system packages](BE/docs/conversations.md#system-packages)). A
`run_migration` run is the same locked container with one injected variable:
the named migration action's connection secret, decrypted at run time and
redacted from the stored output (see
[database migrations from chat](BE/docs/conversations.md#database-migrations-from-chat)).

**Upgrading CLIs without sudo.** Both developer images point npm's global
config at `/etc/npmrc` (`NPM_CONFIG_GLOBALCONFIG`), which sets
`prefix=/workspace/.kuayle/npm-global` and lets Claude Code's install script
run (`allow-scripts=@anthropic-ai/claude-code`), and put
`/workspace/.kuayle/npm-global/bin` first on `PATH`. `npm i -g
@anthropic-ai/claude-code` or `claude update` therefore works on every machine,
with or without sudo, and the upgrade survives restarts on the workspace volume
(the terminal, IDE and agent containers of a machine share it; agents keep
using their images' pinned CLIs). Project and `~/.npmrc` settings override `/etc/npmrc`.

**Agent CLI updates.** Releases pin every built-in agent CLI in the images
(`scripts/ci/upgrade-agent-clis.mjs` raises the pins at release time, to the
newest stable versions published at least 48 hours earlier, and never lowers
them; see [release agent CLI refresh](scripts/ci/README.md#release-agent-cli-refresh)).
Each image build, and the IDE and headless smoke tests, run
`devmachine/common/check-agent-cli-flags.sh`: it fails when a CLI's help no
longer lists a flag or fixed value the adapters in `BE/internal/agent/providers.go`
pass (its list is kept equal to the adapters by
`TestAgentCLIFlagContractMatchesTheAdapters`). Between
releases the machine manager keeps the npm-distributed ones current for every
machine: every `DEV_MACHINE_AGENT_CLI_REFRESH_HOURS` (default 6; 0 turns it off)
it runs a helper container from the headless image that installs the latest
Claude Code, OpenCode (`@opencode/cli`, the 2.x line), Codex, Pi and Qwen Code published at least
`DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS` ago (default 48; `npm install --before`,
so a compromised release has time to be withdrawn; install scripts only for the
packages the images allow) into a new release directory of the
`kuayle-agent-clis` volume, checks that each CLI starts (`--version`) and fits
the release (below), and atomically switches the volume's `current` link to it,
keeping the previous release. Unchanged versions are discarded. The helper runs as root (the volume's
owner) without capabilities, with `no-new-privileges` and a read-only root
filesystem, on the shared `kuayle-machine-egress` network, with no machine data
or secrets: the CLI volume is its only mount. Agent runs of built-in providers and the developer terminal and IDE
created afterwards mount the volume read-only at `/opt/kuayle/agent-clis` with
`/opt/kuayle/agent-clis/current/node_modules/.bin` ahead of the image's CLIs on
`PATH` (after `/workspace/.kuayle/npm-global/bin` in developer containers, which
agents do not have), so neither an agent nor a person can change what other runs
execute; a switch applies to containers that already mount it. Crush, Goose and
Kimi are release binaries with pinned checksums and keep the image's versions;
custom agents keep their configured image (`BE/internal/machine/agent_clis.go`).

The volume outlives releases, so its CLIs are used only while they **fit the
release**: every package installed at the headless image's version or newer,
each command linked from its own package (an old or renamed package such as
OpenCode 1's `opencode-ai` does not count), and the headless image's
`check-agent-cli-flags.sh` passing with the volume's CLIs first on `PATH`. The
manager checks this when it starts, before it leases any work, so a deploy (the
release updater or `update.sh` restarting it with new images) never runs this
release's argv on CLIs a previous release installed. The check runs the same
helper with no network and installs nothing, after removing any refresh helper
a previous manager left running. Every refresh checks the current
release again first, and checks the new CLIs before it switches: new CLIs that
do not fit are discarded, and a current release that does not fit is set aside
(the `current` link is renamed `rejected`), so `PATH` falls through to the
image's CLIs at once, also in containers that are already running. A set-aside
release stays on the volume until the next switch, for runs that started from
it. Agent runs put the volume on `PATH` only while this manager's last check or
refresh found it to fit, so until the first check, after a set-aside, or when
the check could not run, they use their image's CLIs; the run trace then
records no shared CLI version. Failures are logged and retried after an hour;
machines keep the current CLIs while they fit, and their image's otherwise.

An upgrade replaces only CLIs. What machines keep: everything on the workspace
volume (repositories and task worktrees, task sessions under
`/workspace/.kuayle/agent-sessions/<task>` with Claude Code transcripts and
OpenCode's session store, the Codex package cache in
`/workspace/.kuayle/codex-packages`, and a person's own global npm installs in
`/workspace/.kuayle/npm-global`, which stay first on their terminal's `PATH`
and are never used by agent runs); the sign-ins Kuayle stores, written into the
in-memory home on every start (a login made only in a terminal lives in that
home, as before); and notebooks and agent settings, which are in the
database. Only `releases/`, `current` and `rejected` on the
`kuayle-agent-clis` volume change.

**Environment status.** Both developer images delete apt's package index
(`/var/lib/apt/lists`) at the end of their build to stay small, so `apt install`
finds no package until `apt-get update` runs. The machine dashboard's
"Environment status" card says so: the machine manager probes the running
developer container (as UID 1000, 3-second timeout) on its port-scan tick, at
most once a minute per container, and records `absent` (no `*Packages*` list;
a failed update leaves `lock` and `partial/`), `present` with the newest list's
time, or `unknown` when the probe fails, in `dev_machine_environment_checks`.
The row describes one container: it is deleted when the machine stops, pauses
or is torn down, a row of an earlier container reads `unknown`, and nothing
runs in a machine that is not running. It never changes machine status or
service health. `GET /dev-machines/:machineId/environment-status`
(`dev_machine:read`) only reads the row. For a running `sudo_enabled` machine,
its creator may `POST …/environment-status/package-index/refresh`
(`dev_machine:manage`; 403 `SUDO_DISABLED` without sudo, 409 when not running):
it queues one `refresh_package_index` operation (never retried; 202) whose
`sudo -n apt-get update` runs for at most 120 seconds; the exit code and the
last 40 output lines are recorded on the row, the index is probed again, and
the UI polls GET until the refresh finishes. A failing refresh (mirrors are
reached only through the egress proxy) is shown, not hidden
(`BE/internal/machine/environment.go`).

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
(`devmachine/docker`: the official `docker:29.10.0-dind-rootless` image pinned by
digest, plus `kuayle-docker-start` and a health check). The image takes Alpine
security updates, drops the client plugins and `ctr` it does not need, and
replaces runc, the containerd runc shim and rootlesskit with builds from the same
release commits whose `golang.org/x/net` is raised past published advisories. dockerd runs rootless as
UID 1000 under rootlesskit (slirp4netns networking, builtin port driver,
overlayfs on its data volume). The container is never privileged and has no
host mounts, socket or published host ports, a read-only root filesystem,
tmpfs `/run`, `/tmp`, `/certs` and home, 40% of the machine's CPU and memory (at
least 512 MB; the whole machine's CPU and memory when it shares its resources, see
[shared machine resources](#shared-machine-resources)) and at least 1,024 PIDs. Rootless Docker needs, and gets only for
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
volume. The Docker CLI 29.10.0 (`devmachine/common/install-docker-cli.sh`, SHA-256
checked) and the Compose 5.6.1 and Buildx 0.38.1 plugins
(`devmachine/common/build-docker-plugins.sh`, built from pinned release commits
with dependency floors) are installed in the IDE, headless and provider agent
images. Advisories with no fixed release are accepted only through
`.trivyignore.yaml`, per package version and with an expiry date.

Manual checks keep the restricted Compose subset (see agent tasks): a
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

Opening a paused machine service queues an idempotent resume operation and returns a `resuming` response with `retry_after_seconds`; the UI polls/retries. Queued or spawning machines return `pending`. Stopped, destroyed and invalid states do not auto-resume.

Machine access decisions are persisted in `dev_machine_access_logs` without query strings.

### Public demo mode

With `PUBLIC_DEMO_MODE` on, only the users listed in `SYSADMINS` may use Dev Machines; an empty list admits no one. The rule (`config.DemoDevMachineAllowed`) is enforced in four places:

- **Routes.** `DevMachineDemoGuard` answers 403 on every Dev Machine, coding task, automation, agent message and agent sign-in route.
- **Services.** `service.MachineAccess` applies the same rule wherever work reaches a machine without those routes. The task service refuses creating, retrying, reviewing, checking, previewing, messaging, continuing after sign-in and opening a terminal for the caller, so assistant approvals, automations, review follow-ups and GitHub mentions are covered. The Dev Machine service refuses creating or starting a machine, checkouts, agent runs, service and terminal launches, app ports and environment snapshots for the user it acts for. The task worker acts as the task's creator: a task of an excluded user fails with the reason instead of running. A custom tool's test run answers the same 403 as the routes. The services do not refuse stopping a machine or a preview, or cancelling a task: those only release resources.
- **Assistant.** Tools and approvals marked `machine` in the action policy table (`delegate_issue`, `request_agent_review`, `follow_up_task`, `message_task`, `start_preview`, `start_process`, `expose_port`, `run_check`, `run_custom_tool`) are refused when requested, and again when approved: the approval ends as failed with the reason and nothing is queued. The preview and port tools that need no approval are refused too.
- **Gateway.** Ticket exchanges and existing machine sessions recheck `SYSADMINS` on every request.

A new path that starts work on a machine must call `MachineAccess` (through `DevMachineService.RequireMachineUse` or the task service) rather than rely on the route guard.

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
| `opencode` | OpenCode | optional `OPENCODE_API_KEY` (a machine secret, or the user's saved OpenCode API key); otherwise model/provider dependent | interactive, autonomous |
| `codex` | Codex | optional `OPENAI_API_KEY` (or a subscription sign-in) | interactive, autonomous |
| `pi` | Pi | none; model/provider dependent (provider API keys as machine secrets) | interactive, autonomous |
| `qwen-code` | Qwen Code | none | interactive, autonomous |
| `crush` | Crush | none | interactive, autonomous |
| `kimi` | Kimi | none | interactive, autonomous |
| `goose` | Goose | none | interactive, autonomous |
| `custom` | Admin-configured argv CLI | explicit configuration | interactive, autonomous |

Provider adapters can construct both invocation modes, but the current product dialogs queue autonomous runs only and there is no UI/gateway endpoint for attaching to an interactive agent container. The IDE developer image pins all eight built-in CLI versions for terminal use. Pi, Qwen Code, Crush, Kimi, and Goose use `DEV_MACHINE_HEADLESS_IMAGE` for both developer-terminal and autonomous execution. The existing three built-ins retain their provider-specific agent images. Custom providers must be enabled by workspace policy and are currently launched through the autonomous dashboard flow. Provider-specific agent images remain separately pinned by tag or digest in configuration and runtime Dockerfiles; custom provider images must also be pinned.

Codex, Claude Code, OpenCode and Pi runs accept an optional `model` and `thinking`; Codex and Claude Code also an optional `model_server_id`. All three are rejected for the other providers (`harnessAcceptsModel` and `harnessRunsOnModelServer` in `BE/internal/service/dev_machine.go` are the two rules). A Codex or Claude Code run uses the chosen model server, or the workspace default when it speaks the harness's protocol (OpenAI-compatible for Codex, Anthropic-compatible for Claude Code); without one the harness uses its own sign-in and takes any valid model name. A model on a server must be offered by it when its model list is known; thinking levels are `off`, `low`, `medium`, `high`, `xhigh`, and `max` (Claude Code 2.1.330 accepts `low`, `medium`, `high`, `xhigh` and `max` for `--effort` and `CLAUDE_CODE_EFFORT_LEVEL`; Codex maps `off` to `none`). A run on the harness's own sign-in fills the model and thinking level it did not choose from the [workspace default](#agent-model-defaults). The server is recorded on the run (`model_server_id`). Codex receives the server and model entirely as argv `-c` overrides (`model_provider="kuayle"`, its `base_url`, `env_key="KUAYLE_MODEL_API_KEY"`, `wire_api="responses"`), so a user-managed `config.toml` cannot redirect a run; model names and URLs are restricted to characters that need no TOML quoting. Claude Code receives the model as argv and the thinking level through the validated non-secret `CLAUDE_CODE_EFFORT_LEVEL` container environment; `off` omits that variable.

OpenCode brings its own providers, so it never runs on a model server: a `model_server_id` is refused, and the workspace default server, its default model and thinking level, and the other agents' [workspace defaults](#agent-model-defaults) are never applied to it. Its model is written `provider/model` (for example `opencode-go/deepseek-v4.1-flash`; both parts are required) and is passed as `--model`; the thinking level is passed as the model's variant, `--model <provider/model>#<level>` (OpenCode 2 has no `--variant` flag), and `off` omits it. Without a model OpenCode runs the one it is configured with, and the thinking level is not passed: OpenCode selects a variant only together with a model. The autonomous command is `opencode run --format json --auto --print-logs --log-level warn --standalone [--model <provider/model>[#<level>]] -- <prompt>` with `OPENCODE_LOG_LEVEL=WARN`: the prompt follows `--` as one argument, because OpenCode's parser otherwise reads a prompt that begins with a dash as its own flags, and `--standalone` gives the run a private OpenCode server instead of looking for a background service. The terminal UI (`opencode`) takes no model flag; it gets the model as `OPENCODE_CONFIG_CONTENT={"model":"<provider/model>"}`. Limits: the variants a model has are specific to it (for one model `low`, `high` and `max`). A run accepts any of Kuayle's levels, and OpenCode 2 fails a run whose model lacks the variant (`provider.no-route: Variant unavailable for <model>: <level>`); for the models of OpenCode's Go plan the [model catalog](#agent-model-catalog) knows the allowed levels, offers only those and checks them when a profile is saved. `--auto` approves every permission the configuration does not explicitly deny: without it OpenCode rejects a request nobody can answer (reading the live inbox at `/run/kuayle/inbox` or any path outside the worktree); OpenCode 1.19.5 then ended the session, and the run failed without a final message. `--print-logs` sends OpenCode's warnings and errors to standard error, which the run's failure cause quotes: `--log-level warn` sets the client's level and `OPENCODE_LOG_LEVEL=WARN` its private server's, which otherwise prints every INFO line (OpenCode 2.3.2 rejects the upper-case `WARN` as a flag value). OpenCode stores its log at `$XDG_DATA_HOME/opencode/log`. A run that still ends on a refused permission (a repository `opencode.json` that denies one) fails with class `platform`, rule `permission_refused`, naming the permission.

Pi (`@earendil-works/pi-coding-agent`, pinned at or above 0.79.0, where CVE-2026-54325 to -54328 are fixed; the old `@mariozechner` package has no fix and is never installed) also brings its own providers and never runs on a model server. Its model is written `provider/model` like OpenCode's and passed as `--model`; the thinking level is passed as `--thinking <level>` (Pi's levels have Kuayle's names, `off` included). The autonomous command is `pi --print --mode json --no-session --no-approve [--model <provider/model>] [--thinking <level>] -- <prompt>`: `--no-session` keeps no session file (Kuayle does not resume Pi), and `--no-approve` declines the repository's own `.pi/` settings, extensions and packages, which an unattended run cannot be asked to trust (Pi still loads `AGENTS.md`). Pi exits 0 also when the model provider kept failing; the adapter reads its `message_end` events, takes the last assistant message that ended the turn with text and no tool call as the conclusion, and otherwise reports the message's `errorMessage` as the harness error (`parsePiResult` in `BE/internal/agent/providers.go`). Pi reads provider keys from the environment (for example `OPENCODE_API_KEY` or `ANTHROPIC_API_KEY`), so they are configured as machine secrets; it has no Kuayle sign-in, profile files or live inbox, and its worktree stays writable (checked after the run).

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

Completed autonomous provider stdout/stderr is collected and redacted before persistence. JSON-line provider events are normalized into `dev_machine_events`. A JSON event line over 16 KiB is stored with its long values cut instead of being dropped, the summary is the agent's final message and never its events, and a Claude Code, Codex or OpenCode run that exits 0 without a final message is recorded as failed; see [agent output](BE/docs/agent-tasks.md) ("Summary and final message").

## Execution Modes

### Human and Agent

The user opens authenticated IDE and browser links from Kuayle and attaches native terminal tabs rendered by the main UI. Terminal tabs speak `ttyd.v1` over the dedicated terminal host and attach to tmux sessions inside the developer container. All eight built-in CLIs are available directly from that terminal. Headless machines omit code-server and browser access and expose only the terminal plus agent workflow. Agents run only as [agent tasks](BE/docs/agent-tasks.md) started from an issue chat (or an automation); each attempt is a `dev_machine_agent_runs` record that runs in a separate container autonomously to completion under a timeout. The manual "Run agent" dialog, its run list and trace sheet, and the `agent-runs` HTTP routes were retired; existing run records stay as task attempts.

### Agent Only

An autonomous run receives:

- optional issue/project and checkout linkage;
- repository, base branch, working branch, and workspace path from the selected checkout or machine repository affinity;
- prompt and acceptance criteria;
- allowed commands and forbidden paths;
- exact test-command argv;
- maximum runtime (its own time limit; the machine has none);
- explicitly allowed secret names;
- whether to push the working branch.

A task attempt works in its selected checkout, runs the requested tests and
leaves changes uncommitted. Kuayle's publication operation (`publish_agent`)
commits them, pushes the task branch and opens or updates the pull request.

## Lifecycle

Machine status and desired status are persisted separately.

```text
queued -> spawning -> running
                       -> paused -> running
                       -> stopping -> stopped -> running
                       -> tearing_down -> destroyed
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
| `expired` | Legacy: no longer assigned. Migration 101 turns a machine the old expiry reaper marked but had not started tearing down into a stop; the manager stops it (`expired` → `stopped`) |

Lifecycle commands write `dev_machine_operations` with generation and idempotency key. Each lease pass locks the machine row and selects at most one operation per machine. Teardown and cancellation may preempt a long-running agent operation. The manager cancels local run contexts before destructive cleanup. Every non-destructive state write is conditional on the operation generation, and a stale spawn compensates by tearing down anything it created, so it cannot overwrite or survive a newer desired state. Identical idempotency-key retries return the original operation; reusing a key for another action is rejected. The manager can lease another task's operation on the same machine under the [task sharing rules](#task-machine-reuse).

Every reconciliation pass inspects Docker networks, volumes, gateway attachment, and non-agent service containers. A desired-running machine with missing, stopped, or detached resources is repaired from deterministic names and labels before the database is returned to `running`. Workspace deletion is rejected until all machine runtimes are destroyed, preserving the records needed for cleanup.

Resource names and labels are deterministic. Missing resources during teardown count as success. A partial spawn removes containers, disconnects the gateway, and removes the private network and workspace volume before retrying.

The production repository currently supplies services to the runtime in egress, collector, developer, then browser order; the Docker runtime starts them in caller-provided order rather than computing dependencies itself. Developer and browser containers publish file-based Docker health checks.

Machines have no lifetime. The one exception is an assistant temporary machine (`dev_machines.temporary_expires_at`, migration 159), which reconciliation permanently deletes 30 minutes after it was created; see [commands and temporary machines](BE/docs/conversations.md#commands-and-temporary-machines). A booted machine stays up until its user pauses, stops, tears down or deletes it: nothing pauses idle machines, stops them after a maximum runtime, tears them down when they expire, or stops an idle machine to make room for another (a spawn or start that waits for host CPU or memory keeps waiting, see host admission below). Agent tasks do not pause or stop their machine when they finish or are cancelled, so the next task can reuse it (see [task machine reuse](#task-machine-reuse)). Only the host disk floor still stops running machines, to protect the host. Gateway requests, native terminal WebSockets, API activity touches, checkouts, and agent runs update `last_activity_at`, which orders machines for reuse. Each agent run, check and custom tool run keeps its own time limit (`max_runtime_seconds`, 3600 seconds by default, 10800 seconds (three hours) for task attempts, up to 86400). Migration `000101_dev_machines_persistent` drops `dev_machines.expires_at`, `max_runtime_minutes` and `keep_running` and the workspace policy's `max_runtime_minutes` and `idle_pause_minutes`; existing machines keep running whatever their old expiry, and the machine's own credentials (collector token, browser CDP token, Docker TLS material) no longer expire with it. Docker TLS material is issued for ten years and discarded at teardown; Docker-enabled machines created before migration 101 keep certificates that end a day after their old expiry, so recreate them to use Docker beyond that. Rollback restores the columns with a 480-minute runtime and a lifetime of one day counted from the rollback, after which the previous release tears machines down again.

Starting a stopped or paused machine is idempotent. When its workspace volume was never recorded (its first spawn failed), the volume no longer exists, or a service container is missing, the start provisions the machine again from its deterministic names and reuses whatever still exists. The developer start scripts accept a workspace volume that already holds Kuayle state (`.kuayle/`, `repos/`, `tasks/`) or the user's work: a repository machine's root checkout is cloned into `.kuayle/root-checkout`, its files are moved into `/workspace` without replacing existing paths, and `.git` is moved last, so an existing `/workspace/.git` means the root is prepared and an interrupted preparation completes on the next start. The working branch is selected only when the root checkout is created; a restart keeps the current branch and local commits.

Pause, stop, teardown, and permanent delete are intentionally distinct:

- **Pause** pauses Docker processes and keeps memory allocated.
- **Stop** stops containers while retaining deterministic runtime resources for a later start.
- **Teardown** revokes sessions/tickets and removes containers, the private network, and workspace volume; database history remains.
- **Permanent delete** is admin-only. `DELETE /dev-machines/:machineId` and selected bulk DELETE record `delete_requested_at`, which hides the machine from the list at once. A machine that still has runtime resources is torn down first and purged when the teardown completes; an already destroyed machine (or a failed/expired one without a network or volume) has nothing to tear down and is purged in the same request, with the manager's reconcile pass retrying a purge that fails. The `/permanent-delete` endpoint remains an equivalent explicit route.
- **Bulk permanent delete** (`POST /dev-machines/bulk/permanent-delete`) purges settled machines: destroyed ones, plus failed/expired ones without runtime resources when `include_failed`/`include_expired` are set, optionally limited to `machine_ids`. `older_than_days` is the retention guard: omitted it defaults to 7, and `0` removes machines of any age; the Settings → Dev Machines "Clear" action sends `0` because the user asked explicitly. Machines with a queued or running operation, with runtime resources, or newer than the guard are kept and reported as `skipped_active`, `skipped_runtime` and `skipped_recent` beside the removed `count`.
- A deleted machine on which agent tasks have attempts keeps its record as task history (attempts, logs, diffs and the pull request refs a later merge completes through cascade from it). The record keeps `delete_requested_at`, so it stays out of the machine list and counts as removed; the manager purges it once no task run references it.

## Agent task integration

The task worker runs attempts through the existing machine operations. The
[agent task contract](BE/docs/agent-tasks.md) covers access and leases,
per-attempt credentials, conversation events, publication and recovery.

A task's check can also run on demand ([manual checks](BE/docs/agent-tasks.md#manual-checks),
migration 97): `POST /agent-tasks/:taskId/checks`, the task card's "Run check"
button or the assistant's approved `run_check` tool queue a `run_check`
operation that runs in the credential-free check container on the latest
attempt's checkout, records a `manual_check` step and never starts an attempt
or changes the task's state. It is refused while the agent works on the machine.

Every task attempt runs inside a tracked [agent session](BE/docs/agent-tasks.md#agent-sessions)
(migration 84, `agent_sessions`): an implementing task keeps one session across
its follow-ups, and its state follows the task's transitions in the same
transaction. Reviews requested from chat run as separate review tasks that
never validate, publish or receive a push credential; see [requested agent reviews](BE/docs/agent-tasks.md#requested-agent-reviews).
A reviewer is told that CI is judged separately and not to request changes over
CI it cannot see or a run on the commit that has not finished; its prompt lists
each check's state on the reviewed commit when Kuayle knows it, and otherwise
says CI is unknown.

Kuayle's own frontend checks publish a merged browser report on the Tests run.
The [CI routing guide](scripts/ci/README.md#frontend-browser-coverage) describes
which specs run and how shard failures reach the stable check.

A failed CI suite on a task's pull request carries what GitHub reported about
its failed checks ([CI and review feedback](BE/docs/agent-tasks.md#ci-and-review-feedback),
migration 170): the checks' names, failed steps, annotations and an excerpt of
the failed step's job log (failure lines and its end, without timestamps,
colours, other steps or the post-job clean-up), stored as the signal's bounded
summary. The webhook keeps what it reads within its eight seconds; when that
missed a failed check's log, a background read (60 seconds, outside the webhook
and the task worker) completes it, and the follow-up and the workflow's CI step
wait for it for at most three minutes. The follow-up prompt, the notebook
finding and the CI step's detail (the next agent's `{{failing_checks}}`) carry
the summary fenced as untrusted CI output, with the suite's link. A failed check
run that no webhook reported, which a workflow's CI step finds in GitHub's check
list, gets the same read (KUA-215): the step waits for it on its own record
(`agent_task_workflow_steps.ci`, migration 175).

A workflow's CI step judges the published commit once
([workflow CI on an unchanged commit](BE/docs/agent-tasks.md#workflow-ci-on-an-unchanged-commit),
KUA-215). It reads only the suites reported for that commit and records what it
judged on its step. When the work comes back without a new commit, the failures
already judged are no new result: Kuayle runs the failed GitHub Actions jobs
again once per commit (GitHub App **Actions: Read and write**, a token with
only that permission) and judges that run; CI that still fails pauses the
workflow for a person instead of sending the same failure back to an agent.

Agent profiles are also members ([agents as issue assignees](BE/docs/agent-tasks.md#agents-as-issue-assignees),
migration 142): an issue can be assigned to an agent beside people, and an agent
can be @mentioned in a comment, in the teams an administrator enables
(`agent_team_policies`). Assigning starts a task through the delegation path as
the assigning member, so the agent never has more permissions than that member;
unassigning cancels a task that is still working. `issue_agent_assignees` sits
beside the people-only `issue_assignees`.

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
in the same channel as untrusted text. A reply is mandatory (KUA-132, migration 151):
every delivered message must be answered by an outbox file naming it
(`"reply_to":<seq>`); Claude Code's `Stop` hook does not let the agent stop
without it, and an attempt of Claude Code or Codex that ends well with a
message unanswered is continued once on its session only to reply. A missing
reply never fails the attempt: the message is shown as unanswered and given
once more to the same step's next attempt. The task's creator can also
interrupt the working agent with a message (its session resumes with it; with
or without a workflow) or start an attempt for one, see the
[live channel](BE/docs/agent-tasks.md#live-channel). The agent view shows the
conversation in the task's timeline and has the composer, which reads and
sends through `GET/POST /agent-tasks/:taskId/live-messages` (the assistant
composer, aimed at a task's agent from the chat, sends through it too; the
task card in the sidebar has no thread and shows the path the task took); the assistant has
`message_task` (the task's creator approves the exact text on a card before
anything is sent) and the read-only `get_task_messages`; agent messages appear in the owning chat as escaped,
untrusted items through `task.live_message` conversation events and history
`agent_messages`, and questions also raise a toast and a Telegram notice
([agent messages in chat](BE/docs/conversations.md#agent-messages-in-chat)).
Codex and OpenCode are told to poll the inbox by the `kuayle-live-channel`
section of the `kuayle-agents` skill.

[Workspace memory](BE/docs/agent-tasks.md#workspace-memory) (migration 149,
`workspace_lessons`) keeps short lessons per repository. Agents propose them
through the outbox (`{"to":"memory","lesson":"..."}`), reviews that requested
changes propose their findings, and members write them by hand; a person's
messages to a task and CI results propose nothing, and nothing calls a model.
Text is redacted before it is stored. Only a workspace owner or admin
approves, edits, retires, drops or reorders lessons (`/memory/lessons`,
session only, audited in `workspace_lesson_events`); until then a task's
proposal is read only by them and the task's creator. Only approved lessons
are returned by `WorkspaceMemoryService.ApprovedLessons`, bounded and
redacted again, for a task's prompt. A private team's lesson stays with that
team. A lesson may be for one role of a team workflow (migration 209,
`role`): a pipeline member's outbox lesson is proposed for the role it plays,
and once approved it reaches only the tasks that play that role, alongside the
lessons for every task; any other task reads only the lessons without a role.

[Configurable workflows](BE/docs/agent-tasks.md#configurable-workflows)
(migration 118) replace the fixed implement → validate → publish → review flow
with a workspace's own state machines: agent profiles, immutable workflow
versions validated on save, defaults per workspace, team and project, and a
runner that drives each task's pinned version node by node under the worker's
lease. Tasks without a pinned version run the built-in Standard workflow,
which is the existing fixed flow.
A paused task with a pinned workflow can [continue on a chosen step of
another workflow](BE/docs/agent-tasks.md#continuing-on-a-chosen-workflow-step)
with its existing checkout, branch and pull request (migration 199).
A workflow's ship approval (an approval leading to `done` or a merge step, or,
reached after CI passed, through agent steps to `done`) marks a draft pull
request ready and brings a branch behind its base up to date when it is
entered, and approving it merges the pull request through the merge gate below
(on the approval, at the merge step after it, or at the `done` end the agent
steps reach, which may merge their added commits once their checks pass); the
task is done only after the merge. Accepting a task that waits at its ship
approval approves it (KUA-216). Agent reviews also start on an up-to-date
branch, and a conflicting branch goes back to the step that changes code
([merging from a workflow](BE/docs/agent-tasks.md#merging-from-a-workflow),
KUA-209). A CI step waiting for checks does the same and then judges CI on
the updated head (KUA-260). A waiting review, ship approval or merge step does
it again on every wake, and every update of a published commit shares one
bound (KUA-264). The step stores what it did and the facts it read, and its
detail says why (KUA-265). A workflow's iteration budget counts rounds of work: an agent step
that continues the round of the agent step before it (a plan handed to the
implementer) starts no new iteration (KUA-215).
Each profile has a [behaviour](BE/docs/agent-tasks.md#behaviours) (migration
143): it plans, changes code or reviews, and its role is a free-text label. A
planning step must leave the worktree unchanged and end with a plan, which is
stored whole and passed to the following agent steps as trusted instructions
through `{{plan}}`.
Every task also has a [notebook](BE/docs/agent-tasks.md#task-notebook)
(migration 150): one bounded record of the plan, decisions, what is done,
what remains, how to verify, findings and people's instructions, kept outside
the worktree. Each agent step receives it as instructions and is asked to
update it; one that does not is asked once more in its session, then Kuayle
records its final message itself, and the attempt never fails over it. A
planning step's worktree is mounted read-only, and the prompt every attempt
received is recorded by source.

[Automations](BE/docs/agent-tasks.md#automations) (migrations 69 and 193) delegate matching issues to their default, selected latest or pinned workflow. Rules match continuously, during a legacy UTC hour or on a timezone-aware cron schedule. The dedicated automation worker supports leased starts, bounded retries and replay. Runs need approval unless a rule is unattended and the workspace policy allows unattended automations; every start acts for a recorded user under that user's current permissions.

### Team workflows

Settings → Workflows lets workspace owners and admins build a pipeline from
phases, checks and role profiles, start from a template, or draft a separate
pipeline from a supported graph. Tasks pin the workflow version at start.
A graph runs on the graph runner. A pipeline runs on the task worker's
[tick](BE/internal/service/team_tick.go), which reads facts, posts changes in
the task room and schedules turns and platform actions.

Writer roles take turns on the root task's checkout under one write lock.
Reviewers and the optional lead run on separate member tasks. The room holds
instructions, verdicts tied to a commit, phase changes and decisions. The
lead reads the repository board, moves phases, maintains the notebook and
reports to the creator. Authority to approve the current head or waive a
check comes from the pinned pipeline or a grant from the creator. A lead's
split request creates sub-issues once the creator approves it, or at once
when the creator granted the lead the split (KUA-313). With no
lead, Kuayle derives the phase from required checks. Kuayle enforces the
merge gate in both cases.

Kuayle publishes the writer's work. The task stays in `publishing` while
the machine operation is queued or running. Only its finished result goes
back to the tick. The task page shows room posts, running roles, the pinned
phase chart, check results and files at a commit. See the
[team workflow contract](BE/docs/agent-tasks.md#team-workflows) for the
editor, room permissions, turn delivery, capacity and merge rules.

### Worktree scans

The assistant reads a task creator's live checkout on its running machine
through `scan_worktree`. Grep and glob respect ignore files. Read checks the
resolved path against the checkout. Status compares the live files to the task
branch and ignores submodules. A list scan reports tracked modifications
across worktrees and returns partial results if its deadline expires. These
read-only operations can run alongside other work. See the
[worktree scan contract](BE/docs/agent-tasks.md#worktree-scans) for limits and
result storage.

The Files panel reads a commit. Published commits come from a temporary
GitHub checkout. Before publication, or for a commit recorded by an attempt,
the service can read on the machine with the `tree` and `show` scan kinds.
Both read tracked regular files from Git objects, so uncommitted work stays
out of the panel. See [files at a commit](BE/docs/agent-tasks.md#files-at-a-commit).

## Resource Limits and Quotas

**Host admission.** Before spawning or starting a machine, the manager reads the
Docker host's CPU count and memory (`/info`) and admits the machine only if the
sum of live machines' requests (configuring, spawning, running, paused or
stopping; paused machines keep memory) plus this one fits within the host minus
`DEV_MACHINE_HOST_RESERVED_MEMORY_MB` / `DEV_MACHINE_HOST_RESERVED_CPU_MILLIS`.
Memory is never overcommitted; CPU may be, up to `DEV_MACHINE_CPU_OVERCOMMIT`.
A machine that does not fit stays queued: its operation is deferred for 30
seconds with a visible `host_capacity_wait` reason and without consuming a
retry, and agent tasks keep waiting rather than failing, with a reason naming
the wait; cancelling such a task withdraws its machine's start (see
[cancellation](BE/docs/agent-tasks.md#failure-cancellation-and-explicit-retries)). Admission is serialized
within one manager process, which also counts machines admitted but not yet
running; multiple manager replicas on one host are not coordinated. An unknown
host size never blocks. No machine is stopped to make room; the workspace/user
machine quotas below still apply.

**Host disk floor.** In monitored mode the manager stops running machines when
free space drops below either configured floor, in GB or percent, and defers
spawn and start operations that tasks are waiting for. Those tasks show
**Waiting for disk space** with the measured free space and both floor values.
Workspace owners and admins receive an inbox notice for affected tasks, grouped
by breach. The manager retries starts automatically once both floors are met.
The Compose configurations mount the Docker data filesystem at the manager's
`DEV_MACHINE_HOST_DISK_PATH` so it can measure recovery while machines are stopped.

Machine sizes are aggregate admission profiles:

| Size | CPU | RAM | Workspace disk hard quota |
|---|---:|---:|---:|
| small | 2 vCPU | 4 GB | 20 GB |
| medium | 4 vCPU | 8 GB | 50 GB |
| large | 8 vCPU | 16 GB | 100 GB |

A `custom` size picks CPU (1, 2, 4, 6, 8, 12 or 16 vCPU) and memory (2, 4, 8, 12, 16, 24, 32, 48 or 64 GB) from the offered options with at least 1 GB per vCPU, and any disk size in 2 GB steps from 20 GB up to the workspace `max_disk_gb` (itself an even number from 20 to 2048). The policy API returns these options as `size_options`.

**Task machines.** Machines Kuayle creates itself for agent tasks (issue delegations, the assistant's delegate tool and automations all go through agent tasks) are headless custom machines sized by the workspace policy's `task_machine_cpu_millis`, `task_machine_memory_mb` and `task_machine_disk_gb` (Settings → Dev Machines → Policy → Task machines). The defaults are 4 vCPU, 8 GB and 20 GB; migration 90 gives existing workspaces the same values (task machines were previously `small`, whose 4 GB left an agent such as Claude Code 1.4 GB and was OOM-killed under builds). The same size rules apply, and the task disk must stay within `max_disk_gb`. Changes affect task machines created afterwards; reused task machines keep their size.

The manager assigns a bounded percentage of the profile to each service and applies Docker `NanoCPUs`, memory, memory-swap, and PID limits (`containerSharePercent` in `BE/internal/machine/docker.go`). On standard machines the IDE (which also serves the terminal) and browser each receive 25%, an agent run 35%, and the collector and egress 5% each. Headless machines (task machines among them) have no IDE or browser, so the agent receives 50% and the terminal container 40%; with a Docker daemon, which takes 40%, the agent and the terminal receive 35% each. Shares are per-container ceilings, not reservations: without Docker the headless shares add up to the machine, and with Docker the agent and terminal ceilings overlap (120%) so the terminal people use by hand is not squeezed below the agent. An 8 GB task machine therefore gives its agent 4 GB (2.8 GB with Docker). PID limits count threads. Agent containers, including manual checks and custom tools, get at least 4,096 PIDs up to the machine's configured PID limit, or their share if higher. Migration 194 raises the machine default from 512 to 4,096 and updates machines with the previous default. The IDE and terminal retain their 1,024-PID floor. Other services keep their share, with at least 256 for the browser and 32 otherwise. Manual checks keep the 35% agent share because their compose services share another 30%. When agent tasks share a machine, the memory limit of each agent attempt and manual check container is the agent share divided by the tasks with a running attempt or check there, the new one included (`agentMemoryBytes`, counted from the machine's running agent containers when a container is created and again on resize), with at least 1 GB (`agentMinimumMemoryBytes`) and never more than the undivided share. The limits then add up to about the agent share instead of a multiple of the machine, so a busy machine reclaims cache and slows down rather than the kernel stopping agents with exit code 137; a container keeps the limit it started with until the machine is resized. CPU and PID limits apply per container without division among tasks. These CPU and memory shares apply to machines that do not share their resources ([shared machine resources](#shared-machine-resources)); PID shares always apply. Non-builder workloads use a read-only root filesystem, tmpfs scratch paths, `CapDrop=ALL`, `no-new-privileges`, the Docker default seccomp profile or a configured replacement, and AppArmor where available. Environment Builder deliberately relaxes the developer container root filesystem so owners/admins can install tooling before snapshot, and a workspace that allows sudo relaxes its developer containers (see [Sudo in machines](#sudo-in-machines)). Docker's `json-file` logs rotate at 10 MiB with three files per container, and removal paths also remove anonymous image volumes.

**Live resize.** `PATCH /dev-machines/:machineId` with any of `cpu_millis`,
`memory_mb` and `disk_gb` (omitted values keep the current size; a request
without a size returns the machine unchanged) resizes the creator's machine while it is running, paused
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

**Release images.** Each machine service and agent provider records the image
it was created with (`dev_machine_services.image_ref`,
`dev_machine_agent_providers.image_ref`); the manager moves them to the
installed release's images, its `DEV_MACHINE_*_IMAGE` settings. A new run of a
built-in agent uses the release's agent image and records it on the provider; a
run that already has a container keeps its image so it can resume, and a custom
agent keeps its configured image. A machine's own containers (collector, egress,
Docker daemon, browser, and the IDE or headless terminal) are recreated from the
release images: when a stopped machine starts, and, for a running machine, by an
`update_images` operation (migration `000108_machine_image_updates`) that
reconciliation queues for an outdated machine or one with finished run
containers, only while the machine is idle: no other queued or running
operation, no unfinished agent run, no running task preview and no activity
through the gateway or API (`last_activity_at`, which an open terminal or IDE
connection renews every minute) in the last 15 minutes. The
operation re-checks this when it runs, removes the machine's finished agent,
check and never-started live inbox writer containers and the inbox volumes no
container uses (they would keep old images in use; a failed removal fails the
operation, which is retried), removes any outdated containers, records the new images and provisions the machine again like a
recovery, then records `machine.images_updated` with each service's old and new
image. Containers keep no state of their own (read-only root, in-memory `/tmp`
and home), and the workspace and Docker volumes stay, so this is like a restart:
running processes and detached terminal sessions end. A paused machine keeps its frozen
processes and is not recreated. The developer container of a machine built from
a Development Environment keeps the environment's image. A busy or failed
update is queued again after an hour. A manager without release images
configured leaves machine images alone.

**Resource limit visibility.** An exited agent or check container is inspected
for Docker's `State.OOMKilled` (set for any out-of-memory kill in its cgroup, a
killed compiler included); the manager also follows Docker `oom` events for
Kuayle containers (terminal, editor, Docker daemon, check services) as they
happen, reporting each container once per minute. A kill records
`resource_limit_kind=memory` with a message such as "Out of memory: the agent
container reached its 1.4 GiB memory limit (or, on a machine that shares its
memory, "the machine reached its 16 GiB memory limit and a process in the agent
container was stopped"). Resize the machine or raise the task
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
- maximum per-machine workspace-volume size (2 GB steps up to 2048 GB; migration 90 rounded an odd limit down);
- task machine CPU, memory and disk (default 4 vCPU, 8 GB, 20 GB; older clients that omit them keep the current values);
- how many active agent tasks share one task machine (`task_machine_max_tasks`, default 10, 1 to 10; migrations 113 and 117, which kept saved values; rolling 117 back caps limits above 4 at 4; older clients that omit it keep the current value);
- an optional per-machine override of that limit (`dev_machines.max_tasks`, 1 to 10, migration 156; NULL follows the policy and it can be changed while the machine runs);
- how many entries one directory of a task's file listing holds (`task_files_max`, default 30,000, 100 to 30,000; migration 210; older clients that omit it keep the current value), for both [sources of a task's files](BE/docs/agent-tasks.md#files-at-a-commit);
- allowed providers;
- allowed repositories;
- whether custom providers are allowed;
- whether new machines get their own Docker daemon (`allow_docker`, default off);
- whether the developer containers of new machines allow sudo (`allow_sudo`, default off).

### Task machine reuse

A new task uses a running machine owned by its creator in the same workspace
and environment. The machine must have a standard or headless profile and
satisfy the workspace's sudo and Docker policies. Builders, temporary machines
and machines being deleted are excluded. A manual agent session or pending or
leased stop, pause, teardown, snapshot, check, tool or command also excludes
the machine.

[ReserveMachine](BE/internal/repository/agent_task.go) picks an idle machine
first, then the one with the fewest active tasks. Ties favour a machine used
for the same issue, recent activity, the cached repository and an enabled
provider. If every suitable machine is full, the task stays in `preparing`
and shows the machine name and task count. If none is suitable, Kuayle creates
a task machine. A task can give up a newly created machine that has never
started when a suitable machine becomes free. Later attempts keep their
assigned machine while it exists and can restart it if paused or stopped.

Each task has its own checkout, branch and session directory. Task checkouts,
attempts, validations and publications can run alongside one another.
A user's manual agent session needs exclusive access, and manual checks and
custom tool runs are refused while the machine has active agent work.
Read-only [scans](#worktree-scans) can run alongside either. See the manager's
[operation leasing rules](BE/internal/repository/dev_machine.go).

The workspace policy sets `task_machine_max_tasks` (1–10, default 10).
The creator with `dev_machine:manage` can set `max_tasks` with
`PATCH /dev-machines/:machineId` or send `clear_max_tasks: true` to follow
the policy again. This works in every machine status, including during a
resize request. The machine page shows the effective limit and its source.
Changes apply at the next placement or turn without a restart. Placement and [member turns](BE/internal/repository/task_member_slot.go)
count active tasks holding the machine under the same workspace lock.
Finished, blocked and awaiting-review tasks release their slots. A member
uses its own task's machine or the root task's machine for its first turn.
When turns from several tasks are waiting on one machine, the oldest unread
room post gets the next slot. Attempt creation checks capacity again under
the workspace and machine locks.

### Shared machine resources

Fixed shares strand resources: on a 16 GB headless machine with Docker the
daemon container was capped at 6.4 GiB and its Node build was killed while the
terminal's 5.6 GiB sat unused. When the host's Docker daemon uses the systemd
cgroup driver with cgroup v2, every container of a **new** machine runs in one
systemd slice, `kuayle-<routing key>.slice` (a child of `kuayle.slice`), whose
`MemoryMax` is the machine's memory, whose `MemorySwapMax` is
`DEV_MACHINE_SWAP_PERCENT` of it (default 50) and whose `CPUQuota` is the
machine's CPUs. Workload containers (editor, terminal, browser, agent runs,
checks, custom tools, the Docker daemon and check services) get the machine's
memory and CPUs as their own limits and unlimited swap, so any of them may use
the whole machine; CPU is throttled at the machine's quota, and when the machine
as a whole runs out of memory the kernel stops the largest process in the slice
and the alert reads "Out of memory: the machine reached its 16 GiB memory limit
and a process in the Docker daemon container was stopped." The collector and
egress proxy keep their 5% CPU and memory limits inside the slice. PID shares
are unchanged. Swap holds idle pages; a
runaway process still reaches `MemoryMax` and is killed, and host swap is shared
by all machines, so it is a ceiling rather than a reservation. Swapped pages are
written unencrypted to the host's swap device like any other host memory.

The machine network's `com.kuayle.resource-scope=machine` label records the choice
when the machine is created, after its slice limits are set, so a machine never
mixes both models; machines created earlier, on hosts without systemd/cgroup v2,
with `DEV_MACHINE_SLICES=off`, or whose slice could not be prepared keep
per-container shares until they are recreated. Containers on a shared machine
carry the same label, which resize and out-of-memory reporting read. Limits are
stored as persistent systemd properties (`/etc/systemd/system.control`), so they
apply before the slice exists and survive host reboots; Spawn re-applies them,
resize applies the new size to the slice before its containers, and teardown
stops the slice and reverts its properties (`BE/internal/machine/machine_slice.go`).

The manager itself runs as UID 1000 without capabilities, and systemd accepts
unit changes from root only. It therefore runs each change in a short-lived
helper container from its own image (found by inspecting its own container):
`machine-manager machine-slice probe|apply|remove`, as UID 0 with no
capabilities, `no-new-privileges`, no network, a read-only root filesystem and
only systemd's private socket (`/run/systemd/private`) bind-mounted. The helper
calls `SetUnitProperties`, `StopUnit` and `RevertUnitFiles` and accepts only
names matching `kuayle-[a-z0-9]+.slice` (`BE/internal/systemdslice`). The manager
probes the helper at startup and logs why new machines keep container shares
when it is unavailable (for example on Docker Desktop or with user-namespace
remapping).

**Usage history.** A sampler beside reconcile (`BE/internal/machine/resource_sampler.go`)
records, every 30 seconds (`domain.DevMachineResourceSampleInterval`), the summed
CPU, memory, process and network usage of all of a running machine's containers
(its services and the agent runs, checks, check services and custom tools beside
them; CPU is 100% per core, measured between a container's one-shot Docker
samples of successive calls, so a container's first sample reads zero) plus
its workspace and Docker disk usage in `dev_machine_resource_samples`. The sampler
is the only caller of `Runtime.Stats`; reconcile no longer samples, so a slow or
stuck reconcile pass never stops it. Each tick lists the `running` machines
(including ones with a queued operation, so a resize leaves no hole) and samples
each in its own goroutine: at most 8 at once, each bounded to 20 seconds, and a
machine whose previous sample is still running is skipped, so one hung Docker
call delays no other machine. A failed stats call or insert is logged with the
machine id, counted (`resource_sample_stats_failed_total`,
`resource_sample_inserts_failed_total`, `resource_samples_missed_total`,
`resource_samples_recorded_total` in the manager metrics) and retried once after
5 seconds, so a failing sample leaves a gap of about a minute at most. The volume
size update and the disk-quota warning and stop follow each measured sample.

The history is a rolling 24-hour window (`domain.DevMachineResourceRetention`):
every 5 minutes (and at start) the manager deletes the samples older than that
for every machine, stopped or torn down included, in batches of 5000 rows
(`PruneExpiredResourceSamples`, served by the `created_at` index of migration
000169). Teardown deletes a machine's history at once; deleting the machine
cascades. `GET .../resource-usage?window=1h|6h|24h` returns every bucket of the
window, oldest first (`generate_series` joined to the `date_bin` averages, so
network counters keep the bucket's highest value). A bucket is at least two
sampling intervals wide (1h and 6h: 1 minute; 24h: 4 minutes), so one late sample
does not look like a gap. A bucket with no samples is returned explicitly with
`sample_count` 0 and null values; the machine page's CPU, memory and disk charts
break the line there and span exactly the returned buckets. Without `window` the
endpoint returns the latest 120 samples, newest first.

### Disk Limitation

Workspace volumes use Docker's built-in `local` driver with its `size` option set to the machine profile's exact byte limit. Docker enforces that option through XFS project quotas. The Docker data root must therefore reside on XFS mounted with `pquota` or `prjquota`, and the daemon must not run in a user namespace. The manager creates and removes a quota-probe volume during startup; if Docker reports that quota support is unavailable, manager startup fails and its readiness endpoint remains unavailable. Existing volumes without the expected local driver, ownership labels, and exact hard limit are rejected before containers start or runtime reconciliation reuses them.

The manager still samples workspace usage, stores resource telemetry, emits `resource.disk_quota_warning` at 90 % of the limit, and queues a stop with `resource.disk_quota_exceeded` at the limit as defense in depth. That delayed monitor is not the primary storage boundary in the default `hard` mode. CPU, memory, process, and workspace disk limits are enforced by the runtime.

#### Monitored mode (hosts without XFS project quotas)

`DEV_MACHINE_DISK_QUOTA_MODE=monitored` is an explicit operator opt-in for hosts whose Docker data root cannot provide project quotas (for example ext4). The manager then skips the startup quota probe and creates workspace volumes without the `size` option, labelled `com.kuayle.workspace-quota-mode=monitored` and with the limit in `com.kuayle.workspace-quota-bytes`. The workspace volume is the only unbounded write surface: root filesystems are read-only, `/tmp` and home directories are size-limited tmpfs, and container logs are capped. Because those tmpfs mounts are small (256 MiB `/tmp`, 512 MiB home, where the images keep Go's caches), check, manual check, custom tool, preview and autonomous agent containers point `TMPDIR`/`TMP`/`TEMP`, `GOTMPDIR`, `GOCACHE`, `GOMODCACHE`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `npm_config_cache`, `PIP_CACHE_DIR` and `YARN_CACHE_FOLDER` at a per-run directory `/workspace/.kuayle/tmp/<container key>` on the workspace volume (`BE/internal/machine/run_tmp.go`). These settings override a custom image's own `XDG_DATA_HOME` and `XDG_STATE_HOME`. A fixed shell prelude recreates the directory when the container starts and unsets the overrides if it cannot. Checks, tools and previews remove the directory when their commands end. The manager removes an attempt's directory through the developer container once the attempt ends. An OpenCode task attempt links `opencode/` in its data and state directories to the task's session directory `/workspace/.kuayle/agent-sessions/<task>/opencode/`, so the next attempt continues its session with `--session`; a Goose task attempt links its data directory's `goose/sessions` there the same way (see [native session resume](BE/docs/agent-tasks.md#native-session-resume-and-handoff)). Interactive attempts keep the tmpfs defaults.

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

Model discovery calls `GET <openai>/models` with a bearer key, or `GET <anthropic>/v1/models` with `x-api-key`, through a client that dials only public addresses unless `AI_ALLOW_PRIVATE_NETWORK` is set and never follows redirects. A saved server's key is only sent to the URLs saved with it. At most 500 IDs matching `[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}` are kept. Discovery returns labels from `display_name` or `name` as an optional `model_names` map keyed by ID. The settings form saves the map alongside the IDs. Pickers use the ID as the label when a server supplies no display name or when its list predates label storage.

Servers are workspace data rather than machine snapshots. The IDE and terminal containers receive the default server; an agent run receives only the server recorded on it. The manager copies `KUAYLE_MODEL_API_KEY`, `KUAYLE_MODEL`, `KUAYLE_MODEL_THINKING`, `KUAYLE_MODEL_LIST`, `KUAYLE_MODEL_OPENAI_BASE_URL` and, for an Anthropic-compatible server, `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` and the Claude Code model aliases into `/run/kuayle-secrets`; browser, collector and egress containers never receive them. Rotating a key therefore applies the next time the developer or agent container starts.

The IDE and headless start scripts and the autonomous agent runner create `$HOME/.codex/config.toml` (mode 0600) for the default server only when the file is absent, map Codex `off` to `none`, and leave an existing user-managed configuration unchanged. They also generate `$HOME/.codex/model-catalogs/kuayle.json` from the installed Codex's bundled catalog (`codex-model-catalog.cjs`) for the server's models, so Codex's `/model` picker offers only those; if generation fails, the config omits the catalog and the configured model still works. Because the home directory is a small in-memory tmpfs, the same scripts link `~/.codex/packages` (Codex's managed app-server, about 400 MB per version) to `/workspace/.kuayle/codex-packages`, so the download lives on the machine's workspace volume and is shared by its terminal and agent containers.

### Agent sign-in

A Codex or Claude Code run without a model server uses the agent's own account. An API key secret on the machine (`OPENAI_API_KEY`/`CODEX_API_KEY`, or `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`/`CLAUDE_CODE_OAUTH_TOKEN`) takes precedence. The manager then uses the requesting user's saved Claude Code token or the agent's saved credentials file. Without a sign-in it stops before starting a container, with `sign_in_required=true` on the task (migration `000077_agent_sign_ins`). OpenCode never stops for sign-in and has its own saved API key.

**Connect Codex from Settings.** In Settings > Agent configuration > Your sign-ins, connect your ChatGPT account before starting a machine. Enable device code login in ChatGPT Security settings, or ask your ChatGPT workspace admin to enable it. Open the verification page and enter the code shown in Kuayle. Keep the dialog open until Kuayle confirms the connection. The session-only `POST /agent-sign-ins/codex/device/{start,poll,cancel}` routes scope each attempt to the caller and workspace. The API runs the pinned Codex App Server over stdio using an isolated temporary home. Only the public prompt and status reach the UI. The authenticated poll stores the encrypted file with `DEV_MACHINE_ENCRYPTION_KEY` in `agent_sign_ins`. Attempts expire after 15 minutes. A new attempt replaces the previous one for that user in that workspace. Active attempts live in the API process, so clustered deployments need session affinity for the start, poll and cancel requests. An API restart requires a new attempt. Native API installations need `codex` on the server's path. Dev Machine provider policy and credential precedence still apply when machines and tasks use the sign-in.

**Continue after sign-in.** A task creator can retry a stopped task with their saved Codex sign-in or Claude Code token. The task panel offers Claude Code token entry and terminal sign-in. Codex also offers terminal sign-in as a fallback. The terminal opens with `claude /login` or `codex login --device-auth`. When a user continues without saved credentials, `POST /agent-tasks/:taskId/sign-in` queues a `capture_sign_in` operation. The manager reads `~/.claude/.credentials.json` or `~/.codex/auth.json` from the machine's terminal or IDE container, accepts a JSON object of at most 64 KiB, encrypts it, and retries the task. Only the creator of a task stopped for sign-in can continue, and `request_id` makes repeated requests converge. Missing terminal credentials return `409 SIGN_IN_NOT_FOUND`. The manager saves a run's refreshed credentials only if the stored credentials still match what the run started with. This preserves replacements and removals made while a run was active.

### Token usage and cost

Every agent run records the tokens and cost its harness reported (Claude Code, Codex, OpenCode and Pi; NULL when not reported), its closing continuation included (migration `000204_agent_run_usage`, KUA-255). A task's usage is the sum over its runs, shown in the agent view's details block and per run. The harness's cost is an estimate, and no budget is enforced on it. See [BE/docs/agent-tasks.md](BE/docs/agent-tasks.md#token-usage-and-cost).

### Usage limits

An agent whose provider refuses work until a quota resets (Claude Code's 5-hour or weekly limit, Codex's usage limit) does not fail its task (migration `000137_agent_task_usage_limits`, KUA-113).

- **Detect:** when a run fails, the manager checks its output with `domain.DetectUsageLimit`. It reads Claude Code's rejected `rate_limit_event` (exact `resetsAt`) and the providers' messages ("You've hit your 5-hour limit · resets 4:10am (UTC)", "try again in 2 hours"). It stores the run error `usage_limit_reached: <message>` with `resets_at=`. Unknown wording stays an ordinary failure.
- **Pause:** the task worker then pauses the task. The attempt goes through the usual runtime cleanup, which keeps the checkout, worktree and session transcripts, and ends in `blocked` rather than `failed`. The task carries `usage_limit_message`, `usage_limit_resets_at` and `usage_limit_resume_at` (the reset plus two minutes; 30 minutes when no time was given).
- **Hold:** for a task using the creator's own sign-in (no model server), `agent_usage_limit_holds` records the limit per user and agent. `Claim` skips that user's queued tasks for the agent until it expires, so they wait instead of starting and failing.
- **Resume:** every 15 seconds the worker starts the next attempt of paused tasks that are due. That is a new generation and run, `resume_native_session=true`, so the attempt continues the previous native session (`claude --resume`, `codex exec resume`, `opencode run --session`, `qwen --resume`, `kimi --session` or `goose run --resume --name`, when the same harness ran it) with a handoff telling the agent it was paused, not failed. A task resumes on its own at most five times in a row; after that the user resumes it.
- **Controls:** `POST /agent-tasks/:taskId/resume` is "Resume now" (it also lifts the creator's hold) and `DELETE /agent-tasks/:taskId/resume` is "Don't resume". Both are session-only and limited to the task's creator; cancel and retry work as for any blocked task, and a manual retry forgets the pause.

The task card shows the provider's message, the reset time and the two controls; the transcript shows "Paused at the usage limit" instead of "Stopped with an error".

**Provider account without credit** (KUA-218, migration `000176_agent_task_provider_credit`). `domain.DetectProviderCredit` reads only the harness's own error and the error events of its JSON output for OpenCode's "Insufficient balance", Anthropic's "credit balance is too low", OpenAI's `insufficient_quota` / "You exceeded your current quota" and Codex's "Quota exceeded. Check your plan and billing details." The run error is `provider_credit_exhausted: <sentence>` (links dropped) with rule `provider_credit`. The task pauses through the same cleanup into `blocked` with `usage_limit_message` and `provider_credit_exhausted=true`, but with no reset, no automatic resume and no hold. A workflow task stays on its step and its time budget is held. "Resume now" continues the same step, visit and iteration. The task card says to top the provider account up, then resume.

**Model the provider does not offer.** `domain.DetectModelNotFound` reads OpenCode's `ProviderModelNotFoundError` (also from its `level=ERROR` log line) and OpenCode 2's "Model unavailable: …" (`provider.no-route`; its "Variant unavailable" names a model that exists and is not matched), Anthropic's `not_found_error` ("model: …"), Claude Code's "There's an issue with the selected model (…)" and OpenAI's "The model `…` does not exist" / `model_not_found`. The run error is `model_not_found: <model>` with rule `model_not_found`. The task fails with class `provider`, naming the model and where it is set, and does not take a workflow's `agent_failed` edge. See [agent-tasks.md](BE/docs/agent-tasks.md#provider-credit-and-unknown-models).

### Workflow pause and resume

A task's creator can pause its running workflow and resume it from the same step (migration `000145_workflow_pause`, KUA-129): `POST /agent-tasks/:taskId/pause` with `{"mode": "now" | "after_step"}` and `POST /agent-tasks/:taskId/resume`, session-only and limited to the creator like cancel.

- **State:** `agent_tasks.workflow_paused_at` marks a paused workflow and `workflow_pause_request` a pause that waits for the current step. `Claim` skips paused tasks and `applyWorkflowTx` refuses every move, so waiting steps do not advance and no step starts.
- **Pause now on a working agent:** the attempt is stopped through the same cleanup a usage limit uses and the task ends in `blocked` ("Paused") with its machine, checkout, worktree and session kept. Resuming starts a new attempt of the same step with `resume_native_session=true`; visits and iterations are unchanged. A failed cleanup is retried by the worker until the task is paused. A paused task is not machine load.
- **Otherwise:** a waiting or not yet started task pauses in place; a check, a publication or "after this step" pauses when the worker has the step's outcome (`ParkWorkflow`), before acting on it.
- **Time:** the paused time is added to `workflow_budget_from` and to the step's `paused_seconds`, so neither the time budget nor the step's timeout counts it. The time budget counts working time only: `workflow_started_at` is set at the first `queued`→`preparing` move (migration `000173_workflow_clock`, KUA-171), and `agent_tasks.workflow_held_since` stops the clock during approval, merge, `waiting_input` and `waiting_for_disk` waits, adding the held time to the budget start when the wait ends. A pause inside such a hold does not move the budget twice.
- **Audit:** `workflow_audit_events` records `pause_requested`, `paused`, `resumed` and `pause_withdrawn`; `GET /agent-tasks/:taskId/workflow` returns them as `pauses`.

Details are in [BE/docs/agent-tasks.md](BE/docs/agent-tasks.md#pause-and-resume).

Later runs of that user receive the sign-in as the scoped secret `KUAYLE_CLAUDE_CREDENTIALS` or `KUAYLE_CODEX_AUTH`; its token strings are added as further secrets so output redaction covers them. The runner writes the file into the run's in-memory home (mode 0600) and unsets the variable. Because agents may rotate refresh tokens, the runner then runs the harness as a child (forwarding stop signals and keeping its exit status) and afterwards copies the possibly refreshed file to `/workspace/.kuayle/sign-in-return/<run-id>.json` (mode 0600); the manager reads and deletes it right after the run and stores it when it changed. The sign-in is therefore briefly on the user's own machine volume at the end of each such run. Sign-ins are never returned by the API.

**Claude Code token.** `claude setup-token` prints a one-year OAuth token for a Claude Pro, Max, Team or Enterprise subscription; it can only make model requests. A user saves it with `PUT /agent-sign-ins/claude-code/token` (`{"token": "..."}`; one line of 20-1024 printable ASCII characters, surrounding whitespace trimmed; the `sk-ant-oat01-` prefix is not required). It is encrypted with `DEV_MACHINE_ENCRYPTION_KEY` into `agent_sign_ins` with `kind='token'` next to any captured `kind='credentials'` file (migration `000081_agent_sign_in_tokens`; a token is allowed only for `claude-code`; rolling back deletes saved tokens). `expires_at` is set to one year after saving: never later than the real expiry, so a token past it is skipped (the run falls back to the captured file, or asks to sign in again). A run receives it as the scoped secret `CLAUDE_CODE_OAUTH_TOKEN`, redacted from output; nothing is written to the run's home and nothing is handed back. When the task's creator has saved a token, `POST /agent-tasks/:taskId/sign-in` skips the terminal capture and starts the new attempt directly.

**OpenCode API key.** OpenCode has no terminal sign-in Kuayle can capture, and an `opencode auth login` made in a terminal does not reach a run, whose home is in memory. A user instead saves an OpenCode API key (for example the key of an OpenCode Go subscription, opencode's `opencode-go` provider) with the same `PUT /agent-sign-ins/opencode/token` (`{"token": "..."}`, the same one line of 20-1024 printable ASCII characters; no prefix is assumed). It is stored like the Claude Code token: encrypted with `DEV_MACHINE_ENCRYPTION_KEY` into `agent_sign_ins` with `kind='token'`, per workspace and user, and never returned (migration `000141_opencode_api_key` allows `opencode` there, as a token only; rolling back deletes saved OpenCode keys). An API key has no known lifetime, so `expires_at` stays null and the API reports no expiry estimate: the one-year estimate applies only to `claude setup-token`. Every OpenCode run requested by that user (tasks, workflow steps, reviews and retries) receives it as the scoped secret `OPENCODE_API_KEY`, the environment variable opencode's provider catalog declares for `opencode-go`, through `/run/kuayle-secrets` like other run secrets and redacted from output; runs of other agents never receive it. An `OPENCODE_API_KEY` machine secret the run is allowed to use takes precedence, as `ANTHROPIC_API_KEY` does for Claude Code. An OpenCode run without a saved key is not stopped for sign-in, because OpenCode can be configured in other ways; a missing or rejected key shows as the agent's own error. Kuayle does not verify the key when it is saved. With an explicit `DEV_MACHINE_EGRESS_ALLOWLIST`, add `opencode.ai`.

**Sign-ins in the owner's own terminal.** The machine owner's terminal and IDE shells use the owner's own sign-ins, so `claude` and `codex` start signed in. Only the machine's creator (`created_by_user_id`), in the machine's workspace, is read; a machine never receives another user's sign-in, and only agents the workspace policy allows (`allowed_providers`) are included. An agent the developer container already reaches through the workspace's default model server or an API key secret of the `ide`/`terminal` service is left out. The manager writes the set (a saved Claude Code token with its expiry in Unix seconds, `claude-code.token-expires`; the captured Claude Code credentials file; the Codex `auth.json`; a saved OpenCode API key, `opencode.api-key`; and a `managed` list) as a tar archive on stdin into `~/.kuayle/agent-sign-ins` (0700, files 0600) in the container's in-memory home, never on the workspace volume and never in argv or the container environment. It does so after every spawn, start and recovery, and on a `sync_agent_sign_ins` operation (migration `000103_agent_sign_in_sync`) that saving, replacing or deleting a sign-in (including a terminal capture and a run's refreshed sign-in) queues in the same transaction for each running machine the user owns in that workspace; a pending sync is not queued twice, and rollback refuses while one is queued. `kuayle-agent-sign-in.sh`, sourced by `kuayle-shell` for each interactive shell, exports the token as `CLAUDE_CODE_OAUTH_TOKEN` (the same environment variable a run gets) until its recorded expiry, then falls back to the captured file (an expired token is never synced at all). It exports a saved OpenCode API key as `OPENCODE_API_KEY` unless the shell already has one; the key is synced only when the policy allows `opencode` and the `ide`/`terminal` service has no `OPENCODE_API_KEY` secret, OpenCode is not listed in `managed`, and a missing key prints no hint. It installs a credentials file only where the user has no login of their own: a copy in `~/.kuayle/agent-sign-ins-installed` recognises Kuayle's file, so a later change replaces it, and a removal, or Kuayle no longer signing that agent in (the policy stopped allowing it, or a model server or API key now configures it), deletes it, while a login the user made in the terminal is never overwritten or removed and keeps precedence over the token. Changes therefore apply to the next shell; a shell that is already open keeps its environment. Without a sign-in for a managed agent, the first pane of each terminal tab prints how to sign in (`claude setup-token` saved under Settings > Agent configuration > Your sign-ins, or `codex login --device-auth`). Every start also pre-seeds `~/.claude.json` (`kuayle-claude-config.cjs`) with `hasCompletedOnboarding` when it is missing, so the first-run screens are skipped while values the user set, and their own login, are kept. Workspace trust is never pre-granted: a checkout's committed `.claude/settings.json` hooks run with the user's permissions and could read the synced sign-in, so Claude Code keeps asking the user to trust each checkout. A refreshed credentials file can diverge between a terminal and Kuayle's copy when both refresh the same OAuth login; the saved token has no refresh and avoids this.

`GET /agent-sign-ins` lists the caller's own status for Claude Code, Codex and OpenCode (`supports_token`; `token` with `saved_at`, `expires_at` (null for an OpenCode API key), `expired`; `terminal` with `saved_at`), and `DELETE /agent-sign-ins/:provider[?kind=token|credentials]` removes the caller's token, terminal sign-in, or both. These routes are session-only, refused for guests, and always act on the signed-in user: admins cannot read or remove anyone else's sign-ins. Removing a token deletes only Kuayle's encrypted copy; the token stays valid until it expires unless the user revokes it in their claude.ai settings. Codex has no equivalent token and keeps the terminal capture of `~/.codex/auth.json`. Removing an OpenCode API key likewise deletes only Kuayle's copy; the key is revoked in the user's OpenCode account.

Setting up the Claude Code token (also shown under Settings → Agent configuration → Your sign-ins):

1. On any computer where you are signed in to Claude Code with your subscription, run `claude setup-token` and complete the browser sign-in it opens.
2. Copy the token it prints (it starts with `sk-ant-oat01-`).
3. In Kuayle, open Settings → Agent configuration → Your sign-ins, paste it into **Claude Code token** and choose **Save token**. A Claude Code task stopped for sign-in also accepts it directly: paste it there and choose **Save token and continue**.
4. Renew it yearly: when the card shows **Token expired** (or a task asks you to sign in again), run `claude setup-token` again and paste the new token; it replaces the old one.
5. To stop Kuayle from using it, choose **Remove token**. To revoke the token itself, revoke it in your claude.ai settings.

Migration `000076_agent_model_servers` converts an existing workspace DeepSeek credential into a default server named DeepSeek with the same encrypted key, models and thinking level; rolling back restores it.

### Agent model catalog

`GET /agent-model-catalog?harness=<id>` (`dev_machine:read`, like the model server list) returns `{harness, models: [{id, name, efforts}], source, updated_at, stale, unavailable}`. Pickers show the display name and ID and save the ID. An empty list of `efforts` means any thinking level is accepted. The profile sheet accepts custom models. The task composer and approval card offer a model server's known list when a server is selected, and accept custom models when using a harness's own sign-in.

- **OpenCode** (`source: "opencode-go"`): the public model list of OpenCode's Go plan, `GET https://opencode.ai/inference/go/v1/models`, read without a key and with the User-Agent `kuayle`. The address is a constant in `BE/internal/service/agent_model_catalog.go`; no request or setting changes it. IDs are prefixed `opencode-go/`. Each model's `capabilities.reasoning.effort` is mapped onto Kuayle's levels: values Kuayle has are kept, `none` is `off`, others (`minimal`) are dropped, and `off` (no variant, the model's default) is always allowed.
- **Codex and Claude Code** pickers (`source: "workspace"`) refresh by reading the server lists saved in the workspace. Workspace owners and admins can update those lists with **Fetch models** in Agent configuration, then save the server. When the workspace's default model server speaks the agent's protocol it runs every profile of that agent and refuses a model outside its list, so the catalog is that list. Otherwise the agent runs on its own sign-in, which takes any model, and the catalog is the models of every server that speaks its protocol plus the agent's [workspace default](#agent-model-defaults).
- Other agents: an empty list.

The Go plan list is fetched with the model discovery client (public addresses only unless `AI_ALLOW_PRIVATE_NETWORK` is set, no redirects or proxy), within 10 seconds, reading at most 2 MB and keeping at most 500 chat models whose ID fits the model-name pattern. Display names are stripped of control characters and cut to 80 characters. The list is cached in memory per API process for one hour, and concurrent refreshes share one fetch. `refresh=true` bypasses the one-hour TTL and uses a three-second minimum gap between fetch attempts. Once a fetch finishes, refreshes within that gap return the cached result. Automatic retries and save-time rechecks use a 30-second gap. A failed fetch returns the last successful list with `stale: true`, or an empty list with `unavailable: true` if no fetch has succeeded. Both responses have status `200`.

**Outbound access.** The API server makes an HTTPS request to `opencode.ai` the first time someone opens an OpenCode model field or saves an OpenCode Go profile, and about hourly while they keep doing so. It also fetches on explicit refresh. A deployment that blocks that egress shows a stale or unavailable list and still accepts typed models.

**Choice-time check.** A fresh OpenCode Go list validates `opencode-go/` profiles on save and explicit `effort` in assistant delegation or follow-up requests. A missing model gets one rate-limited recheck, and an invalid effort reports the allowed levels. A stale or unavailable list leaves validation to the harness. The check waits up to 3 seconds for a fetch. Other OpenCode providers and ordinary task starts, retries or workflow steps skip the catalog check.

### Agent model defaults

Settings → Agent configuration → Default model and effort sets, per workspace, the model and effort Codex and Claude Code use on their own sign-in (`agent_model_defaults`, one row per workspace and agent; migration `000109_agent_model_defaults`). `GET /agent-model-defaults` (`dev_machine:read`) lists both agents, configured or not, with the accepted `thinking_levels`; `PUT /agent-model-defaults/:provider` (`dev_machine:admin`, owners and admins only) takes `{"model","thinking"}` with the same model-name pattern and levels as runs, and both empty removes the default. For production without a model server, setting Claude Code to `claude-opus-5-5` and `medium` makes its runs start with `--model claude-opus-5-5` and `CLAUDE_CODE_EFFORT_LEVEL=medium`.

The default is resolved when each run is created (`createAgentRun`), not when a task is accepted: tasks keep storing only an explicit choice, and a run fills each of model and thinking that it left empty. Every new task, including those delegated from chat (`delegate_issue`) and automations, and every new attempt of an open task (retry, follow-up, sign-in continuation, review) therefore uses the default current at that moment. A run that already exists keeps the model and effort recorded on it, including while it is recovered, and resuming a Claude Code session in a new attempt applies the current default to that attempt. A run that uses a model server, chosen or the workspace default speaking the agent's protocol, ignores the agent default; the server's default model and thinking level apply as before. IDE and terminal shells are not affected.

Migration `000109` also widens the `thinking` checks of `dev_machine_agent_runs` and `agent_model_servers` to `medium` and `xhigh`. Rolling it back drops the defaults and maps `medium` to `high` and `xhigh` to `max` on runs, servers and tasks, which is not reversible.

## Scoped Settings, Worktrees, and Environments

Development defaults are stored in `dev_machine_scope_settings`. A setting may provide a linked GitHub repository, base branch, development environment, Compose file for checks (migration 68), preview command and preferred port (migrations 71 and 85), preferred coding agent and/or agent instructions (at most 8000 characters), and the pull request description template (migration 172, at most 8000 characters using only `{{description}}`, `{{changes}}`, `{{verification}}`, `{{screenshots}}`, `{{notes}}` and `{{issue}}`) for a workspace, team, project, or issue. Each value is resolved independently in this order: issue, project, team, workspace (migration 65). The preferred agent is the default for task delegation from chat and the task composer when the workspace policy allows it; instructions and the Compose file path are snapshotted onto each task when it is accepted or retried and placed ahead of the task in the native prompt. This lets an issue override only the repository while inheriting an environment, or vice versa.

`GET /dev-machine-repository-defaults` (`dev_machine:read`, optional `issue_id` or `project_id`) reports the effective repository with the scope that supplied it, the value inherited without the most specific scope's own setting, and the linked repositories the policy allows. Issues and projects are read with machine-creation visibility (public teams of the request workspace). A refused default is reported with `allowed: false` rather than replaced. The create-machine dialog preselects the effective repository and sends `repo` only for an explicit choice, so the server keeps applying the configured repository and base branch; the issue defaults dialog names the inherited repository. The assistant's `get_issue` reports an issue's `development_repository`, and `delegate_issue` resolves it when approval is requested: it asks the user only when no scope configures one, refuses a repository the policy disallows, and shows the resolved repository on the approval (the task resolves it again when it runs). A task with no resolvable repository is blocked with a reason naming where to set one.

Machine creation accepts optional issue/project/repository information but does not require it. If no repository resolves, legacy repository fields remain empty and the machine starts as a generic developer workspace. When an issue is opened, `POST /dev-machines/:machineId/checkouts` resolves the issue's development settings, enforces the machine's repository affinity, and creates an idempotent issue worktree under `/workspace/tasks/{issue-key}`. A machine can hold multiple ready manual checkouts from the same repository. Task checkouts (`/workspace/tasks/{issue-key}-{task-id}`) do not use or change the machine's repository affinity, so one task machine serves several repositories; the environment must still match.

For placement and capacity, see [task machine reuse](#task-machine-reuse).

Each checkout is a worktree of a per-repository bare cache under
`/workspace/repos/{owner}/{repo}.git`. Older worktrees can still point at the
flat `{owner}-{repo}.git` cache. Preparing an existing worktree fetches again
and preserves its files. Kuayle checks the repository allowlist for every
checkout. Worktrees on the same machine share the creator's workspace volume.

The [checkout script](BE/internal/machine/docker_checkout.go) borrows the
cache's objects and refs in a temporary Git directory with isolated
configuration and disabled hooks. It fetches from the fixed GitHub URL with
a token scoped to that repository and removes the token before writing the
worktree. The IDE and terminal credential helper answers only for HTTPS on
`github.com`. Publication and worktree scans use the same Git isolation.
See [what publication runs](BE/docs/agent-tasks.md#what-publication-runs) and
[KS-39](SECURITY_AUDIT.md#ks-39--checkout-leaked-the-repository-token-critical).

Development Environments are immutable local OCI images created from owner/admin-created Environment Builder machines. Environment Builders run the developer container with a writable root filesystem so tooling can be installed intentionally. Snapshot requests are accepted only while the builder is `paused` or `stopped`; the environment moves `pending -> building -> ready` or `failed`. The snapshot commits the developer container layer with Kuayle environment/workspace labels, may leave a human-readable local tag (`kuayle/dev-environment-{id}:snapshot`), and records the immutable local image ID (`sha256:...`) as both `image_ref` and `image_digest` once ready. Machine creation uses only that immutable local ID, verifies the image labels match the expected workspace/environment, and never registry-pulls a missing local snapshot ID. The repository workspace named volume, `/run/kuayle-secrets`, `/tmp`, and other tmpfs scratch mounts are not part of the image. Selected shell and code-server customization is copied into an image template before commit; files in that template whose names contain `history`, `token`, or `credential` are removed. The complete writable developer layer is still committed, so operators must scrub any other sensitive data before snapshotting.

Environment deletion is two-phase. The API marks an environment `delete_requested`; the manager removes only the immutable local image ID after verifying the Kuayle environment/workspace labels and only then deletes the database record. Operators migrating hosts must move any required local OCI images separately from PostgreSQL and Docker volumes.

## GitHub Integration

Dev Machines use GitHub App installation tokens. The IDE and terminal use
the machine's root repository. Task publication uses the checkout's
repository, which may be any repository the policy allows. Task attempts
run without a GitHub token.

Required GitHub App repository permissions:

| Permission | Level |
|---|---|
| metadata | read |
| contents | read and write |
| pull requests | read and write |
| workflows | read and write for publication and PR branch updates or merges that change workflows |

Kuayle mints a fresh installation token when task publication opens or
updates the pull request.

Agent pull request descriptions are written by the agent in `.kuayle/pull_request.json` (description, changes, verification, notes, screenshots), sanitized, and rendered with the nearest scope's template or the default Description/Changes/Verification/Screenshots/Notes template. Empty sections are dropped. Screenshots the agent leaves in `.kuayle/screenshots/` are validated and committed through the Git data API to a separate `kuayle/screenshots/<task id>` branch. Commit-pinned links follow the repository's visibility, and the screenshot branch retains prior snapshots. When a new attempt updates an open pull request, Kuayle restores missing links from the latest screenshot snapshot and labels carried screenshots as earlier evidence. A new upload supersedes earlier evidence with the same filename. The description reports recoveries, omissions and screenshot branch failures for that publish. See [Pull request description](BE/docs/agent-tasks.md#pull-request-description-kua-210).

Branches, commits, and pull requests are persisted in `dev_machine_git_refs` with workspace, machine, run, issue, repository, and URL linkage. Existing GitHub webhooks continue to populate the issue development timeline.

Each workspace customizes its GitHub bot in Settings > GitHub > Kuayle bot (`workspace_github_bot_settings`, migration 119): a display name and profile picture that sign Kuayle's GitHub comments and agent pull request descriptions, and per-ability toggles enforced in `GitHubService` where the GitHub write happens (issue link comments, CI/review feedback to tasks, review comments, pull request actions, merges, releases). The opt-in `mention_reviews` ability handles `issue_comment` webhooks: an `@<app-slug> … review` comment by someone with write access to the repository (confirmed with GitHub for organization members and collaborators) on a pull request a task published starts the same review as an approved `request_agent_review`, without an approval card, as the task's creator. See [GitHub integration](BE/docs/github-integration.md#kuayle-bot-identity-and-abilities-kua-96).

Each issue checkout is an idempotent `dev_machine_checkouts` record with its own working branch and workspace path under `/workspace/tasks/{issue-key}`. Agents and terminal sessions can target a checkout; if multiple checkouts exist, agent runs must name the checkout to avoid ambiguous writes.

## Forge Integrations

GitLab (gitlab.com and self-managed), Gitea and Forgejo link issues through the
same normalized forge abstraction as GitHub. `domain.ForgeWebhook` verifies and
parses webhooks, `domain.ForgeCommenter` posts the issue link comment, and
`domain.ForgePublisher` is the declared extension point for agent publishing
and Bitbucket.

Connections are admin-configured per workspace (`workspace:manage`) with a base
URL, webhook secret and optional access token. Secrets are encrypted with the
same key handling as other integration credentials and are never returned by
the API. Each connection has its own webhook URL,
`POST /api/forges/webhook/:connectionId`; the connection ID selects the secret,
and an unknown ID, bad token or bad signature answers `401` before any payload
field is trusted. `FORGE_WEBHOOK_BASE_URL` overrides the origin shown in that
URL; without it the URL follows `FRONTEND_URL` (with `:5173` mapped to `:8080`
in development). Deliveries are processed at most once per connection in
`forge_webhook_deliveries`; a delivery without an ID header uses the SHA-256 of
the body.

The new tables (`forge_connections`, `forge_repos`, `forge_merge_requests`,
`forge_branches`, `forge_commits`) mirror the GitHub ones with `forge_repo_id`,
so GitHub data and behaviour stay unchanged; issue activity merges both sources.
The GitHub forge adapter decodes the same `ghWebhook*` payload types as the live
GitHub processor, which keeps its own flow for installations, the bot, check
suites and reviews. Linking rules and status automation are shared with GitHub: forge connections
seed the `branch_created`, `pr_opened` and `pr_merged` defaults in
`github_auto_transitions` only when missing, and edits made in GitHub settings
apply to every forge. Unknown or inactive repositories and unsupported events
are acknowledged and ignored.

Outbound calls dial through `validate.PublicAddressControl` with no redirects.
`FORGE_ALLOW_PRIVATE_NETWORK=true` opts into private/loopback base URLs (and
plain http) for self-hosted forges on a LAN. Setup, events, token scopes and
extension points are documented in
[Forge integrations](BE/docs/forge-integrations.md).

## Single Sign-On (OIDC)

OIDC sign-in is per workspace and free. `workspace_sso_configs` holds the issuer,
client ID, encrypted client secret, allowed domains, provider name, default role
(`member` or `guest`) and the `enabled`/`require_sso` flags. The client secret is
encrypted with `crypto.DeriveKey(JWT_SECRET + ":sso")` and is write-only: API
responses expose only `has_client_secret`, and no log line includes it.

`SSOService` runs the authorization-code flow with PKCE `S256`, a random state,
a random nonce and a browser-binding cookie. The server keeps the flow in
`sso_login_states` keyed by the SHA-256 of the state for ten minutes and consumes
it once with `DELETE … RETURNING`. Discovery, JWKS and token calls use
`pkg/forge.NewHTTPClient`; `OIDC_ALLOW_PRIVATE_NETWORK=true` permits a private or
plain-http issuer. `github.com/coreos/go-oidc/v3` verifies the ID token signature,
issuer, audience, expiry and nonce; a missing `email_verified` is unverified.

The callback is per workspace (`/api/auth/sso/:slug/callback`) and refuses a state
minted for another workspace. Identities are unique on
`(workspace_id, issuer, subject)`, so a configuration in one workspace cannot
authenticate into another. First sign-in creates the account with a random
unusable password and adds the workspace membership in one transaction; an
existing account links itself from an authenticated session. An active SCIM-created
account can bind its first verified IdP subject in the workspace that provisioned it.

SSO sessions carry the `sso_ws` JWT claim and `refresh_tokens.sso_workspace_id`,
so refresh rotation keeps the minting workspace. `WorkspaceMembership` answers
`403 SSO_REQUIRED` when the workspace requires SSO, the session was minted
elsewhere and the member is not an owner; owners keep the break-glass password
path. Password and passkey sign-in are refused with `SSO_REQUIRED` only when every
workspace of the account requires SSO and the account owns none.

PATs and CLI connection links record the minting session's SSO workspace in
`personal_access_tokens.sso_workspace_id` / `cli_connection_links.sso_workspace_id`
(migration 000159; a redeemed link copies it to its token).
`TokenService.ValidateScopesAndWorkspaces` is the one rule for token create and
CLI link create/redeem: an SSO-scoped session may mint only for exactly its own
workspace (`SSO_SESSION_SCOPED`), and a workspace that requires SSO is refused
(`SSO_REQUIRED`) unless the session is SSO-scoped to it; an all-workspaces list
is refused when any of the user's workspaces requires SSO. `WorkspaceMembership`
repeats the check per request for PAT callers (`TokenSSOWorkspaceKey`), with no
owner exemption, so turning require-SSO on stops older tokens.

Instance-level variables: `SSO_OIDC_WORKSPACE_SLUG`, `SSO_OIDC_ISSUER`,
`SSO_OIDC_CLIENT_ID`, `SSO_OIDC_CLIENT_SECRET`, `SSO_OIDC_ALLOWED_DOMAINS`,
`SSO_OIDC_DEFAULT_ROLE`, `SSO_OIDC_REQUIRE_SSO` and `OIDC_ALLOW_PRIVATE_NETWORK`.
They win over the database configuration for that workspace and make the settings
page read-only. `SSOService` caches the instance workspace ID; a slug with no
workspace means the instance config does not apply (a warning is logged, and
the miss is retried after 30 s). Shared issuers (Google, Microsoft
`/common|organizations|consumers`) need allowed domains
(`validate.SharedOIDCIssuer`), both in `SaveConfig` and at startup. The Keycloak development profile and provider notes are in
[docs/sso.md](docs/sso.md). SAML 2.0 shares the same login state, provisioning and workspace-scoped sessions. The POST callback verifies a signed assertion using the workspace’s stored IdP metadata, its SP entity ID, ACS and one-use AuthnRequest ID. SAML identities have a protocol-prefixed issuer to isolate them from OIDC subjects.

## SCIM 2.0 provisioning

The provider endpoint is `/api/scim/v2/:slug`. The separate owner/admin token
endpoint returns a newly rotated bearer token once. The workspace config keeps
only its SHA-256 digest. The protocol uses the matched workspace ID for every
resource query. Workspace config row locks serialize writes with rotation,
revocation and mapping updates.

SCIM resources belong to accounts created through this workspace's provisioner.
Deactivation commits the status, existing workspace membership cleanup, group
membership removal, and credential revocation in one PostgreSQL transaction.
The version on the account invalidates its existing access JWTs. Refresh rows
carry the same version, including during rotation. Legacy tokens start at
version zero. PAT and CLI link issuance from an interactive session also checks
that version while locking the account. Deprovisioning revokes outstanding CLI
links before PATs so in-flight link redemption cannot leave a working PAT.

Group-to-team mappings are explicit and unique per team. The grant table records
which team memberships came from SCIM, allowing the provisioner to remove its
own grants and preserve manually assigned members. See the [self-hosting guide](docs/self-hosting.md#scim-20-provisioning) for provider setup.

## Activity and Logs

| Event | Source |
|---|---|
| file created/modified/deleted, or one `files.changed` summary (counts and 25 sample paths) when a 2-second scan sees more than 25 changes | collector filesystem scan, skipping `.git`, `node_modules`, build and cache trees and `.kuayle/tmp` |
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

Structured events use a monotonic `BIGSERIAL` cursor. Completed autonomous agent stdout/stderr uses cursor-addressable `dev_machine_log_chunks`; chunk writes are idempotent per stream and sequence, so a slow or failed write is retried (15 seconds an attempt, for up to three minutes) before the run fails with `agent_output_persistence_failed`. If stopping such a run's container fails, the manager kills it. The schema reserves a PTY stream, but native terminal and interactive-agent PTY output is not currently persisted. Issue worktrees do not install the root-checkout Git hooks. API reads are workspace-scoped. High-volume log retention should be configured and periodically pruned by operators.

Dev Machines live in Settings → Dev Machines, which has two tabs: **Machines** (the user's machines, manual creation for hand-run development and the admin cleanup of old machines; members and above) and **Policy** (the workspace policy, environments, defaults and automations; editable by owners and admins). The sidebar has no Dev Machines entry. `/:slug/machines` redirects there, keeping an `issue_id` filter, and `/:slug/machines/:machineId` stays the machine page that task cards and issues link to. The machine page exposes status, services, resource samples, cursor-paginated events and logs, checkouts, terminal sessions, app ports, lifecycle/delete controls and **Tasks on this machine**: the agent tasks that used it (active first, then the most recently updated, at most 30), each with its issue, state, agent and a link to its chat. It reads `GET /dev-machines/:machineId/tasks`, a session-only route that answers only the machine's owner (others get 404) and applies the same workspace-membership, task-creator and issue-team checks as the agent queue. Native terminal attachment is implemented in the Kuayle UI with `@xterm/xterm`; ttyd's own web page is not exposed.

## Database Schema

Migration `000033_dev_machines` creates:

| Table | Purpose |
|---|---|
| `dev_machines` | Legacy repository fields, issue/project, desired/observed state, size, runtime names, failures |
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
| `agent_model_servers` | Workspace model servers: protocol URLs, encrypted API key, model IDs and display names, default and fast model, thinking level, default flag |
| `agent_model_defaults` | Workspace default model and effort per coding agent on its own sign-in |

The migration creates the final scoped-workspace schema directly. Composite foreign keys bind workspace, machine, service, checkout, run, session, and environment tuples so cross-workspace identifiers are rejected by PostgreSQL rather than relying only on application checks. Rolling migration `000033` back is destructive for all Dev Machine control-plane records.

Migration `000051_dev_machine_agents_headless` adds the `dev_machines.profile` value (`standard` or `headless`) and expands only the default workspace-policy provider allowlist to all seven built-ins. Existing workspace policies are not rewritten, so administrators retain their current allowlists until they opt in. The down migration removes the profile column and restores the previous default allowlist. Pi was added later without a migration: it is in the service's default allowlist for workspaces without a policy (`GetPolicy`) and in the Coding agents switches, while saved policies and the column default (used by no application write) are unchanged, so administrators opt in.

Migration `000052_dev_machine_deepseek` adds workspace model credentials and persists each agent run's non-secret command environment so a Claude Code effort override survives queueing.

Migration `000089_dev_machine_docker` adds `dev_machine_workspace_policies.allow_docker` (default false), the `docker` service type, `dev_machine_ports.target` (`developer` or `docker`) and `dev_machines.docker_listening_ports`. Its rollback deletes `docker` service rows (tear such machines down first; their containers and volumes are otherwise left for manual removal) and port targets; ports are kept and stay unlimited in number.

Migration `000090_dev_machine_task_resources` adds `dev_machine_workspace_policies.task_machine_cpu_millis`, `task_machine_memory_mb` and `task_machine_disk_gb` (defaults 4000, 8192, 20, applied to existing workspaces), at least 1 GB per vCPU, an even task disk within `max_disk_gb`, and an even `max_disk_gb` (odd limits are rounded down, irreversibly). Its rollback drops the columns and constraints; rounded limits keep their even values.

Migration `000093_dev_machine_sudo` adds `dev_machine_workspace_policies.allow_sudo` and `dev_machines.sudo_enabled` (both default false). Its rollback drops both columns; containers already created with sudo keep their configuration, so delete those machines first.

Migration `000101_dev_machines_persistent` removes machine lifetimes: it drops `dev_machines.expires_at`, `max_runtime_minutes` and `keep_running` (with the expiry and idle indexes) and `dev_machine_workspace_policies.max_runtime_minutes` and `idle_pause_minutes`, makes `dev_machine_tokens.expires_at` nullable and clears the expiry of the machine's own credentials. A teardown the old expiry reaper queued but never leased is cancelled and the machine is stopped instead. Columns are dropped rather than kept unused because the self-hosting updater never runs an older image against a newer schema (a failed update is recovered from its backup); rollback restores them with new lifetimes (see [Lifecycle](#lifecycle)).

Migration `000156_machine_task_limit` adds `dev_machines.max_tasks` (integer, NULL or 1 to 10). NULL follows the workspace policy's `task_machine_max_tasks`; a value overrides it for that machine. Its rollback drops the overrides, so every machine follows its workspace policy again; no other data changes.

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
GET    /api/workspaces/:slug/dev-machine-merge-methods
PUT    /api/workspaces/:slug/dev-machine-merge-methods
GET    /api/workspaces/:slug/dev-machine-scope-settings
GET    /api/workspaces/:slug/dev-machine-scope-setting
GET    /api/workspaces/:slug/dev-machine-repository-defaults
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
GET    /api/workspaces/:slug/agent-model-defaults
PUT    /api/workspaces/:slug/agent-model-defaults/:provider
GET    /api/workspaces/:slug/agent-model-catalog

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
  "env_vars": []
}
```

Create requests accept `profile: "standard" | "headless"`; omission means standard. Headless requests must omit both `services.ide` and `services.browser`.

### Public OpenAPI reference

The stable, token-accessible part of the HTTP API is described by an OpenAPI 3.1
document generated from the route registry and the DTOs, served at
`GET /api/openapi.json` and rendered at kuayle.com/api-reference. Routes outside
it, including every Dev Machine route above, are internal. See the
[API guide](docs/api.md) for the generator and the tests that keep it in sync.

### Outgoing webhooks

Workspace webhooks (Settings → Webhooks, `/api/workspaces/:slug/webhooks`)
receive `issue.*` and `comment.*` events; the API guide in the OpenAPI document
is the receiver contract (envelope, `X-Webhook-Signature`, `X-Webhook-Event`,
`X-Webhook-Delivery`, events). Write paths call `WebhookService` after their
write commits ([webhook_events.go](BE/internal/service/webhook_events.go)); a
failed emit is logged and never fails the write. `comment.updated` comes from
resolving, reopening and an author's edit that changes the body;
`comment.deleted` carries the comment as it was before deletion. Restoring a
deleted comment, like restoring an issue, sends no event.

Delivery is durable (migration 167, KUA-197):

- Emitting lists the workspace's active, subscribed webhooks (none while the
  workspace has or had a private team) and stores one `webhook_deliveries` row
  per change and webhook with the exact signed body. Bounds are store counts
  checked under a per-workspace advisory lock: 1024 pending rows and 8 MiB per
  workspace, 256 per webhook, 16384 rows and 128 MiB in all. Rows beyond them
  are dropped and logged.
- Each instance runs four workers, woken by its own emits and every two
  seconds. A worker claims the `webhook_lanes` row of the workspace whose turn
  is oldest among those with a due row (`FOR UPDATE SKIP LOCKED`), so a
  workspace has one delivery in flight across instances and workspaces take
  turns. In that transaction it takes the shared privacy guard (a workspace
  that turned private gives all its pending rows up as `workspace_private`),
  gives up rows older than a day (`expired`), and sends the earliest due row
  to a webhook that is not paused. The transaction stays open until the
  outcome is recorded, so a crash or shutdown leaves the row pending without a
  counted attempt; `main` starts the workers at boot and closes them on exit.
- A non-2xx answer, timeout or network error is retried after 30 s, 2 min,
  10 min, 30 min and 2 h; the sixth failure marks the row `failed`. The
  `webhook_circuits` row counts consecutive failures per webhook and pauses it
  for five minutes after three; while paused its rows wait. A success clears
  it.
- Finished rows drop their body and stay in the delivery log
  (`GET .../webhooks/:id/deliveries`, Settings → Webhooks) for seven days; an
  hourly job purges older ones.

Outbound requests dial only public addresses, follow no redirects and log the
webhook ID, never its URL. Creating or enabling a webhook in a workspace that
has or had a private team is refused with 403
`PRIVATE_WORKSPACE_OPERATION_DISABLED`, like the other privacy refusals, using the same `workspace_privacy`
predicate as emitting; the settings page explains why from the workspace's
`privacy_enabled`.

## Workspace roles

Migration 202 seeds the four built-in role definitions for every workspace.
Older installations can have several owner memberships in one workspace. Before
changing any memberships, migration 202 stops and lists the affected workspace
slugs. Review the rows against `workspaces.owner_id` and explicitly reassign
the additional owners. Once the failed transaction has rolled back and the
database is confirmed to be at schema 201, run `server migrate force 201` and
retry `server migrate up`. This preserves existing grants until an operator
has chosen how to resolve the conflict.

`workspace_members.role_key` identifies the assigned role and
`team_role_overrides` stores explicit exceptions. The legacy `role` column
gives custom memberships and memberships with team exceptions guest privileges
in features that still check role names. The API reads the current permission
set on each request. Permission checks under `/teams/:teamId`, including
cycle operations, use that team's role. Routes under `/issues/:identifier`
use the issue's stored team, and issue collection queries check each selected
row.

Resource routes such as `PATCH /projects/:id` and `GET /views` neither
resolve a team nor check grants per row. They require the permission in
the workspace role and every team override. For example, a workspace Guest
with an Admin override in a team they belong to can manage that team's cycles.
They cannot update a project there because their workspace role lacks
`project:manage`.

In Settings → Members, owners can manage role definitions, see member and team assignment counts, and set team overrides. Member listings return the assigned key and its display name. A workspace detail response includes `can_manage_members` for the current caller.

Custom roles with `workspace:manage` can configure webhooks and GitHub integrations.
The separate `workspace:transfer` grant permits downloading a workspace archive,
including attachments. Managing roles and deleting the workspace still require the owner.

The interactive owner manages definitions at `/api/workspaces/:slug/roles`.
Within that workspace, `GET /roles/permissions` returns grouped codes and hints.
A create request takes a name, description and either a permission list or a
built-in template key. Change a membership through `PATCH /members/:userId`.
List its exceptions at `GET /members/:userId/team-roles`. Use `PUT` with a
role key or `DELETE` on `/members/:userId/team-roles/:teamId` to set or clear
an exception. Membership management requires the workspace `member:invite`
grant. Updating or removing a member also requires authority over that
member's current permissions in the workspace and in each team, including
any PAT scope limits. Assigning a new role checks its grants against the
caller's workspace role and every team override. For example, an admin with
a Member override can invite a Member, but a Guest override limits the
invitation to grants available to a Guest. Invite links support Member and Guest.
Use a direct invitation or assign the role after joining to give a member a custom role.

Administrators retain implicit access to private teams where
their effective role is still administrative. A team override alone cannot
give a non-administrator implicit access to a private team. Members with an
override or custom role can leave an explicit team membership in an interactive
session. Leaving the last membership in a public team preserves the team if the
delegated role lacks `team:manage`. Issue PATCH accepts the issue’s current `team_id` and rejects a
different team before applying any edits.

Custom roles and members with any team override use guest-level checks in
legacy features without a matching grant. This applies workspace-wide, even
if other teams inherit the member's ordinary role. Interactive sessions can use
routes open to Guests. Routes that require a built-in admin role, including
import jobs, SSO configuration and SCIM administration, still enforce that check.
Assistant chat and assigning agents to issues also require a non-Guest membership.
Workflow and agent-task reads use their respective permission codes.
Background workers for the conversation queue, Telegram deliveries, agent
messages and agent signals also skip these members. Clearing the last
override restores the built-in role's usual access.

After applying migration 202, rerun the machine gateway database provisioner
so it can read `dev_machine_role_access`. To downgrade, move members to built-in
roles, clear team overrides and delete custom definitions first.

## MCP server

`POST /mcp` is a stateless Streamable HTTP [MCP server](docs/mcp.md) in
[`BE/internal/mcp`](BE/internal/mcp). It accepts personal access tokens only and has
no logic of its own: each tool and resource is a row naming an API route, and a call
is dispatched in-process through the application router with the caller's token and
client address. The `scoped` annotation, token scope, workspace RBAC, team privacy
and validation of that route therefore apply unchanged, and services record the
token's user. `/mcp` has its own per-address rate limit.

## Command-line interface

The `kuayle` CLI and Rust SDK live in [`CLI/`](CLI/) (MIT, originally by @uiYzzi;
see [NOTICE](NOTICE)). The CLI is a REST client authenticated by a personal access
token: every call is checked by the same `scoped` route annotation, token scope and
workspace RBAC as any other PAT request, and it adds no server permissions.
[`docs/api-tokens.md`](docs/api-tokens.md) generates the endpoint-by-endpoint
scope matrix and records why the remaining `sessionOnly` routes stay interactive.

`kuayle auth connect <link>` configures a profile from a connection link instead
of a copied token. The link keeps a one-time code (`kuayle_cl_`) in its URL
fragment; `POST /api/cli/connect` redeems it for a newly minted PAT with the
scopes, workspace restriction and expiry fixed when the link was created. Links
expire (15 minutes by default, 24 hours maximum), are single-use unless created
with more uses, and can be revoked without touching tokens they already minted.
A password reset revokes open links and tokens together. The CLI verifies the new
token, writes credentials atomically, and on any failure restores the profile
snapshot and revokes the token via `DELETE /api/tokens/current`; codes and tokens
are redacted from output and logs.

Cycle, view, favorite, notification, preference and issue-subscription writes are
reachable with tokens holding `cycle:manage`, `view:manage`, `account:write` or
`notifications:write`. Agent task reads (queue, task, history, runs) and workflow
reads (list, get, agent profiles) accept tokens with
`agent_tasks:read` / `workflows:read`; only owner, admin and member roles hold these
codes, and the repositories apply the same task and member visibility as for
sessions. Task writes, run logs, diffs, artifacts, sessions and live messages stay
session-only. Wiki and registry commands use the
`/teams/:teamId/wiki` and `/projects/:id/wiki` routes with `wiki:read` and
`wiki:write`; restoring a revision is an ordinary versioned update, as in the web
app. Releases are `cli-v*` tags built by `cli-release.yml`; installers and
`self-update` verify SHA-256 checksums from the official repository only. User
guide: [docs/cli.md](docs/cli.md).

## Failure Handling

| Failure | Behavior |
|---|---|
| API or manager restart | Pending/expired leases remain in PostgreSQL and are re-leased; active operations renew their lease every 30 seconds |
| Image pull or container creation failure | Bounded retry; partial resources removed before retry |
| Docker unavailable | Manager readiness fails; operations remain durable |
| PostgreSQL unavailable | Running containers continue; manager/gateway report degraded health |
| Service unavailable | Gateway returns 502 and records an access/proxy error |
| Opening paused service | API queues an idempotent resume and returns `resuming` with retry guidance |
| Permanent delete | Admin request is persisted and hides the machine; a destroyed machine is purged at once, otherwise the manager queues teardown and purges after it. Records with agent task history stay hidden until no task needs them |
| Disk threshold reached | Resource event recorded and stop operation queued |
| Teardown repeated | Missing containers/networks/volumes are treated as already removed |
| Agent timeout | Container stopped; run result normalized to `timeout` |
| Agent cancellation | Cancel operation removes the run container |

The manager exposes `/health`, `/ready`, and Prometheus-text `/metrics`. Structured logs include workspace, machine, run, operation/provider where available, event type, and duration. Secret values and full environment maps are not logged.

## Installable app

The UI is an installable web app (PWA) in production. `UI/static/manifest.webmanifest` names it Kuayle (`start_url` and `scope` `/`, `display: standalone`, the default dark theme's `#111111` as theme and background colour) with 192/512 px icons, a 512 px maskable icon and `apple-touch-icon.png`. The PNGs are generated from `UI/static/favicon.svg` by `UI/scripts/generate-app-icons.mjs` (Playwright's Chromium, deterministic) and committed. `UI/src/app.html` adds the manifest and Apple touch icon links and the iOS home-screen tags; the `theme-color` meta follows the active theme's `--color-bg`.

`UI/src/service-worker.ts` precaches only the built immutable app chunks and public shell files (favicon, manifest, icons, `offline.html`) and serves them cache-first. API requests, WebSockets, requests with credentials headers, other origins, release data and page HTML are never cached; navigations always go to the network, falling back to the static offline page only when it fails ([policy](UI/src/lib/features/pwa/service-worker-policy.ts)). The worker is registered by the app itself (`serviceWorker.register` is off in the `sveltekit()` options of `UI/vite.config.ts`) as an ES module, because SvelteKit 3 bundles it as one, so browsers without module service workers (Firefox before 147) run without it. It is registered only in production builds and never under browser automation unless a test sets the `kuayle-service-worker-tests` localStorage key. A new deployment installs in the background and waits: an "update available" toast reloads into it on request, otherwise it activates once every Kuayle tab has closed. The UI Caddyfile serves the manifest as `application/manifest+json` and `service-worker.js` with `Cache-Control: no-cache`.

Launched as the installed app (display mode `standalone`, `fullscreen` or `window-controls-overlay`, or iOS `navigator.standalone`), the page locks zoom: [installed-viewport](UI/src/lib/features/pwa/installed-viewport.ts) adds `maximum-scale=1, user-scalable=no` to the viewport meta (which also stops iOS focus zoom on small inputs), sets `data-installed-app` on `<html>` so `app.css` applies a zero-specificity `touch-action: pan-x pan-y` (no pinch or double-tap zoom; explicit `touch-*` utilities still win), and cancels Safari pinch `gesturestart`/`gesturechange` events. Browser tabs keep the default viewport and normal zoom.

Installation requires HTTPS (or `localhost`). Chrome and Edge offer Install in the address bar or menu; on iOS use Safari's Share → Add to Home Screen.

## Self-Hosted Deployment

Outside `ENVIRONMENT=development`, `config.Load` and `config.LoadMachineGateway` refuse published secret values: anything starting with `CHANGE_ME` (the self-hosting example placeholders) or `local_dev_`, ending in `_do_not_use_prod` (the root Compose development defaults), or written as `<...>`. The check covers `JWT_SECRET`, the passwords inside `DATABASE_URL` and `DEV_MACHINE_GATEWAY_DATABASE_URL`, `SYSTEM_UPDATER_TOKEN`, `VOICE_INTERNAL_TOKEN`, `DEV_MACHINE_ENCRYPTION_KEY`, `SMTP_PASSWORD`, `S3_SECRET_KEY` and the GitHub App client and webhook secrets. Errors name the setting and never the value. The `dev-machines` profile preflight and `selfhosting/update.sh` apply the same rule before provisioning the gateway role or stopping services. The root development Compose stack keeps its literal defaults and publishes its ports on `127.0.0.1` only. `server migrate` does not load the full configuration and is not covered. See [Secrets must be generated](docs/self-hosting.md#secrets-must-be-generated). A database password equal to the Compose fallback (what the Compose files use when `POSTGRES_PASSWORD` is unset) is refused too, and `update.sh` refuses an unset `POSTGRES_PASSWORD`.

The optional [release updater](selfhosting/RELEASE_UPDATER.md) supports detached release checkouts and digest-pinned production Compose deployments. It exports release source without changing the checkout, prepares images before downtime (pulling digest-pinned API, UI and, when active, Dev Machines control-plane/runtime images published by the release workflow; a Dev Machines image missing from GHCR is built locally from the release source), stops application/control-plane writers before a database backup and migrations, and waits for health before removing maintenance mode. Failure after migrations begin leaves writers stopped; it never restarts old binaries against a potentially changed schema. After migrations it records the dump in `backup_runs` (kind `pre_upgrade`).

Instance backups (migration `000164_backups`, `BE/internal/service/backup*.go`) are configured by sysadmins in Settings → Backups. `backup_settings` is a singleton row holding the schedule, retention, AES-GCM encrypted S3 credentials and passphrase (key derived from `JWT_SECRET+":backup"`) and a lease (`lease_token`, `lease_expires_at`) that allows one run at a time across replicas; each replica's scheduler claims `next_run_at` with compare-and-set every minute. `backup_runs` is the history; each run records the S3 endpoint and bucket its copy went to, and retention and Verify act only on copies in the bucket configured now. The S3 endpoint dials public addresses only unless `BACKUP_S3_ALLOW_PRIVATE_NETWORK=true`. See [Backups](docs/backups.md) for the archive and encryption format and the restore procedure.

### Instance configuration

The [configuration registry](BE/internal/config/registry.go) describes the
API process's settings, including the backup command's
`KUAYLE_BACKUP_PASSPHRASE`. Its [test](BE/internal/config/registry_test.go)
checks field coverage and secret classification. Settings → Instance
configuration shows this registry to sysadmins listed in `SYSADMINS`.

The API requires a sysadmin session, rejects SSO-scoped sessions and returns
`Cache-Control: private, no-store`.

| Route | Result |
| --- | --- |
| `GET /api/system/config` | Returns each variable's effective value and source (override, environment or default). Secrets expose only set/unset status. A URL carrying credentials, a query or a fragment is withheld. |
| `PUT /api/system/config/:name` | Validates and stores a runtime override in `instance_config_overrides`. Returns `400 INVALID_CONFIG_VALUE` for an invalid value and `409 CONFIG_READ_ONLY` for a startup-only setting. |
| `DELETE /api/system/config/:name` | Removes the override. The environment value or built-in default takes effect again. |

The runtime readers are `AgentTaskService.PipelinesEnabled`
(`WORKFLOW_PIPELINES_ENABLED`, default `true`), registration
(`DISABLE_REGISTRATION`) and backup runs and Verify (`BACKUP_TIMEOUT`).
Disabling pipelines prevents saving them or starting tasks on them. Existing
runs continue on their pinned version. A stored override wins over the
environment, which wins over the built-in default. The
[instance configuration service](BE/internal/service/instance_config.go)
caches overrides for five seconds per process. If the store is unavailable,
it uses its last successful read or the environment and defaults on first
use. Writes and resets record the actor and both values in the audit log as
`system.config_updated` and `system.config_reset`. See the
[operator guide](docs/self-hosting.md#instance-configuration) for editing
and resetting a setting.

The optional `searxng` Compose profile runs a pinned SearXNG image for the
assistant's `web_search` (see [self-hosted web search](docs/self-hosting.md#self-hosted-web-search-optional)).
It has no published port and shares only the internal `search-control` network
with the backend; `search-egress` reaches the upstream engines. It runs
read-only as the image's non-root user (977) with all capabilities dropped and
tmpfs for its config directory and cache; [settings.yml](selfhosting/searxng/settings.yml)
is mounted read-only. Its entrypoint refuses to start without a
`SEARXNG_SECRET` of at least 32 characters, which
[searxng.sh](selfhosting/searxng.sh) generates into `selfhosting/.env`; the
checked-in settings carry only SearXNG's placeholder, which it refuses as a
key. `searxng.sh enable` also sets the backend's `WEB_SEARCH_SEARXNG_URL` to
`http://searxng:8080`. While the service is running, `selfhosting/update.sh`
ensures the secret, pulls the pinned image before downtime and restarts
SearXNG after the application; a SearXNG failure there is a warning, not an
update failure.

| Variable | Service | Purpose |
|---|---|---|
| `WEB_SEARCH_SEARXNG_URL` | backend | SearXNG base URL. Operator setting validated at startup (http(s), no credentials, query, fragment, escapes or dot segments); the only private host and port `web_search` may dial, without redirects. Unset: SearXNG is not offered. |
| `SEARXNG_SECRET` | searxng | SearXNG `server.secret_key`, generated by `searxng.sh`; never committed. |

Dev Machines are an opt-in Compose profile.

Prerequisites:

- Linux Docker Engine and Compose v2;
- Docker's data root on XFS mounted with project quotas (`pquota` or `prjquota`); rootless or user-namespace Docker is not supported because the local volume driver cannot apply project quotas there. Hosts without project quotas may opt into `DEV_MACHINE_DISK_QUOTA_MODE=monitored` (see Disk Limitation);
- `FRONTEND_URL` set to the exact public Kuayle origin, including scheme and any non-default port, for native terminal WebSocket `Origin` validation;
- a separate registrable domain for machine workloads by default (e.g. `kuayle-machines.example.net`), or the explicit personal/trusted `DEV_MACHINE_ALLOW_SAME_SITE` opt-in;
- wildcard DNS for `*.${DEV_MACHINE_DOMAIN}`;
- production TLS via DNS-01 wildcard certificates, an operator-provided wildcard certificate, or restricted on-demand individual certificates (see [personal instances](docs/self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials));
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
| `DEV_MACHINE_EGRESS_ALLOWLIST` | Optional comma-separated public domain suffixes; when set, include each agent's provider hosts (for example `opencode.ai` for OpenCode with an OpenCode API key) |
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
| `DEV_MACHINE_AGENT_CLI_REFRESH_HOURS`, `DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS` | Agent CLI updates (defaults 6 and 48, each 0-720): how often the manager installs the latest npm-distributed agent CLIs into the shared read-only volume, and how long a version must have been published; the manager also checks the volume on every start. A refresh interval of 0 turns updates off and machines use their image's CLIs. See [Sudo in machines](#sudo-in-machines), "Agent CLI updates" |
| `DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS` | How long a stopping machine manager lets operations other than agent runs and checks finish before stopping them (default 90 seconds, 0 stops immediately). Interrupted assistant commands are marked failed. Other operations are handed to the next manager. Agent runs and checks are not bounded by it: they keep running and the next manager re-adopts them. Keep it at least 60 seconds below the manager's Compose `stop_grace_period` (150s), so cleanups finish. See [machine manager restarts](BE/docs/agent-tasks.md#machine-manager-restarts) |
| `DEV_MACHINE_SLICES`, `DEV_MACHINE_SWAP_PERCENT` | Shared machine resources (defaults `auto`, 50): with `auto`, new machines on hosts with Docker's systemd cgroup driver and cgroup v2 share their CPU and memory in one systemd slice, with a swap ceiling of the given percentage of the machine's memory (0-100); `off` keeps per-container shares. See [shared machine resources](#shared-machine-resources) |
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
- A task preview runs in its own container without the developer container's secrets, sign-ins or sudo, but, like every agent attempt and check, it shares the machine's workspace volume and private network. It can therefore change files a person later runs in the terminal, and reach the machine's other containers on that network, where code-server and the terminal listen without the gateway's authentication (SECURITY_AUDIT.md KS-17). A preview has the isolation of an agent attempt, not more. Review agent changes before running them yourself (SECURITY_AUDIT.md KS-24).
- Command and filesystem collection is best-effort and can be bypassed by trusted workspace code.
- Provider adapters can construct interactive invocations, but agent tasks queue autonomous agent runs and no interactive-agent attach endpoint is exposed. Direct interactive use of the built-in CLIs is available through Kuayle's native xterm terminal; ttyd's web page is not exposed.
- Workspace hard quotas require Docker local-volume project-quota support; the manager intentionally fails startup rather than run machines with an unbounded workspace.
- A separate registrable domain provides browser site isolation by default. Personal/trusted installations can explicitly opt into same-site machines with host-prefixed app cookies and exact-origin mutation/WebSocket checks. This reduces defense in depth and logs a startup warning. Localhost development remains exempt.

These limitations are deployment facts and must not be represented as stronger guarantees in product or security documentation.

### Platform controls outside Dev Machines

The October 2026 audit ([SECURITY_AUDIT.md](SECURITY_AUDIT.md)) added these invariants:

- Session JWTs carry `typ` (`access` or `refresh`). The API authenticates only access tokens; untyped legacy tokens are accepted only with the 15-minute access lifetime.
- Assets are served with their stored content type only when it is in `domain.AssetContentTypeExtensions`; any other type, including one restored from a workspace import, is served as `application/octet-stream` attachment. Import normalizes the type too.
- A workspace archive is caller-supplied input. Import passes its rich-text columns through the sanitizers the write paths use (`importedTextSanitizers` in `internal/service/workspace_transfer.go`: issue titles and descriptions, comment bodies, project names and descriptions, template descriptions, recorded description versions); a new archive column that holds HTML must be added there. In the UI, stored HTML is turned into text only through `$lib/security/plain-text` (an inert `DOMParser` document, never `innerHTML`), and links read back from stored content (wiki file list, attachment nodes) go through `safeFileUrl`, which keeps `http(s)` and `/api/` addresses only.
- Stored rich text keeps only the class names the editor and renderers emit: `editor-attachment`, `mention`, `mention-issue`, `registry-ref`, the editor link classes and `language-<name>` on code blocks. Both sanitizers drop every other class (`pkg/sanitize/editor_classes.go`, `$lib/security/editor-classes`), so stored content cannot use the app's utility classes to restyle the page. `BE/pkg/sanitize/testdata/editor_html_contract.json` is read by the Go, UI unit and browser tests; a new editor class must be added in both lists and that file. Issue template descriptions go through `sanitize.SanitizeEditorContent` on create and update, like issue descriptions.
- Dragging a table column in an issue description or comment saves its width in the existing HTML. Both sanitizers keep pixel `width` and `min-width` styles on `table` and `col`, integer width attributes on `col` (optionally suffixed with `px`), and comma-separated `colwidth` values on cells. A zero in a merged cell reserves an automatically sized column. The editor imports colgroup widths when cells lack `colwidth`, and read-only views use the stored colgroup directly.
- Images in stored rich text load on their own only when the app serves them (uploaded assets, signed asset links, anything same-origin). The backend keeps accepting `http(s)` images and stored content is never rewritten; the rule is applied when rendering. `sanitizeHtml` (`$lib/security/sanitize`) moves the address of an image from any other host to `data-remote-src`, so rendering makes no request, and the `remoteImages` action on the document body (`src/routes/+layout.svelte`, `$lib/components/shared/remote-images.action`) puts a placeholder naming the host next to it, with "Load image" and "Always load from <host>" (kept per browser in `localStorage`). The editor's image node view follows the same rule, except for images the user pastes or drops in during the session. Because the sanitizer removes the `src`, a surface that renders sanitized HTML needs no code of its own, and one outside the action shows no remote image at all. `sanitizeEditorOutput` is the only variant that keeps remote addresses, for saving; release notes rendered through `renderMarkdown` with a link check load their already-restricted images directly.
- Outbound requests to workspace-chosen URLs (AI provider `base_url`) dial through `validate.PublicAddressControl`, never follow redirects, and keep transport errors server-side. `AI_ALLOW_PRIVATE_NETWORK=true` lets operators use a private-network provider, which trusts every workspace owner with internal access.
- With the shared GitHub App, installation callbacks require the installing user's OAuth code and the installation in `GET /user/installations`; enable "Request user authorization (OAuth) during installation" and set `GITHUB_APP_CLIENT_ID`/`GITHUB_APP_CLIENT_SECRET`. Being listed is not enough, because GitHub lists an installation to every user who can reach one repository it covers: the installation is bound only when its account, as GitHub reports it for that grant, is the OAuth user's own account (`GET /user`, compared by numeric id) or an organization where `GET /user/memberships/orgs/{org}` returns `role: admin` and `state: active` for the same organization id. That lookup needs the shared app's organization permission **Members: Read-only**; when it fails or is forbidden, and for any other account type, the callback returns 403 `GITHUB_INSTALLATION_UNVERIFIED` and binds nothing. Per-workspace (manifest) apps skip this check because only their own workspace can resolve their installations, so the manifest does not request the permission.
- Issue, project and template writes reject users who are not workspace members and labels, cycles and statuses from other workspaces; repository unlinks are workspace-scoped; the public GitHub webhook body is capped at 25 MB, and each signed delivery (`X-GitHub-Delivery`) is processed at most once (`github_webhook_deliveries`, kept seven days; see [replay protection](BE/docs/github-integration.md#replay-protection)).
- The API (Echo v5) and the machine gateway resolve the client address with `internal/clientip`: `X-Forwarded-For` is honored only through hops on loopback, link-local or private networks (the bundled Caddy and Compose networks) and the nearest public hop is used; `X-Real-IP` is ignored. A client connecting from a public address therefore cannot choose its rate-limit, login-throttle, abuse-guard or audit address. Do not publish the backend or gateway port to a private network whose clients are untrusted; such peers are treated as proxies.

The gateway also serves a private HTTP certificate-approval listener on port 8091.
It validates the exact requested hostname against a live route and checks running
status, desired status and expiry with a two-second database deadline and bounded
concurrency. This listener has no Docker or application credential access. It must
not be published; Caddy uses it to restrict on-demand issuance. Public workload
traffic remains on port 8090. See [personal instances](docs/self-hosting.md#personal-instances-same-site-machines-and-https-without-dns-api-credentials) for a stock-Caddy setup without DNS API keys.

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

## Foreign imports (KUA-100, Jira deliverable)

Workspace admins can import issues from an external tracker into an existing
workspace. This is distinct from workspace transfer, which builds a *new*
workspace from a `.kuayle.zip`. Only Jira ships in this deliverable; Linear,
GitHub Issues and CSV are future sources and share the framework.

Migration `000174_imports` adds three tables:

- `import_jobs` stores the source, site, opaque config/mapping/preview JSON,
  encrypted credentials, progress counters and status (`draft`, `previewed`,
  `queued`, `running`, `completed`, `failed`, `cancelled`).
- `import_job_items` is the per-item report: one row per skipped or failed
  source item with its type, source id, the Jira key of the issue it belongs to
  (`PROJ-123`, so the admin can find it in Jira) and the reason. A parent-link
  problem is a `parent` row naming the child issue.
- `import_external_refs` maps `(workspace, source, site, external type, external
  id)` to the Kuayle entity and a content hash. The unique key is what makes a
  re-run update or skip instead of duplicating; the original Jira key
  (`external_key`) and browse URL are kept here and as a reference line in the
  issue description, while Kuayle numbering remains its own
  (`IssueRepo.NextNumber`).

`ImportService` creates a draft job, previews it without writing anything
outside `import_jobs`, validates and stores the mapping, then queues the job.
The preview is stored in the API's JSON shape: `unmapped_fields` lists source
data no mapping can carry (custom fields, users without an email, unsupported
description nodes) and `mapping_gaps` lists previewed projects, statuses,
priorities and users the current mapping does not cover. Saving the mapping
recomputes `mapping_gaps` in the same statement, so the admin can preview first
and map afterwards. A previewed job can be previewed again; a queued, running
or finished job cannot, and that is checked before the source is read. A Jira
failure during preview (rejected credentials, unreachable or private address,
unexpected response) is a `502 IMPORT_SOURCE_FAILED` with a message naming the
cause and never the credentials, response body or request URL
(`jira.DescribeError`). Mapping targets come from `GET .../imports/options`.

The `source_site` part of the idempotency key is derived on the server from the
lower-cased host of the credentials' `base_url` (the same value
`jira.Client.Site()` returns); no client-supplied site is trusted, so importing
two Jira Cloud sites into one workspace cannot collide. A base URL with
credentials, a query or a fragment is refused. `ImportService.Run` is a polling
worker like the backup scheduler: it recovers interrupted jobs, claims queued
jobs with `FOR UPDATE SKIP LOCKED`, re-checks that the job's creator is still a
workspace owner or admin, and writes labels, sprint cycles, epic projects,
issues, sub-issues, comments (including a history summary), attachments and
relations. Each issue is created in a short transaction together with its
external ref; comments and attachments have their own refs so re-runs never
duplicate them. Container resolution errors (an unmapped project, a failing
label/cycle/project) are reported as per-item failures instead of aborting the
job. Ref lookup or write errors fail the affected item rather than being
silently treated as "not found".

An issue's hash covers its source content (fields, comment hashes, attachment
ids, history) and the Kuayle values it resolved to under the current mapping
(team, status, priority, assignee, project, cycle, labels, parent, unmatched
author). Unchanged issues are skipped; changing the mapping and re-running
updates them. A re-run writes title and description explicitly
(`IssueRepo.UpdateContent`). When an imported issue was moved to another team in
Kuayle, the update keeps its team-scoped values: only a status mapped for its
current team is applied, and its project and cycle stay.

Status mappings are keyed `<team_uuid>:<source status>`: two projects mapped to
different teams can map the same source status to different team statuses, and
the worker falls back to that team's default status when a status is unmapped
or its target belongs to another team. Priorities must be a Kuayle value (0..4).
User mappings are keyed by the lower-cased email, or by the source user id when
Jira hides the email, so every previewed user can be mapped; an empty key is
rejected. There is no labels mapping — source labels and components are matched
or created by name.

Imported titles go through `sanitize.PlainText`. Descriptions, comments and the
history summary are Markdown converted by `sanitize.EditorMarkdown`, which
treats a literal tag as text rather than switching the body to HTML. The
attribution line is built as HTML with the name escaped, and attachments are
the editor's own attachment nodes linking `/api/workspaces/<slug>/assets/<id>`,
the access-checked asset route in-app uploads use (never a storage or presigned
URL). Attachment links are rebuilt from their refs on every run, so updates and
a re-run after a failed issue create keep them. An imported issue is created
already triaged unless its team has triage enabled, and gets a `SortOrder`
derived from its Kuayle number like any in-app issue. On a re-run a comment or
history summary whose source changed is updated in place; unchanged ones are
left untouched. If the Kuayle comment was deleted or edited so it can no longer
be updated, the re-run records that as a skipped item instead of duplicating
it. A cancel requested mid-fetch stops the job before any Kuayle entity is
written (checked every 25 emitted issues).

Sprints are read from Cloud sprint objects and from the Server/DC
`com.atlassian.greenhopper...Sprint@...[...]` strings. An issue gets the cycle
of its active sprint, else a future one, else the last one listed; a new cycle
takes the sprint's dates and state (active → active, future → upcoming, closed
or unknown → completed). Existing cycles are matched by name and not changed.

Assignees and authors resolve differently: a missing, unmatched or explicitly
unassigned source user leaves an issue with no assignee, while an unmatched
author (reporter or comment author) is attributed to the importer with the
original name in the text. The importer fallback is for authors only.

`total`, `processed`, `created`, `updated`, `skipped` and `failed` count
issues only; comment, attachment, relation and parent-link outcomes appear
exclusively in the per-item error report. Skipped or failed items do not fail
the job.

Running jobs send a heartbeat (`updated_at`) during a long fetch, while
writing and before each attachment download. Stale recovery looks at a
heartbeat older than ten minutes rather than the start time, and runs both at
startup and once a minute, so a live job on a busy instance is never failed by
another replica's recovery pass.

Credentials are encrypted with `crypto.Encrypt` under a key derived from
`JWT_SECRET` (`:import`) and written to `credentials_enc`. They are cleared in
the same statement that sets a terminal status, again in a worker `defer` on
every exit path, and again whenever stale running jobs are recovered. Draft and
previewed jobs can be cancelled too: cancelling them ends the job immediately
and clears their credentials. `Run` also expires draft/previewed jobs idle for
more than 24 hours, cancelling them and wiping credentials in one statement;
the item report stays readable. Responses never contain credentials. All import
routes are session-only and require
the workspace owner/admin role; personal access tokens and SSO-scoped sessions
are rejected by middleware. Outbound requests dial only public addresses
unless `IMPORT_ALLOW_PRIVATE_NETWORK` is enabled for a LAN Jira Server/DC, and
redirects are not followed. Attachments use the shared `domain.MaxUploadSize`
(10 MiB) and the upload handler's type validation (`domain.AllowedUploadType`,
which ignores MIME parameters such as `charset`); oversized files are skipped
and reported. Imported files belong to the target team, so a private team's
attachments stay private.

The item report is paged: `GET .../items?limit=&offset=` returns at most `limit`
items (default 200, clamped by the service to 1..`repository.ImportItemsPageMax`
= 500) and a `has_more` flag. The service reads one extra row to set
`has_more`, and the repository clamp (`ImportItemsQueryLimit`) allows exactly
that extra row. The UI appends further pages with "Load more". Unknown jobs and
other workspaces' jobs get 404.

The whole worker path, including private-team writes, attachment storage and
idempotent re-runs, is covered against PostgreSQL by
`TestJiraImportWritesThroughTheRealStore` (needs `DATABASE_URL`).
Known limits: the fetch holds every issue in memory before writing (fine for
the ~1,000-issue target, not for 100k), and the update path is not one
transaction with its ref (a failure leaves a stale hash, so the next run
updates again; it never duplicates).

The UI for this step lives in `UI/src/routes/[workspaceSlug]/settings/import`:
the prerendered `WEB/` app is the marketing site, while admin settings pages
belong to the SvelteKit application. The mapping, progress and error-report
screens use the installed shadcn-svelte primitives.

## Soft delete and undo

Issues and wiki pages are soft-deleted (migrations `000165_issue_soft_delete_undo`
and `000166_wiki_page_soft_delete`). The tables are `issue_records` and
`wiki_page_records`; `issues` and `wiki_pages` are simple, automatically
updatable views over the rows whose `deleted_at` is NULL. Every existing
SELECT, INSERT, UPDATE, `RETURNING *` and `FOR UPDATE` keeps working through the
view and never sees a deleted row, so new queries need no filter of their own.

- Name the table only where deleted rows must be seen: restore, purge, issue
  numbering (`NextNumber`), team key renames, status deletion and membership
  cleanup. Foreign keys, indexes and triggers belong to the table.
- A new column goes on the table, and the view must be recreated with it (the
  view lists its columns explicitly and hides `deleted_at`/`deleted_by`).
- Trigger functions shared with other tables compare `TG_TABLE_NAME`; for
  `issue_records` they use the logical name `issues`, which is also what the
  private-asset reference index stores. Private-team integrity checks read
  `issue_records`, so a restore cannot bring back a crossing edge.
- Deleting a wiki page lifts its children to its parent, as before. A deleted
  registry page gives its day up (`deleted_registry_date`) so the registry can
  start a new page; restoring it takes the day back only if it is still free.
- Comments are soft-deleted differently (migration
  `000168_comment_soft_delete`): `comments` keeps its name, because trigger
  functions shared with other tables compare `TG_TABLE_NAME = 'comments'`, and
  gains `deleted_at`, `deleted_by` and `edited_at`. Every reader of comments
  must filter explicitly: `liveComment(alias)` in
  [comment.go](BE/internal/repository/comment.go) hides deleted comments and
  the replies of a deleted comment (backlinks and workspace export use it too).
  Only admins and owners delete (`DELETE .../comments/:commentId`, scope
  `issue:delete`) and restore; only the author edits (`PATCH`, scope
  `issue:create`). Every comment route checks the comment belongs to the
  issue in the path.
- `ISSUE_DELETE_GRACE_PERIOD` (default `720h`) is how long a deleted issue,
  page or comment can be restored (`POST .../restore`; 410 `GRACE_EXPIRED` after it). An
  hourly job in the API server hard-deletes older rows in batches of 500, which
  runs the cascades the hard delete used to run. Notifications and webhooks
  already sent are not retracted.

Every write to an issue row advances `issues.version` (a `BEFORE UPDATE`
trigger); label and assignee writes touch the row so they count too. An
undoable edit (`PATCH /issues/:identifier` or `/issues/bulk` changing status,
priority, assignees, labels, project, cycle or sort order with status) returns
`undo: {expected_version, previous}`. `POST /issues/:identifier/undo` and
`/issues/bulk/undo` lock the rows, refuse with 409 `EDITED_MEANWHILE` if any
version moved, and otherwise write the previous values in one transaction with
the usual reference and visibility checks, history and `issue.updated` events.
The UI offers the Undo in the change's toast for about eight seconds and on
Cmd/Ctrl+Z outside text fields (`UI/src/lib/features/undo`).

## Wiki page references

Issue descriptions, comments, wiki pages and assistant messages can reference a
wiki page of any team or project.

- **Stored form.** A reference in editor content is a mention chip that holds only
  the page ID: `<span data-type="mention" data-kind="wiki" data-id="<page>">`.
  `sanitize.SanitizeHTML` rewrites every wiki mention to that exact form
  (`sanitize.WikiMentionHTML`) and drops any label, text or nested markup sent
  with it, so stored content, revisions and realtime payloads never carry a page
  title. Because issue, comment and wiki writes all pass through the sanitizer,
  this holds for the web editor, the API, the CLI and agents alike. Only a
  mention closed by its own `</span>` loses its content; an unclosed one becomes
  the chip alone, so malformed input never swallows what follows it. Mention
  notifications read only user chips (`data-kind` absent or `user`). The chip's
  `mention-wiki` class is on the stored-class allowlist (`editor_classes.go`,
  `editor-classes.ts` and the shared contract file). Readers without chips (the
  assistant's issue and wiki tools, a coding agent's brief) get the content
  through `sanitize.StripHTMLKeepingWikiReferences`, where a chip reads
  `@[wiki page](wiki:<page id>)`; `get_wiki_page` accepts that page ID alone.
- **Rendering.** Chips ask `POST /wiki/references/resolve` what to show. Pages the
  caller can see through `wikiScopeVisible` (the predicate the wiki itself uses)
  answer with title and team or project; every other ID, whether invisible,
  foreign or unknown, answers `{id, accessible: false}` and renders as the
  "Restricted page" placeholder. `GET /wiki/references?q=` suggests pages for `@`
  with the same predicate, across all teams and projects of the workspace.
- **Attached references.** `issue_wiki_references` holds pages attached to an
  issue (the "Add wiki page" search of the issue page, or dropping a page from a
  wiki tree onto it; the UI offers both only with `issue:update`). Attaching needs
  `issue:update` plus `wiki:read` and both the issue and the page visible;
  listing returns attached pages the reader cannot see as inaccessible IDs, and
  anyone who may edit the issue may remove them. Attaching and removing
  broadcast `issue.wiki_references_changed` (issue ID, identifier and page ID
  only), so the open issue and the page's backlinks reload.
- **Backlinks.** `GET <wiki>/:pageId/backlinks` lists issues (description, comment
  or attached) and pages that reference a page, each filtered by the caller's
  visibility. Mentions in content are indexed in `wiki_page_mentions` (mentioned
  page → issue, comment or page) by triggers on `issues.description`,
  `comments.body` and `wiki_pages.content` (migration 140, which also backfills),
  so every write path keeps the index current and a backlink query is an index
  lookup followed by the visibility check on the few matching rows. The side
  panel reloads at most once per burst of workspace events, never for edits of
  the page itself, and not while the tab is hidden. The route needs `wiki:read`;
  referencing issues are included only for a caller that also has `issues:read`.
- **Published pages.** Readers have no chips. A reference to a page published in
  the same reader is a link; a reference to any other page the reader may know by
  title is plain text; the rest is the placeholder. For public links the server
  resolves the mentioned pages under `domain.PublicContext` (so private teams'
  pages are never named) and returns them as `references`. The publish dialog
  states this when "Public" is chosen.
- **Chat.** The composer is plain text: `@` or a dropped page inserts
  `@[Title](wiki:<page id> team|project <name>)`, which sent messages show as a
  chip. The assistant reads the page with its wiki tools, as the user.

## Persistent assistant chat

The Global Assistant reuses workspace AI settings, PostgreSQL and the existing
HTTP/WebSocket stack. Conversations and incremental events survive navigation
and API restarts. Generation attempts are leased and idempotent, with explicit
retry after interruption. Issue details embed a creator-private chat with a fresh,
bounded issue snapshot and original-team visibility checks. Conversations are
never shared; private folders and visible projects only tag and filter the
creator's list, and issue chats follow their issue's project. Orchestrator tools let
the assistant list teams, search, read, create and update issues, read and list
agent work, watch a coding task so the user is pinged in chat when it reaches a
state they asked about, link an issue or project to a repository and request
approval for
delegation, an agent review of a published pull request, a follow-up on a task,
publishing a stopped task's unpublished changes as a draft, or a merge, start or
stop a task's preview and expose or list its machine's app
ports (start and expose need approval), with the user's own permissions; it
cannot change Dev Machine lifecycles. One reply runs up to 10 minutes, 400 tool calls and 10 issue
changes, with a bounded per-round context. Every merge Kuayle makes goes
through one [merge gate](BE/docs/agent-tasks.md#merging-only-up-to-date-heads):
a head that contains its base and whose checks passed. A branch behind its base
is brought up to date and the approved merge waits (`waiting_checks`, migration
171), polled every minute by `RunMergeWaits`, and merges the verified update once
its checks pass, or ends with the reason (KUA-207). Comments by workspace members on an
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
Workspace skills (`/assistant-skills`, migration 98) are versioned Markdown
guidance, workspace-wide or for one team, that members can read (private-team
skills only when they see the team) and owners and admins write, activate,
restore and delete. The assistant's system prompt lists active, enabled skills
by name and description; `load_skill` returns a body as workspace guidance and
`propose_skill` only saves drafts for an admin to activate; chat shows a
proposed skill's full text with a link to Settings → Assistant → Skills, where
admins edit with a Markdown preview, review a draft's diff, activate, restore
and delete, and members read. See [skills](BE/docs/conversations.md#skills).
Custom tools (`/assistant-tools`, migrations 99–100) are versioned scripts with
a strict scalar parameter schema that owners and admins write, activate and
test-run; the assistant is offered active, enabled ones as `custom_<name>` and
can only draft new ones with `propose_tool`. Each run asks for approval unless
an admin marked the tool read-only and `auto`, and executes as a `run_tool`
machine operation in the credential-free check container under the manual
check's machine lock, with arguments only as environment variables and a JSON
file. Settings → Assistant → Custom tools
(`?tab=custom-tools&tool=<id>`) lists, edits (script, parameter builder),
reviews, activates, restores, deletes and test-runs them with their run
history; chat shows `propose_tool` drafts and run approvals and results as
escaped text. See [custom tools](BE/docs/conversations.md#custom-tools) and
[custom tool runs](BE/docs/agent-tasks.md#custom-tool-runs).
The optional [public URL reader](BE/docs/conversations.md#public-url-reader) runs
through the Dev Machine egress proxy in `devmachine/egress`. The proxy builds
from the `devmachine` context and uses a generated copy of the forge address
check. After editing `BE/pkg/validate/public_ip.go`, run `go generate ./pkg/validate`
from `BE` to update the copy. Backend tests verify that both files stay in sync.
Command tools (KUA-153, migration 159) run one shell command after
approval: `run_command` read-only, `run_command_write` in the conversation's
folder `/workspace/kuayle-assistant/<conversation>`, read with
`get_command_run`. A command runs as a `run_command` machine operation (attempted
once) in a fresh credential-free check container under the manual check's
machine lock. The command is only in the environment and `/workspace` is
always read-only: a write run gets only its conversation folder read-write (a
volume subpath, created by a fixed-argv prep container; Docker API 1.45+, and
write runs fail on older daemons), so packages go there too (`pip install
--target ./pkgs`, a venv or `npm install`). The per-command approval binds the timeout (60 s by default,
1–600 s with `timeout_seconds`, migration 197). The transcript keeps the last
64 KiB of output. It runs on a running machine the user owns: the given
`machine_id`, the conversation's temporary machine, the user's own machine or
their live temporary machine from another conversation (reused). `run_migration`
(KUA-181, migration 181) runs a named migration action the same way, with the
action's admin-configured command and its connection secret injected as
`KUAYLE_MIGRATION_SECRET` and redacted from the stored output; a workspace
owner, admin or the machine's creator configures the actions in the machine
page's Database migrations card. `start_temporary_machine` asks for approval only when there is
none, then starts a temporary machine (1 vCPU, 2 GB, 20 GB — the global
`DevMachineMinDiskGB` floor — headless, no repository, agents, Docker or
sudo), re-checking the approver's role under the workspace machine lock. The manager permanently deletes it 30
minutes after creation. The workspace allows `max_temporary_machines` of them
(default 2, 0 turns them off) and one per user. The terminal capability matrix
and the details are in
[commands and temporary machines](BE/docs/conversations.md#commands-and-temporary-machines).
Ad-hoc servers use `start_process` with approval on a running machine the
user owns. They run for up to 30 minutes in an isolated container and use
the authenticated app-port gateway. `get_process`, `stop_process` and the
approved chat card expose their status and controls. See
[ad-hoc processes from chat](BE/docs/conversations.md#ad-hoc-processes-from-chat).
Commands never install system packages (KUA-180); those come from the
machine's Development Environment image, see
[system packages](BE/docs/conversations.md#system-packages).
Commands never commit, branch or push a task checkout either (KUA-183);
changes to a task's branch go through `follow_up_task` and publication, see
[commits and pushes from chat](BE/docs/conversations.md#commits-and-pushes-from-chat).
Release tools read a linked repository's releases (`list_releases`,
`get_release`), draft the next notes from the pull requests merged since the
latest release in the workspace's editable "Release notes style" skill
(`draft_release_notes`), publish a release after approval (`create_release`,
owners and admins, migration 111) and follow its build (`get_workflow_run`,
GitHub App **Actions: Read**; the app now asks for *Read and write*, for the
CI re-runs above). Tagging is refused unless the target is on the
default branch and all its checks passed; the release range (previous tag to
target, from GitHub's compare API) and the notes go through a server-side secret
scan first, and the approval card lets the approver edit the notes that are
published. See [release tools](BE/docs/conversations.md#release-tools).
`ship_pull_request` (owners and admins, migration 112) merges an issue's pull
request, waits for the merge commit's checks, publishes the release, waits for
its build and optionally upgrades the instance, behind one approval that lists
every step; each step re-checks its own tool's role, GitHub permissions and
switch, and the first failing step stops the chain with the next step. See
[shipping in one approval](BE/docs/conversations.md#shipping-in-one-approval).
Read-only research tools (KUA-93, migration 114): `web_search` asks the
workspace's search provider (Brave or Tavily; the key is set by owners and
admins in Settings → Assistant → Research, `/assistant-research-settings`,
stored encrypted and never returned; or, from migration 116, a self-hosted
SearXNG without a key, offered only when the operator sets
`WEB_SEARCH_SEARXNG_URL`, the one private host the search client may dial; the
self-hosting `searxng` Compose profile runs it, see
[self-hosted web search](docs/self-hosting.md#self-hosted-web-search-optional)); the `repo_*` tools list, glob, grep, read
and list symbols of a temporary checkout that the API server extracts from a
linked repository's tarball (read-only, repository-scoped installation token,
no git, `.gitignore`, link and traversal protection, size caps, 15-minute TTL,
removed on shutdown); `spawn_research_subagents` runs up to eight parallel
sub-tasks on a cheaper model limited to those tools, with a shared step and
token budget. See [research tools](BE/docs/conversations.md#research-tools).

### Images in conversations

Users attach PNG, JPEG, WebP or GIF images (sniffed, at most 10 MB each and 6
per message) by button, paste or drag-and-drop; Telegram photos are stored the
same way. Images live in the configured upload storage, are recorded by
migration 82 and are served only to the conversation's readers through an
authenticated, `nosniff`, script-sandboxed endpoint. With the AI setting
"Model can see images" (off by default, with a probe) the newest images are sent
as OpenAI-compatible `image_url` parts; otherwise the model gets a text note.
The `attach_images_to_issue` tool copies them into issue assets of the issue's
team and files them on the issue, by default as a comment embedding them or,
with `as: attachment`, appended to the issue's description as its own images.
Task attempts copy the issue's images into
`.kuayle/attachments/` in their worktree (excluded through the repository's
`info/exclude`) and list them in the prompt. The machine manager therefore
needs read access to upload storage: the Compose files mount the `uploads`
volume read-only and pass the storage settings. See
[conversation images](BE/docs/conversations.md#images) and
[task images](BE/docs/agent-tasks.md#issue-images).

### Rich chat blocks

Completed assistant replies in the web chat may contain two extra fences. A
`chart` fence holds only a JSON series spec (bar, line, area or pie; one to five
series, at most 100 categories and, for a pie, at most 12 so slice colours do
not repeat), which `ChatChart` draws on an echarts canvas
with theme colours from `--chart-1..5` and a rich-text tooltip, so no model
string becomes HTML. Only web replies are told about the fences: the guidance
message is added per channel, and Telegram, which would show them as raw code,
never receives it. An `html` fence renders as `ChatHtmlWidget`: an iframe with
`sandbox="allow-scripts"` only (never `allow-same-origin`) and a fixed 360px
height. Its `srcdoc` document starts with a CSP of `default-src 'none'` that
allows only inline scripts and styles and data images and fonts, and adds
`connect-src`, `frame-src`, `child-src`, `worker-src`, `form-action` and
`base-uri` of `'none'`, `webrtc 'block'` and DNS prefetch off. A prelude script
before model markup overrides `RTCPeerConnection`, `webkitRTCPeerConnection` and
`RTCDataChannel`. That only raises the bar: in browsers without the `webrtc` CSP
directive, WebRTC is reduced rather than proven blocked, a residual risk.

The page itself carries `frame-src 'none'` in `UI/src/app.html`, so a widget
that tries `location=`, a meta refresh or a link cannot navigate its frame and
no request leaves. `srcdoc` is never fetched and still loads under `'none'`. A
second frame load (the widget navigating itself) still tears the frame down as a
backstop. A future parent CSP must keep allowing inline scripts in `srcdoc`
frames, because they inherit the parent policy. Verified on Chromium, WebKit and
Firefox: no request leaves in any of them, but Firefox fires no load event for a
CSP-blocked navigation, so there the blocked frame simply stays instead of being
replaced by the code fallback (the front-end skill documents the page-wide
`frame-src 'none'` constraint for embed authors).

Caps apply per fence (16 KB chart, 64 KB html) and per message (four rich blocks
and 128 KB total); a malformed, oversized or otherwise refused block stays an
ordinary escaped code fence, and a rendering failure shows the original fence
with a short note, never a blank bubble. Rich blocks render only outside
streaming. The per-message copy path and `stripRichBlocks` use the same
parse-and-cap decision, so a fence refused by parsing or caps is copied as code;
a fence that already rendered and then failed at runtime is still left out
(known). Each Markdown segment renders on its own, so reference-style links and
footnote definitions across a rich fence do not resolve (known).
`BE/internal/service/conversation.go` resolves each reply's channel from its own
queued delivery and appends `richBlocksPrompt` to the model request for
web-channel replies only, so Telegram never learns the fences.

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
