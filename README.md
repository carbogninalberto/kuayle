<div align="center">
  <img src="assets/logo_primary.svg" alt="Kuayle" width="250">
  <p><strong>快乐 (kuàilè) · happiness, joy</strong></p>
  <p>A fast, keyboard-driven issue tracker inspired by Linear. <strong>v0.1.0</strong></p>

[Report Bug](https://github.com/carbogninalberto/kuayle/issues/new?labels=bug) · [Request Feature](https://github.com/carbogninalberto/kuayle/issues/new?labels=enhancement)

</div>

<br />

[![Kuayle Screenshot](assets/product-screenshot.png)](https://github.com/carbogninalberto/kuayle)

## 🚦 Current Implementation State

Kuayle is currently a runnable MVP of the core issue tracker. The repository includes a Go API, PostgreSQL migrations, Redis configuration, and a SvelteKit frontend that can be run locally with Docker Compose or the Makefile commands below.

| Area             | State                                                                                                                                                                                                                                                     |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Core tracker** | Implemented end-to-end: auth, workspaces, members/RBAC, teams, custom statuses, issues, labels, comments, history, sub-issues, issue relations, triage, templates, favorites, saved views, notifications, public sharing, uploads, and WebSocket updates. |
| **Planning**     | Implemented: cycles with burndown/velocity charts, project management with Gantt view, and full cycle/project UI.                                                                                                                                         |
| **Integrations** | Implemented: workspace webhooks, GitHub App setup with repo linking, PR/branch/commit activity, auto-transitions, WebSocket real-time dispatch, and smee.io/cloudflared/ngrok support for private networks.                                              |
| **Analytics**    | Backend endpoints exist for overview and issue distribution; a dedicated frontend analytics page is not wired yet.                                                                                                                                        |
| **Dev Machines** | Specification/design only in this repo today. The runtime container manager and UI flow are not wired into the app yet; see [`TECHNICAL.md`](TECHNICAL.md).                                                                                               |
| **Self-hosting** | Production-grade Docker Compose with Caddy reverse proxy, Let's Encrypt TLS, update script, and dedicated config in [`selfhosting/`](selfhosting/).                                                                                                       |

## 🆚 How it compares to similar products

_Note: this is a lightweight snapshot, not a guarantee of current feature parity. Products evolve quickly, so some details may be outdated or incomplete._

**Where Kuayle is different:**

| Kuayle edge                      | Why it matters                                                |
| -------------------------------- | ------------------------------------------------------------- |
| ✅ **No paid feature gates**     | Every implemented feature is intended to stay free.           |
| ✅ **Apache 2.0**                | Permissive for internal forks, embedding, and commercial use. |
| ✅ **Small and hackable**        | Go API, raw SQL, SvelteKit UI, Docker Compose.                |
| ✅ **Multi-assignee by default** | Linear-like workflow without single-owner assumptions.        |
| 🟠 **Dev Machines direction**    | Spec points toward issue-to-coding-environment workflows.     |

| Area                     | Kuayle                                     | Linear                                  | Plane                                  |
| ------------------------ | ------------------------------------------ | --------------------------------------- | -------------------------------------- |
| **License**              | ✅ Apache 2.0                              | ❌ Proprietary                          | ✅ Open source                         |
| **Self-hosting**         | ✅ First-class Docker setup                | ❌ Hosted SaaS                          | ✅ Cloud and self-hosted               |
| **Paid feature gates**   | ✅ None intended                           | 🟠 Tiered                               | 🟠 Tiered                              |
| **Core issues**          | ✅ Fully implemented                       | ✅ Mature                               | ✅ Mature                              |
| **Multi-assignee**       | ✅ Built in                                | ❌ Not a core Linear feature            | 🟠 Work-item ownership varies by model |
| **Sub-issues**           | ✅ Tree view + counter                     | ✅ Mature                               | ✅ Mature                              |
| **Teams and workflows**  | ✅ Teams + custom statuses                 | ✅ Mature                               | ✅ Mature                              |
| **Projects**             | ✅ Projects + Gantt view                   | ✅ Projects + roadmaps                  | ✅ Projects + layouts                  |
| **Cycles**               | ✅ Cycles + burndown/velocity charts       | ✅ Mature                               | ✅ Mature                              |
| **Initiatives/modules**  | ❌ Not yet                                 | ✅ Initiatives                          | ✅ Initiatives + modules               |
| **Views/public sharing** | ✅ Saved views + view scoping + public links | ✅ Views                              | ✅ Views + publish options             |
| **Analytics UI**         | 🟠 Backend endpoints only                  | 🟠 Tiered insights/dashboards           | 🟠 Tiered dashboards/analytics         |
| **GitHub automation**    | ✅ GitHub App + auto-transitions + WebSocket | ✅ Mature                            | ✅ GitHub integration                  |
| **Import/export**        | ❌ Not yet                                 | ✅ Available                            | ✅ Multiple importers/export           |
| **Enterprise auth**      | ❌ No SSO/SCIM/LDAP yet                    | 🟠 Enterprise tier                      | 🟠 Paid/self-hosted tiers              |
| **AI/agents**            | 🟠 Dev Machines spec only                  | 🟠 Tiered Linear Agent/features         | 🟠 Tiered Plane AI/MCP                 |
| **Self-hosting**         | ✅ Production Docker Compose + Caddy+TLS   | ❌ Hosted SaaS                          | ✅ Cloud and self-hosted               |
| **Best fit**             | ✅ Hackable, no-gates Linear-style tracker | ✅ Polished hosted engineering workflow | ✅ Broader PM suite with wiki/modules  |

## 🧐 Why Kuayle?

I've been a happy Linear user for years, and it has set a very high bar for issue tracking. A few things kept bugging me: no multi-assignee on issues, no project-based Gantt, analytics behind a paywall, and per-seat pricing that adds up quickly for small teams.

With AI, building your own tool became realistic, so I did. First for myself, then for anyone who wants a similar workflow without the same cost structure.

I also looked at the open-source alternatives available at the time, and Kuayle took a different approach: **every feature is free, forever.** No paid tiers, no feature gates, Apache 2.0. If you find it useful, consider sponsoring the project, that's the whole model.

## ✨ Features

|     | Feature                | Description                                                                      |
| --- | ---------------------- | -------------------------------------------------------------------------------- |
| 🏢  | **Workspaces**         | Multi-tenant with role-based access (owner, admin, member, guest)                |
| 👥  | **Teams**              | Custom workflows, each team gets its own statuses and triage settings            |
| 📋  | **Issues**             | Priority, due dates, sub-issues, multi-assignee, labels, comments, audit history |
| 🔗  | **Issue Relations**    | Blocking/blocked, duplicate, and related issue links                             |
| 🔄  | **Cycles**             | Sprint planning with burndown/velocity charts and time-boxed iterations          |
| 📁  | **Projects**           | Cross-team work grouped under a single umbrella with Gantt view                  |
| 🏷️  | **Labels**             | Hierarchical, workspace-scoped, with soft delete and default labels on creation  |
| 👁️  | **Views**              | Saved views with personal/workspace/team scoping, drag-and-drop reorder          |
| 🔔  | **Notifications**      | Inbox with snooze, read status, and archive                                      |
| 🔗  | **Webhooks**           | Plug into external services and integrations                                     |
| ⚡  | **Real-time**          | WebSocket-powered live updates across all connected clients                      |
| 🖥️  | **Dev Machines**       | Technical specification for on-demand, single-container development environments |
| 🐙  | **GitHub**             | Link repos, auto-sync issues, webhook-based updates, real-time WebSocket events  |
| 📊  | **Analytics**          | Backend overview and issue distribution endpoints                                |
| 🔗  | **Public Sharing**     | Token-based read-only links for issues and views                                 |
| 📦  | **Asset Management**   | File uploads, signed URLs for prompt images, S3-compatible storage               |
| ⌨️  | **Command Palette**    | Global search with highlighting, keyboard shortcuts, and quick actions           |
| 🎨  | **Rich Text Editor**   | Tiptap-based with code blocks, slash commands, mentions, task lists              |
| 🚀  | **Release Changelog**  | Multi-release changelog modal with markdown rendering from static manifest       |

## 🤖 Dev Machines (Agentic Coding)

The Dev Machines track is currently a **technical specification** for on-demand, single-container development environments on a VPS. The intended design is a disposable workspace with an IDE, agentic coding CLI, and a browser, accessible via auto-generated subdomains.

> See [`TECHNICAL.md`](TECHNICAL.md) for the full specification, architecture diagrams, and API reference.

### Target design

```
You (browser)
  │
  ├─ f8k2m9.kuayle.com         → code-server (IDE + Claude Code)
  ├─ f8k2m9-browser.kuayle.com → Chromium (in-browser web navigation)
  └─ f8k2m9-app.kuayle.com     → Dev server preview
        │
        └── All routed through Kuayle auth — no port management needed
```

1. **Kuayle spawns a container** with code-server + Claude Code CLI + Chromium + your repo
2. **Assigns random subdomains** via wildcard DNS (`*.kuayle.com`), with no per-container port management
3. **Authenticates** users and agents through Kuayle's session layer
4. **Tracks all activity** (edits, commands, git ops, navigation) and feeds it back into project management

### Target modes

| Mode              | Who                             | What happens                                                              |
| ----------------- | ------------------------------- | ------------------------------------------------------------------------- |
| **Agent-only**    | Kuayle assigns a task           | Agent works autonomously, pushes results, machine tears down              |
| **Human + Agent** | Developer clicks "Open Machine" | Gets a browser link to a full IDE with agentic tools, works interactively |

### Target configuration

Machines would be configured from Kuayle's UI or via API: repo, branch, env vars, tools, and size. Configuration would resolve in order: project defaults → user preferences → spawn-time overrides.

| Size   | CPU     | Memory | Disk   |
| ------ | ------- | ------ | ------ |
| Small  | 2 cores | 4 GB   | 20 GB  |
| Medium | 4 cores | 8 GB   | 50 GB  |
| Large  | 8 cores | 16 GB  | 100 GB |

## 🛠️ Tech Stack

Here's what Kuayle runs on and what each piece does:

| Layer             | Tech                                 | Role                                                                  |
| ----------------- | ------------------------------------ | --------------------------------------------------------------------- |
| **API**           | Go + Echo                            | High-performance HTTP server with middleware, routing, JWT auth       |
| **Database**      | PostgreSQL 17                        | Primary data store, raw SQL via sqlx/pgx, no ORM                      |
| **Cache & Jobs**  | Redis 7                              | Configured in Docker/env for future cache and job usage               |
| **Frontend**      | SvelteKit + Svelte 5                 | SPA with TypeScript, runes-based reactivity, static adapter           |
| **UI**            | Tailwind CSS + shadcn-svelte         | Utility-first styling with accessible component primitives            |
| **Editor**        | Tiptap v3 + Yjs                      | Rich text with code blocks, mentions, slash commands, task lists      |
| **Real-time**     | WebSocket (nhooyr.io)                | Live collaboration, presence, issue updates                           |
| **Storage**       | Local FS or S3-compatible            | AWS S3, Cloudflare R2, MinIO, SeaweedFS                               |
| **Reverse Proxy** | Caddy                                | Production HTTPS and routing                                          |
| **Dev Machines**  | code-server + Claude Code + Chromium | Technical specification for single-container agentic dev environments |
| **Infra**         | Docker + Docker Compose              | One-command local and production deployment                           |

## 🚀 Quick Start

### Docker (fastest)

```sh
cp .env.example .env
make docker-up
```

App at `http://localhost:5173` · API at `http://localhost:8080`

### Local Dev

```sh
cp .env.example .env
docker compose up postgres redis -d
make migrate-up && make seed
make dev
```

### 📖 Commands

| Command                | What it does                          |
| ---------------------- | ------------------------------------- |
| `make dev`             | Run backend + frontend (with migrate) |
| `make dev-backend`     | Backend only                          |
| `make dev-frontend`    | Frontend only                         |
| `make dev-full`        | Backend + frontend + smee proxy       |
| `make dev-smee`        | Start webhook proxy (smee.io)         |
| `make migrate-up`      | Apply migrations                      |
| `make migrate-down`    | Roll back migrations                  |
| `make seed`            | Seed the database                     |
| `make reset-dev`       | Reset dev database                    |
| `make test`            | Run all tests                         |
| `make test-backend`    | Backend tests only                    |
| `make test-frontend`   | Frontend tests only                   |
| `make lint`            | Lint everything                       |
| `make docker-up`       | Start all (Docker)                    |
| `make docker-down`     | Stop all (Docker)                     |
| `make scan`            | Security scan backend + frontend      |

## 🏠 Self-Hosting

Kuayle is designed to be self-hosted. A production-grade Docker Compose setup with Caddy reverse proxy, automatic Let's Encrypt TLS, and an update script is available in [`selfhosting/`](selfhosting/).

### Prerequisites

- A Linux server with Docker + Docker Compose
- A domain pointing to your server (for HTTPS)

### 1. Clone and configure

```sh
git clone https://github.com/carbogninalberto/kuayle.git
cd kuayle/selfhosting
cp .env.example .env
```

Edit `.env` with your domain and production values:

```env
DOMAIN=kuayle.yourcompany.com
POSTGRES_PASSWORD=<strong-random-password>
JWT_SECRET=<random-string-at-least-32-chars>
```

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

The frontend serves the app and proxies `/api/*` to the backend, so one public origin is enough.

### Updating

```sh
cd kuayle
bash selfhosting/update.sh
```

The update script pulls the latest code, rebuilds images, recreates containers, and applies pending migrations.

### Storage options

By default, uploads go to the local filesystem. For production, you can use any S3-compatible storage:

```env
STORAGE_TYPE=s3
S3_ENDPOINT=https://s3.us-east-1.amazonaws.com
S3_BUCKET=kuayle-uploads
S3_REGION=us-east-1
S3_ACCESS_KEY=your-key
S3_SECRET_KEY=your-secret
```

Works with AWS S3, Cloudflare R2, MinIO, SeaweedFS, and any S3-compatible provider.

### GitHub Integration (optional)

Kuayle connects to GitHub via a self-configuring **GitHub App**. By default, each workspace creates its own app manifest from the UI. For SaaS deployments, set shared GitHub App credentials in `.env` so each workspace only needs to install the app (see `.env.example` for the `GITHUB_APP_*` variables).

#### What it does

- **Links PRs to issues** — mention `ENG-123` in a branch name, PR title, or commit message (case-insensitive)
- **Auto-transitions** — branch created → In Progress, PR opened → In Review, PR merged → Done (configurable)
- **Activity feed** — linked PRs, branches, and commits on every issue detail page

#### Public-facing instance (recommended)

If your Kuayle instance is reachable from the internet (e.g. `https://kuayle.yourcompany.com`), setup is fully automatic:

1. Go to **Settings → GitHub** in your workspace
2. Click **Set up GitHub App** — redirects to GitHub with a pre-filled form
3. Click **Create GitHub App** on GitHub
4. Redirected back — credentials saved automatically, webhook URL configured
5. Click **Install on GitHub** to grant access to your repos
6. Select which repos to link — done!

Everything including the webhook URL is configured automatically. PRs, branches, and commits start appearing on issues immediately.

#### Private network / no public domain

If your instance runs on a private network (e.g. `http://192.168.1.50:5173` or behind a VPN), GitHub can't send webhooks directly. You have three options:

**Option A: Webhook proxy with smee.io (simplest)**

[smee.io](https://smee.io) is a free webhook relay that forwards GitHub events to your private instance.

1. Go to [smee.io/new](https://smee.io/new) and copy your channel URL (e.g. `https://smee.io/abc123`)
2. Run the proxy on your server:
   ```sh
   npx smee-client --url https://smee.io/abc123 --target http://localhost:8080/api/github/webhook
   ```
3. Set up the GitHub App from **Settings → GitHub** (same steps as above)
4. After the app is created, go to its settings on GitHub:
   **GitHub → Settings → Developer settings → GitHub Apps → your app → General**
5. Set **Webhook URL** to your smee channel URL (`https://smee.io/abc123`)
6. Check **Active** and save

The smee client must stay running to receive events. You can run it as a systemd service:

```ini
# /etc/systemd/system/smee-kuayle.service
[Unit]
Description=Smee webhook proxy for Kuayle
After=network.target

[Service]
ExecStart=/usr/bin/npx smee-client --url https://smee.io/abc123 --target http://localhost:8080/api/github/webhook
Restart=always
User=kuayle

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now smee-kuayle
```

**Option B: Reverse tunnel (no third-party relay)**

Use a reverse tunnel to expose just the webhook endpoint. With [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/):

```sh
cloudflared tunnel --url http://localhost:8080
```

Or with [ngrok](https://ngrok.com):

```sh
ngrok http 8080
```

Then set the generated public URL as the webhook URL in your GitHub App settings (append `/api/github/webhook`).

**Option C: Polling (no webhook needed)**

If you can't expose any endpoint, the GitHub integration still works for PR/branch/commit linking — it just won't receive real-time webhook events. You can manually refresh issue activity from the UI. Auto-transitions won't fire without webhooks.

#### Local development

For local development, the GitHub App is created without a webhook URL (since localhost isn't reachable). To receive webhook events during development:

1. Set up the GitHub App from Settings → GitHub (works on localhost)
2. Start a smee proxy:
   ```sh
   npx smee-client --url https://smee.io/your-channel --target http://localhost:8080/api/github/webhook
   ```
3. Update the webhook URL in your GitHub App settings to your smee channel URL
4. Events will now flow through to your local instance

## 📂 Project Structure

```
kuayle/
├── BE/                     # Backend (Go)
│   ├── cmd/server/         # Entrypoint (server, migrate, seed)
│   └── internal/
│       ├── config/         # Configuration
│       ├── domain/         # Domain models
│       ├── dto/            # Data transfer objects
│       ├── handler/        # HTTP handlers
│       ├── service/        # Business logic
│       ├── repository/     # Data access (raw SQL)
│       ├── middleware/     # Auth and request middleware
│       └── realtime/       # WebSocket support
├── UI/                     # Frontend (SvelteKit)
│   └── src/
│       ├── routes/         # Pages
│       └── lib/
│           ├── api/        # API client
│           ├── components/ # UI components
│           ├── features/   # Feature modules
│           ├── types/      # TypeScript types
│           └── utils/      # Utilities
├── WEB/                    # Build output / minimal web root
├── selfhosting/            # Production Docker Compose + Caddy config
│   ├── docker-compose.yml
│   ├── Caddyfile
│   ├── update.sh
│   └── .env.example
├── scripts/                # Dev and maintenance scripts
├── docker-compose.yml      # Dev Docker Compose
├── Makefile
├── TECHNICAL.md            # Dev Machines specification
└── .env.example
```

## 🤝 Contributing

PRs welcome. Fork it, branch it, fix it, ship it.

1. Fork the repo
2. Create your branch (`git checkout -b feature/cool-thing`)
3. Commit your changes
4. Push and open a PR

## 📄 License

Apache 2.0, see [`LICENSE`](LICENSE).

## 📬 Contact

Alberto Carbognin, [@carbogninalberto](https://github.com/carbogninalberto)

## 🌍 Contributors

View the full contributor graph on GitHub: [contributors](https://github.com/carbogninalberto/kuayle/graphs/contributors).

---

> 🤖 **Heads up:** This codebase was built through AI-assisted development under my supervision, ideas, and direction. It works, but expect some rough edges that still need smoothing.

<!-- MARKDOWN LINKS & IMAGES -->

[contributors-shield]: https://img.shields.io/github/contributors/carbogninalberto/kuayle.svg?style=for-the-badge
[contributors-url]: https://github.com/carbogninalberto/kuayle/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/carbogninalberto/kuayle.svg?style=for-the-badge
[forks-url]: https://github.com/carbogninalberto/kuayle/network/members
[stars-shield]: https://img.shields.io/github/stars/carbogninalberto/kuayle.svg?style=for-the-badge
[stars-url]: https://github.com/carbogninalberto/kuayle/stargazers
[issues-shield]: https://img.shields.io/github/issues/carbogninalberto/kuayle.svg?style=for-the-badge
[issues-url]: https://github.com/carbogninalberto/kuayle/issues
[license-shield]: https://img.shields.io/github/license/carbogninalberto/kuayle.svg?style=for-the-badge
[license-url]: https://github.com/carbogninalberto/kuayle/blob/main/LICENSE
[product-screenshot]: assets/screenshot.png
