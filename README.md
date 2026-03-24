<div align="center">
  <img src="assets/logo.svg" alt="Kuayle" width="250">
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

## 🛠️ Tech Stack

Here's what Kuayle runs on and what each piece does:

| Layer | Tech | Role |
|---|---|---|
| **API** | Go + Echo | High-performance HTTP server with middleware, routing, JWT auth |
| **Database** | PostgreSQL 17 | Primary data store, raw SQL via sqlx/pgx, no ORM |
| **Cache & Jobs** | Redis 7 | Session caching, pub/sub, and background job queue via Asynq |
| **Frontend** | SvelteKit + Svelte 5 | SPA with TypeScript, runes-based reactivity, static adapter |
| **UI** | Tailwind CSS + shadcn-svelte | Utility-first styling with accessible component primitives |
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

> 🤖 **Heads up:** this project is vibecoded. The entire codebase was built through AI-assisted development. It works, it's structured well, but expect rough edges.

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
