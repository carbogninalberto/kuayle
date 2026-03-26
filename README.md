<div align="center">
  <img src="assets/logo_primary.svg" alt="Kuayle" width="250">
  <p><strong>快乐 (kuàilè) · happiness, joy</strong></p>
  <p>A fast, keyboard-driven issue tracker inspired by Linear.</p>

  [Report Bug](https://github.com/carbogninalberto/kuayle/issues/new?labels=bug) · [Request Feature](https://github.com/carbogninalberto/kuayle/issues/new?labels=enhancement)
</div>

<br />

[![Kuayle Screenshot](assets/product-screenshot.png)](https://github.com/carbogninalberto/kuayle)

## 🧐 Why Kuayle?

I've been a happy Linear user for years, it's the gold standard for issue tracking. But some things kept bugging me: no multi-assignee on issues, no project-based Gantt, analytics locked behind a paywall, and per-seat pricing that adds up quickly for small teams.

With AI, building your own tool is now realistic, so I did. First for myself, then for anyone who wants Linear-quality without the price tag.

I also looked at the open-source alternatives out there, but they gate core features behind paid plans, which kind of defeats the purpose. Kuayle takes a different approach: **every feature is free, forever.** No paid tiers, no feature gates, Apache 2.0. If you find it useful, consider sponsoring the project, that's the whole model.

## ✨ Features

| | Feature | Description |
|---|---|---|
| 🏢 | **Workspaces** | Multi-tenant with role-based access (owner, admin, member, guest) |
| 👥 | **Teams** | Custom workflows, each team gets its own statuses, estimate scales, and triage settings |
| 📋 | **Issues** | Priority, estimates, due dates, sub-tasks, multi-assignee, labels, comments, audit history |
| 🔄 | **Cycles** | Sprint planning with time-boxed iterations (upcoming, active, completed) |
| 📁 | **Projects** | Cross-team initiatives grouped under a single umbrella |
| 🏷️ | **Labels** | Hierarchical, workspace-scoped, with soft delete |
| 👁️ | **Views** | Saved and shareable filtered perspectives with JSONB persistence |
| 🔔 | **Notifications** | Inbox with snooze, read status, and archive |
| 🔗 | **Webhooks** | Plug into external services and integrations |
| ⚡ | **Real-time** | WebSocket-powered live updates across all connected clients |
| 🖥️ | **Dev Machines** | On-demand, single-container development environments with agentic coding, browser, and auto work tracking |
| 🐙 | **GitHub** | Link repos, auto-sync issues, webhook-based updates |
| 📊 | **Analytics** | Overview dashboards and issue distribution charts |
| 🔗 | **Public Sharing** | Token-based read-only links for issues and views |

## 🤖 Dev Machines (Agentic Coding)

Kuayle can spawn **on-demand, single-container development environments** on a VPS. Each machine is a disposable workspace with an IDE, agentic coding CLI, and a browser — accessible via auto-generated subdomains.

> See [`TECHNICAL.md`](TECHNICAL.md) for the full specification, architecture diagrams, and API reference.

### How it works

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
2. **Assigns random subdomains** via wildcard DNS (`*.kuayle.com`) — no per-container port management
3. **Authenticates** users and agents through Kuayle's session layer
4. **Tracks all activity** (edits, commands, git ops, navigation) and feeds it back into project management

### Two modes

| Mode | Who | What happens |
|---|---|---|
| **Agent-only** | Kuayle assigns a task | Agent works autonomously, pushes results, machine tears down |
| **Human + Agent** | Developer clicks "Open Machine" | Gets a browser link to a full IDE with agentic tools, works interactively |

### Configuration

Machines are configured from Kuayle's UI or via API — repo, branch, env vars, tools, and size. Configuration resolves in order: project defaults → user preferences → spawn-time overrides.

| Size | CPU | Memory | Disk |
|---|---|---|---|
| Small | 2 cores | 4 GB | 20 GB |
| Medium | 4 cores | 8 GB | 50 GB |
| Large | 8 cores | 16 GB | 100 GB |

## 🛠️ Tech Stack

Here's what Kuayle runs on and what each piece does:

| Layer | Tech | Role |
|---|---|---|
| **API** | Go + Echo | High-performance HTTP server with middleware, routing, JWT auth |
| **Database** | PostgreSQL 17 | Primary data store, raw SQL via sqlx/pgx, no ORM |
| **Cache & Jobs** | Redis 7 | Session caching, pub/sub, and background job queue via Asynq |
| **Frontend** | SvelteKit + Svelte 5 | SPA with TypeScript, runes-based reactivity, static adapter |
| **UI** | Tailwind CSS + shadcn-svelte | Utility-first styling with accessible component primitives |
| **Editor** | Tiptap v3 + Yjs | Rich text with code blocks, mentions, slash commands, task lists |
| **Real-time** | WebSocket (nhooyr.io) | Live collaboration, presence, issue updates |
| **Storage** | Local FS or S3-compatible | AWS S3, Cloudflare R2, MinIO, SeaweedFS |
| **Reverse Proxy** | Caddy | Production HTTPS and routing |
| **Dev Machines** | code-server + Claude Code + Chromium | Single-container agentic dev environments |
| **Infra** | Docker + Docker Compose | One-command local and production deployment |

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

| Command | What it does |
|---|---|
| `make dev` | Run backend + frontend |
| `make dev-backend` | Backend only |
| `make dev-frontend` | Frontend only |
| `make migrate-up` | Apply migrations |
| `make migrate-down` | Roll back migrations |
| `make seed` | Seed the database |
| `make test` | Run all tests |
| `make lint` | Lint everything |
| `make docker-up` | Start all (Docker) |
| `make docker-down` | Stop all (Docker) |

## 🏠 Self-Hosting

Kuayle is designed to be self-hosted. Everything runs in Docker, no managed services required.

### Prerequisites

- A Linux server (or any host that runs Docker)
- Docker + Docker Compose
- A domain (optional, for HTTPS)

### 1. Clone and configure

```sh
git clone https://github.com/carbogninalberto/kuayle.git
cd kuayle
cp .env.example .env
```

Edit `.env` with production values:

```env
DATABASE_URL=postgres://kuayle:<strong-password>@postgres:5432/kuayle?sslmode=disable
REDIS_URL=redis://redis:6379
JWT_SECRET=<random-string-at-least-32-chars>
ENVIRONMENT=production
FRONTEND_URL=https://your-domain.com
```

### 2. Launch

```sh
docker compose up --build -d
```

This starts 4 containers: PostgreSQL, Redis, backend API (`:8080`), and frontend (`:5173`).

### 3. Run migrations and seed

```sh
docker compose exec backend /app/server migrate up
docker compose exec backend /app/server seed
```

### 4. Reverse proxy (recommended)

Put Nginx, Caddy, or Traefik in front to handle HTTPS. Example with Caddy:

```
your-domain.com {
    reverse_proxy localhost:5173
}

api.your-domain.com {
    reverse_proxy localhost:8080
}
```

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

Kuayle can connect to GitHub via a **GitHub App** to automatically link pull requests, branches, and commits to issues, and auto-transition issue status based on PR activity.

#### 1. Create a GitHub App

1. Go to **GitHub → Settings → Developer settings → GitHub Apps → New GitHub App**
2. Fill in the form:

| Field | Value |
|---|---|
| **App name** | `Kuayle` (or any name) |
| **Homepage URL** | Your Kuayle instance URL |
| **Callback URL** | `https://your-domain.com/<workspace-slug>/settings/github` |
| **Webhook URL** | `https://your-api-domain.com/api/github/webhook` |
| **Webhook secret** | Generate a random string (save this for `.env`) |

3. Set **permissions**:

| Permission | Access |
|---|---|
| **Pull requests** | Read |
| **Contents** | Read |
| **Metadata** | Read |
| **Issues** | Read (optional, for future sync) |

4. Subscribe to **events**:
   - `Pull request`
   - `Push`
   - `Create` (branch/tag creation)

5. Set **Where can this GitHub App be installed?** to "Any account" (or "Only on this account" for private use)

6. Click **Create GitHub App**

#### 2. Generate a private key

After creating the app:

1. On the app settings page, scroll to **Private keys**
2. Click **Generate a private key** — this downloads a `.pem` file
3. Base64-encode it for the env var:

```sh
cat your-app-name.2024-01-01.private-key.pem | base64 -w 0
```

#### 3. Get your App credentials

From the app settings page, note:
- **App ID** (numeric, shown at the top)
- **Client ID** (starts with `Iv1.`)
- **Client secret** (generate one if not already created)

#### 4. Configure environment variables

Add these to your `.env`:

```env
GITHUB_APP_ID=123456
GITHUB_APP_PRIVATE_KEY=LS0tLS1CRUdJTi...  # base64-encoded PEM
GITHUB_CLIENT_ID=Iv1.abc123
GITHUB_CLIENT_SECRET=your-client-secret
GITHUB_WEBHOOK_SECRET=your-webhook-secret
```

Restart the backend. The integration is **disabled** when `GITHUB_APP_ID` is not set.

#### 5. Connect from Kuayle

1. Go to **Settings → GitHub** in your workspace
2. Click **Connect** — this redirects to GitHub to install the app
3. Select which repositories to grant access to
4. After redirect back, select which repos to link in Kuayle
5. Configure auto-transition rules (branch created → In Progress, PR opened → In Review, PR merged → Done)

#### How issue linking works

Kuayle matches issue identifiers (e.g. `ENG-123`) in:
- **Branch names** — `feat/ENG-123-add-auth`
- **PR titles** — `fix: resolve ENG-123 login bug`
- **PR descriptions** — any mention of `ENG-123` in the body
- **Commit messages** — `ENG-123: update schema`

Matched issues automatically show linked PRs, branches, and commits on the issue detail page.

#### Local development

For local development, you can use [smee.io](https://smee.io) to forward GitHub webhooks to your local machine:

```sh
npx smee-client --url https://smee.io/your-channel --target http://localhost:8080/api/github/webhook
```

Set the smee URL as the webhook URL in your GitHub App settings during development.

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
│       ├── middleware/      # Auth and request middleware
│       ├── realtime/       # WebSocket support
│       └── worker/         # Background jobs (Asynq)
├── UI/                     # Frontend (SvelteKit)
│   └── src/
│       ├── routes/         # Pages
│       └── lib/
│           ├── api/        # API client
│           ├── components/ # UI components
│           ├── features/   # Feature modules
│           ├── types/      # TypeScript types
│           └── utils/      # Utilities
├── docker-compose.yml
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

<a href="https://github.com/carbogninalberto/kuayle/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=carbogninalberto/kuayle" />
</a>

[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![Apache 2.0 License][license-shield]][license-url]

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
