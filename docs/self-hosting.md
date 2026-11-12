# Self-hosting Kuayle

Kuayle is designed to be self-hosted. The reference stack in [`selfhosting/`](../selfhosting/) includes Caddy, PostgreSQL, Redis, the backend, the frontend, and an update script. Review secrets, backups, monitoring, and host security before production use.

Related guides: [GitHub integration](github-integration.md), [Dev Machines](dev-machines.md), [development setup](development.md).

## Prerequisites

- A Linux server with Docker + Docker Compose v2
- A domain pointing to your server (for HTTPS via Let's Encrypt HTTP-01)
- If enabling Dev Machines: wildcard DNS and public TLS. A separate registrable domain is the default; the explicit personal-instance alternative and DNS-provider-independent HTTPS setup are documented below.

## Install

### 1. Clone and configure

```sh
git clone https://github.com/carbogninalberto/kuayle.git
cd kuayle/selfhosting
cp .env.example .env
```

Edit `.env` with your domain and production values:

```env
DOMAIN=kuayle.yourcompany.com
FRONTEND_URL=https://kuayle.yourcompany.com
POSTGRES_PASSWORD=<strong-random-password>
JWT_SECRET=<random-string-at-least-32-chars>
```

#### Secrets must be generated

Every `CHANGE_ME_*` value in `.env.example` is a public placeholder. Generate each secret yourself, with a different value per setting:

```sh
openssl rand -hex 32
```

This applies to `POSTGRES_PASSWORD` and `JWT_SECRET`, and to `SYSTEM_UPDATER_TOKEN`, `VOICE_INTERNAL_TOKEN`, `DEV_MACHINE_ENCRYPTION_KEY` and `DEV_MACHINE_GATEWAY_DB_PASSWORD` when you enable those features.

Unless `ENVIRONMENT=development`, Kuayle refuses to start while one of these settings (or `SMTP_PASSWORD`, `S3_SECRET_KEY`, `GITHUB_APP_CLIENT_SECRET`, `GITHUB_APP_WEBHOOK_SECRET`) still holds an example placeholder, a development default from the repository's root `.env.example`, or a `<...>` value copied from this guide. The backend and the machine gateway exit with an error that names the setting (never its value), the `dev-machines` profile preflight fails, and `update.sh` stops before rebuilding or stopping anything. Check `docker compose logs backend` if the site does not come up.

The same applies when `POSTGRES_PASSWORD` is not set at all: the Compose files then fall back to a password that is published in this repository, and Kuayle and `update.sh` refuse it the same way. If your `.env` has no `POSTGRES_PASSWORD` line, your database uses that fallback today; change it as described below before upgrading.

To replace a placeholder on an instance that is already running:

- `JWT_SECRET`, `SYSTEM_UPDATER_TOKEN`, `VOICE_INTERNAL_TOKEN`: set the new value in `.env` and recreate the services that use it (`docker compose up -d`, adding `--profile updater` or `--profile voice` when those are in use). Changing `JWT_SECRET` signs everyone out.
- `POSTGRES_PASSWORD`: PostgreSQL keeps the password it was initialised with, so change it in the database first, then set the same value in `.env` and recreate the services. Use a URL-safe value such as the `openssl` output above:

  ```sh
  docker compose exec postgres psql -U kuayle -d kuayle -c '\password kuayle'
  # then edit POSTGRES_PASSWORD in .env
  docker compose up -d
  ```

  Replace `kuayle` with your `POSTGRES_USER`/`POSTGRES_DB` if you changed them. On a fresh install with no data yet, `docker compose down -v` and starting again also works; it deletes the database volume.
- `DEV_MACHINE_GATEWAY_DB_PASSWORD`: set the new value in `.env` and run `docker compose --profile dev-machines up -d`; the provisioning step updates the gateway role.
- `DEV_MACHINE_ENCRYPTION_KEY`: stored machine secrets are encrypted with it, so they cannot be read after it changes. Replace it before storing secrets, or re-enter them afterwards.

`FRONTEND_URL` must be the exact public browser origin, including scheme and any non-default port. Dev Machine native terminal WebSockets use this value for strict `Origin` validation.

### 2. Launch

```sh
docker compose up --build -d
```

This starts 5 containers: Caddy (auto TLS), PostgreSQL, Redis, backend API, and frontend.

### 3. Run migrations and seed

```sh
docker compose exec backend /app/server migrate up
docker compose exec backend /app/server seed
```

Caddy handles HTTPS termination and proxies `/api/*` to the backend and all other requests to the frontend, so one public origin is enough.

## Updating

```sh
cd kuayle
bash selfhosting/update.sh
```

The update script pulls the latest code, refuses to continue while a secret in `.env` is still an example placeholder (see [Secrets must be generated](#secrets-must-be-generated)), rebuilds images, applies pending migrations, and recreates containers. If Dev Machines services are running, it rebuilds their control-plane and runtime images first, stops the existing gateway and manager only while migrations and restricted gateway-role grants are applied, then starts the updated control plane. A failure after the stop clears the upgrade marker, preserves logs, restarts the same previous containers, and returns the original nonzero status. Disabled optional profiles remain disabled.

Instance sysadmins can also enable one-click updates from **Settings → Version**:

1. Copy your user ID from **Settings → Profile**.
2. Add it to `SYSADMINS` in `selfhosting/.env`.
3. Set a strong `SYSTEM_UPDATER_TOKEN` and keep `SYSTEM_UPDATER_URL=http://updater:8081`.
4. Apply the env and start the internal updater sidecar with `docker compose --profile updater up -d backend updater` from `selfhosting/`.

The updater sidecar is internal-only and runs the same `selfhosting/update.sh` flow. The Version page shows whether the updater is connected and the result of its last run; a failed run shows its reason and expandable recent output. When an update starts, every connected browser is redirected to the upgrade page, which returns to the previous page once the update ends. Update scripts do not refresh the running sidecar: rebuild and recreate `updater` to get failure details from an older sidecar. Without a configured updater, use the command above.

For a deployment pinned to published release images or a detached Git checkout, use the opt-in [release updater](../selfhosting/RELEASE_UPDATER.md) instead. It targets an explicit production Compose project, backs up PostgreSQL and updates application and active Dev Machines components together.

## Storage options

By default, uploads use a local Docker volume (`STORAGE_TYPE=local`). The alternative S3 backend accepts a configurable endpoint and path-style requests:

```env
STORAGE_TYPE=s3
S3_ENDPOINT=https://s3.us-east-1.amazonaws.com
S3_BUCKET=kuayle-uploads
S3_REGION=us-east-1
S3_ACCESS_KEY=your-key
S3_SECRET_KEY=your-secret
```

The backend is intended for AWS S3 and compatible APIs such as R2, MinIO, and SeaweedFS. Test bucket creation, permissions, public URL behavior, and signed downloads with your chosen provider.

## Enabling Dev Machines

Dev Machines remain disabled in the default five-service deployment. Before enabling them:

1. Set `DEV_MACHINES_ENABLED=true`, then configure `DEV_MACHINE_DOMAIN` on a separate registrable domain from `DOMAIN` (or explicitly opt into the same-site setup below), with wildcard DNS pointing to the host.
2. Replace the checked-in local `tls internal` wildcard configuration with either a mounted wildcard certificate or a DNS-01-enabled Caddy build for production. Profile startup rejects public production machine domains while Caddy internal TLS remains active. The wildcard route already proxies machine HTTP and WebSocket upgrades to the gateway.
3. Keep `FRONTEND_URL` set to the exact public Kuayle origin; the gateway uses it for native terminal WebSocket `Origin` checks.
4. Set `DEV_MACHINE_ENCRYPTION_KEY` to an independent random value of at least 32 characters and set `DEV_MACHINE_INGEST_URL` to the public HTTPS API URL.
5. Set an independent `DEV_MACHINE_GATEWAY_DB_PASSWORD`; Compose provisions a restricted `kuayle_gateway` login and the gateway rejects the application database credential in production.
6. On Linux, set `DEV_MACHINE_DOCKER_GID` to the numeric group owner reported by `stat -c '%g' /var/run/docker.sock`; Docker Desktop commonly uses the default `0`.
7. Build the runtime images, migrate, provision gateway grants, and start the optional control plane:

```sh
docker compose --profile dev-machine-images build
docker compose exec backend /app/server migrate up
docker compose --profile dev-machines run --rm machine-gateway-db-provision
docker compose --profile dev-machines up -d
```

The API rejects a production machine domain that shares the main application's registrable domain. `machines.localhost` is supported only for local development. The gateway rejects parent-domain and reserved session cookies from workloads. Browser-cookie launches for code-server and browser require exact machine origins for mutations and service WebSockets; native terminal WebSockets use a separate one-use ticket bound to exact `FRONTEND_URL`, host, user/service, tmux session, and working directory.

For DNS-01, replace the stock `caddy:2-alpine` image with a trusted custom Caddy build containing your provider module, replace `tls internal` with a `tls` block using that DNS provider, and pass its scoped API credential through a Compose override. For operator certificates, bind-mount a host directory read-only at `/etc/caddy/certs` through a Compose override and use `tls /etc/caddy/certs/machines.pem /etc/caddy/certs/machines.key` in the wildcard block. Keep private keys outside the repository and container image.

Environment snapshot bytes are local OCI images stored in the host Docker image store, while their image references and lifecycle metadata are PostgreSQL rows. Snapshots exclude the repository workspace named volume and tmpfs secret mounts; plan backup, migration, pruning, and host disk monitoring accordingly.

The manager process runs as UID/GID 1000 with only the Docker socket's host group added. Access to that socket remains host-root-equivalent despite the non-root container UID, so never expose it to machine workloads or general application services.

## Personal instances: same-site machines and HTTPS without DNS API credentials

Separate registrable domains remain the default and recommended isolation boundary for
untrusted workloads. For a personal/trusted installation, explicitly set:

```env
FRONTEND_URL=https://app.example.com
DEV_MACHINE_DOMAIN=app.example.com
DEV_MACHINE_ALLOW_SAME_SITE=true
```

Add `A *.app` pointing to the server at whichever DNS provider hosts `example.com`.
The option requires an exact HTTPS frontend origin (no path). Enabling it switches
app authentication to Secure, HttpOnly, host-prefixed cookies and rejects browser
mutations/WebSockets from other origins, including sibling workloads. Sign in again
after switching this option; legacy session cookies are deliberately not accepted.
Non-browser bearer clients and webhooks without browser cookies remain supported.
A startup warning records the reduced browser site isolation. This option does not
make untrusted workloads equivalent to a separate site.

You can use individual on-demand certificates instead of a wildcard certificate,
with stock Caddy and no DNS-provider credentials. Ports 80/443 must reach Caddy.
In the global Caddy options, configure:

```caddy
{
    on_demand_tls {
        ask http://machine-gateway:8091/
    }
}
```

Replace the checked-in machine wildcard block with the following (retain the main
application's explicit site block):

```caddy
https:// {
    tls {
        on_demand
    }
    @machine host *.app.example.com
    handle @machine {
        reverse_proxy machine-gateway:8090 {
            flush_interval -1
        }
    }
    respond 404
}
```

The catch-all obtains individual certificates, not a wildcard certificate. The
private approval listener on port 8091 allows only exact, existing running machine
service hostnames, rejects unknown or stopped services, and fails closed when
the database is unavailable. Keep it on the private control network: do not publish
8091 or proxy it publicly. With a shared edge proxy, attach it to the control network
and adjust the gateway hostname. Certificate issuance can delay the first connection
and remains subject to CA rate limits; frequent machine churn favors DNS-01 wildcard
certificates. Existing certificates may be retained after teardown, but the gateway
still rejects access to stopped/deleted machines. Approval is not user authorization;
every machine request still uses the existing tickets, sessions and access checks.

## Voice transcription (optional)

Members can dictate assistant messages and send Telegram voice notes; speech is transcribed on your server by the internal `voice` service (faster-whisper int8 or Vosk, CPU only, no GPU). Recordings are limited to 5 minutes, held only while being transcribed and never stored; the transcript fills the composer for review (Telegram notes become the user's message, marked with 🎤).

1. Set `VOICE_INTERNAL_TOKEN` in `selfhosting/.env` to a random value of at least 32 characters (`openssl rand -hex 32`). Keep `VOICE_URL=http://voice:8000` (the default).
2. Start it with `docker compose --profile voice up -d voice backend` from `selfhosting/`.
3. As the workspace owner open **Settings → Voice** and pick a model. It is downloaded from its official source (Hugging Face for faster-whisper, alphacephei.com for Vosk), verified against pinned checksums and then activated.

The service is not published: only the backend reaches it on the internal `voice-control` network, and it needs outbound HTTPS only to download models into the `voice_models` volume (from 41 MB for Vosk small to about 1.6 GB for large-v3-turbo; unused models stay there until you remove the volume). `selfhosting/update.sh` does not rebuild optional profiles: after updating, run `docker compose --profile voice up -d --build voice`. See [voice transcription](../TECHNICAL.md#voice-transcription) for limits and security.

## Self-hosted web search (optional)

The assistant's `web_search` tool can use a SearXNG instance on your server instead of a Brave or Tavily API key.

1. Run `bash selfhosting/searxng.sh enable`. It generates `SEARXNG_SECRET` in `selfhosting/.env` (never printed or committed), sets `WEB_SEARCH_SEARXNG_URL=http://searxng:8080`, starts the `searxng` Compose profile and recreates the backend.
2. A workspace owner or admin chooses **SearXNG (self-hosted)** in **Settings → Assistant → Research**. No key is needed.

The service has no published port: only the backend reaches it on the internal `search-control` network, and it queries DuckDuckGo, Brave, Wikipedia and Bing through `search-egress`. Its configuration is [selfhosting/searxng/settings.yml](../selfhosting/searxng/settings.yml) (JSON output on, limiter off because nothing else can reach it, moderate safe search). `selfhosting/update.sh` keeps the secret and refreshes the pinned image when SearXNG is running; `bash selfhosting/searxng.sh disable` turns it off. To use an existing instance, set `WEB_SEARCH_SEARXNG_URL` to its base URL instead; it must list `json` in `search.formats`. With the [release updater](../selfhosting/RELEASE_UPDATER.md), add the `searxng` service and the backend's `WEB_SEARCH_SEARXNG_URL` to your production configuration. See [web search](../BE/docs/conversations.md#research-tools) for the security model.

## GitHub integration

See [GitHub integration](github-integration.md) for the GitHub App, webhooks on private networks and local development.

## Passkeys

Users can add passkeys under Settings → Profile and then sign in with a fingerprint,
face, screen lock or security key instead of their password. Nothing needs configuring
when the instance is served over HTTPS at `FRONTEND_URL`: that origin is the WebAuthn
relying party.

- Passkeys require HTTPS (or `http://localhost` for development). On a plain-http
  address they are switched off, the server logs why at start-up, and password sign-in
  works as before.
- If people reach the instance at more than one origin, list them in
  `PASSKEY_RP_ORIGINS` and set `PASSKEY_RP_ID` to their common parent domain.
  Changing the relying party ID later invalidates the passkeys already registered.
- `PASSKEYS_ENABLED=false` turns them off. They are always off in public demo mode.
- A password reset removes the account's passkeys along with its sessions and tokens.

Endpoints, the full configuration rules, reverse-proxy behaviour and the threat model
are in the [passkey reference](../BE/docs/passkeys.md). The migration adds two tables and
two nullable columns on `refresh_tokens`; rolling it back deletes the passkeys and
ends the sessions that were signed in with one.

## Private teams

Existing teams remain public. Workspace owners and admins can create private teams,
manage their membership, and change visibility in team settings. Owners and admins
always have access to private content; ordinary members and guests need explicit
team membership. Workspace roles and personal access token scopes still limit actions.
Removing membership affects subsequent requests, including existing tokens.

Issues, projects, cycles, statuses, comments, attachments and wiki pages inherit their
team's visibility; a project wiki follows its project. Teamless projects remain public within the workspace. Relationships
involving a private team must stay within that team; private resource moves are
unsupported. Resolve incompatible relationships before making an existing team
private. Making a team public requires confirmation and can reactivate old public links.

This first version has workspace-wide restrictions after private teams are enabled:
saved views, filtered/view-based public links, workspace export, AI expansion, outgoing
webhooks, development machines and live presence are disabled. These restrictions
remain after teams become public or are deleted. Unfiltered shares of public resources
remain available. Remove existing development machines, environments and development
defaults before enabling privacy. GitHub installation-to-workspace mapping is unchanged;
private issue activity and automation references are removed locally. Already published
or downloaded copies cannot be recalled.

Migrations 39–46 default existing teams to public and add authorization-boundary guards.
Legacy attachments with exactly one referencing team are assigned to that team.
Ambiguous or unattributed attachments become unavailable in privacy-enabled workspaces;
references that cross a private boundary block conversion. Review these attachments
before enabling privacy. Imports reject archives containing private teams.

Migration 46 indexes attachment references from descriptions, comments and history;
it validates affected rows during normal edits and audits the graph during visibility
changes. Apply it with application writers stopped: it backfills existing content and
changes both authorization functions and publication coordination. Start only the new
backend afterward; mixed old/new backends are not supported.

Ordinary database statements share a graph barrier acquired before row locks.
Classification, team deletion/creation and late asset insertion take its exclusive side.
This barrier is global to handle statements spanning workspaces; those uncommon
operations and imports can briefly delay other writes. AI, webhook and export requests
hold a separate per-workspace publication lock, allowing ordinary edits while visibility
changes wait. Direct SQL transactions that attempt a conflicting lock upgrade or a
visibility change during publication fail closed with SQLSTATE 40001 and must retry
from a fresh transaction. The API orders visibility/import locks before its first write.
Access functions recheck current membership after lock waits under READ COMMITTED
isolation. The backend pins every database session to READ COMMITTED, even when the
database or role default is stricter; imports request SERIALIZABLE explicitly. At most
eight AI/webhook publications run at once, so slow providers cannot exhaust the pool. New migrations adding
application tables must install the statement guard before introducing new write paths.

For a real browser regression, start an isolated migrated backend on port 8080 and
`npm --prefix UI run dev -- --host 127.0.0.1 --port 4176`, then run
`node UI/tests/live/private-teams.mjs`. Override the UI URL with `KUAYLE_LIVE_URL`.
The test creates temporary accounts/workspace, exercises live WebSockets, removes its
workspace, and writes evidence to `tmp/reports/`. Use a disposable database for test accounts.

Back up PostgreSQL and asset storage before upgrading. `selfhosting/update.sh` stops
the backend before `migrate up` and starts only the new image afterwards. If a migration
fails, it leaves the backend stopped behind the upgrade page instead of restarting a
previous version against a partially migrated schema. Deploy the new backend and
migrations together, and do not run an older backend against data that has enabled
privacy. Down migrations refuse while private teams exist; attachment rollback also
refuses quarantined legacy assets. A downgrade requires deliberate declassification
and removal of quarantined assets, and may restore features disabled by this version.
It is an administrative disclosure decision, not a routine rollback. A refused
`server migrate down` rolls back its SQL but leaves `schema_migrations` dirty at the
target version; confirm the schema is unchanged and run `server migrate force <previous
version>` before continuing.

## Portable workspace transfer

Workspace owners and admins can download a versioned `.kuayle.zip` archive from **Settings → General → Workspace transfer**. An authenticated user with no workspace can choose **Import workspace** during workspace setup; an owner or admin can also start an import from the transfer settings. Import always creates a separate workspace and never overwrites an existing one.

The archive contains a logical `manifest.json`, `data.json`, and the workspace's uploaded asset bytes. It preserves teams, statuses, labels, projects, cycles, issues and their relationships/history, templates, views, favorites, shared links, workspace notifications, integration metadata, AI prompts, and safe Dev Machine workspace policy/scope settings. Database and asset IDs are regenerated and references—including protected asset URLs—are remapped during import.

Team and project wiki pages are not included in the archive yet; copy them separately before migrating.

Workspace transfer deliberately excludes password hashes, refresh tokens, personal preferences, webhook secrets, GitHub App credentials and access tokens, AI API keys, Dev Machine credentials, active machines, Docker volumes/networks, environment images, logs, sessions, and runtime artifacts. Imported webhooks, shared links, GitHub repositories/automation, and Dev Machine policy are disabled or rotated where appropriate; GitHub, AI, webhooks, and Development Environments must be reconfigured on the target instance.

Users are matched to existing target-instance accounts by normalized email. The importing user becomes the new owner; all other referenced users must register on the target instance before import. The preview reports any missing emails without creating partial data.

This feature transfers one logical workspace. It is not a replacement for an operator backup: disaster recovery and whole-instance migration still require PostgreSQL backups, object-storage backups, deployment configuration/secrets, and any required local Docker volumes or Development Environment OCI images.

## Issue version history (migration 47)

Open **Version history** in an issue to browse recorded title and description
changes in chronological order. Each change shows its author when known, its
time, and the change as a git-style diff or as before/after panes. Descriptions support both rendered
content and HTML source. Existing stored changes remain available; versions
that were never recorded cannot be reconstructed.

Migration 47 captures content changes atomically with the issue update, including
integration writes. It does not backfill history. Deploy the migration with the
new backend to avoid duplicate entries from older service-level history writes.
API clients should accept `null` for `user_id` in history responses when no editor
can be identified.
Rolling back migration 47 removes automatic capture but preserves historical
records, including nullable authors for writes without an identified editor.

## Release candidate validation

The **Release Readiness** workflow builds the all-in-one, API and web images on
native runners without publishing. Pull requests default to amd64 and add native
arm64 jobs when the diff touches a platform-sensitive input. A manual dispatch
defaults to amd64; set `include_arm64=true` to verify both architectures. The
workflow checks the PR head commit (or the selected workflow-dispatch revision),
runs the packaged runtime smoke test, and fails on HIGH or CRITICAL image scan
findings. Each job uploads the source commit, build digest/metadata, local image
identity, runtime log, Trivy JSON report and an archive of the exact image tested.
Require all six image jobs (three on an amd64-only run) and the **Tests** workflow
to pass for the same reviewed commit before declaring a candidate ready.
The existing **Security Scan** and **Build & Push** PR checks also remain
required, including the native Dev Machine image builds and scans. PR builds
do not publish images.

The smoke check can also run against a locally built image:

```sh
python3 selfhosting/image_smoke_test.py kuayle:candidate all-in-one
```

Use `api` or `web` for the other image types. Docker and Python 3 are required.
The check creates and removes disposable containers and a network. API and
all-in-one checks apply the packaged migrations to PostgreSQL 17 and exercise
the real API; the web check uses a fixture upstream to verify proxy routing.
Both Caddy images verify SPA deep links and static assets as well as backend
routes. Container UI builds default to an 8 GB Node heap, the same as host
builds (the client build no longer fits in 6 GB); use
`--build-arg NODE_OPTIONS=--max-old-space-size=<MB>` to override it.
A successful candidate run does not authorize publishing. Automatic releases
publish amd64 images only. Before deploying an automatic release on an ARM64
host, rebuild that release tag with both architectures:

```sh
gh workflow run build.yml --ref main \
  -f release_tag=v0.2.32 \
  -f include_arm64=true
```

The manual run builds the release's version-bump commit on `main` without
creating another commit, and updates `:latest` only when the tag is the current
latest release. See the
[CI routing guide](../scripts/ci/README.md#manual-release-rebuilds-and-arm64-images)
for the tag and republishing risks. A successful candidate run does not authorize
publishing. The existing automatic release-publishing workflow updates source
files on `main`; its output must be revalidated if its revision differs from the
reviewed candidate.
