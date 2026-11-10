# Development

How to run Kuayle from source. For production installs see [self-hosting](self-hosting.md).

## Quick start

### Docker

```sh
cp .env.example .env
make docker-up
```

App at `http://localhost:5173` · API at `http://localhost:8080`

### Local dev

```sh
cp .env.example .env
make reset-dev
make seed
make dev-start-services
make dev
```

`make dev` runs the Go backend on `localhost:8080` and Vite on
`localhost:5173` directly on the host for hot reload. The root Compose stack is
for supporting services only during local development: Postgres, Redis, the Dev
Machine gateway/manager, runtime image builds, and the Caddy wildcard TLS proxy
for `https://*.machines.localhost`. The dev proxy binds `127.0.0.1:80` and
`127.0.0.1:443`; stop the selfhosting stack first if it is using those ports.
The root gateway container is named `kuayle-dev-machine-gateway` to avoid the
selfhosting `kuayle-machine-gateway` name.

## Commands

| Command                | What it does                          |
| ---------------------- | ------------------------------------- |
| `make dev`             | Run backend + frontend (with migrate) |
| `make dev-start-services` | Start Postgres, Redis, and Dev Machine infrastructure |
| `make dev-all`         | `dev-start-services`, then `dev`      |
| `make dev-reset`       | Reset, seed, start infrastructure, then run host dev |
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

`make test`, `make test-frontend` and `make lint` call UI npm scripts (`test`, `lint`) that
do not exist; use the package scripts in [UI/package.json](../UI/package.json) instead
(`npm run check`, `npm run test:unit`, `npm run test:e2e`). See
[AGENTS/TOOLS.md](../AGENTS/TOOLS.md) for verified commands.

## Tech stack

| Layer             | Tech                                 | Role                                                                  |
| ----------------- | ------------------------------------ | --------------------------------------------------------------------- |
| **API**           | Go + Echo                            | High-performance HTTP server with middleware, routing, JWT auth       |
| **Database**      | PostgreSQL 17                        | Primary data store, raw SQL via sqlx/pgx, no ORM                      |
| **Redis**         | Redis 7                              | Required configuration; reserved for future cache and job use         |
| **Frontend**      | SvelteKit + Svelte 5                 | SPA with TypeScript, runes-based reactivity, static adapter           |
| **UI**            | Tailwind CSS + shadcn-svelte         | Utility-first styling with accessible component primitives            |
| **Editor**        | Tiptap v3 + Yjs                      | Rich text with code blocks, mentions, slash commands, task lists      |
| **Real-time**     | WebSocket (nhooyr.io)                | Workspace events, issue presence, and targeted notifications          |
| **Storage**       | Local FS or S3-compatible            | AWS S3, Cloudflare R2, MinIO, SeaweedFS                               |
| **Reverse Proxy** | Caddy                                | Production HTTPS and routing                                          |
| **Dev Machines**  | Docker + Caddy + Go gateway/manager + code-server + ttyd/xterm + Chrome/KasmVNC | Opt-in multi-container development environments with issue worktrees and provider abstraction |
| **Infra**         | Docker + Docker Compose              | Local development and reference self-hosting stack                    |

## Project structure

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
├── WEB/                    # Public marketing site (SvelteKit, statically generated)
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

## Translations

Components import `m`, `getLocale`, `setLocale` and `locales` from
`$lib/features/i18n/messages`, never from `$lib/paraglide/*` directly.

- **Full locales** (en, it, zh-CN, zh-TW) live in `UI/messages/<locale>.json`, are listed
  in `UI/project.inlang/settings.json` and are compiled by Paraglide.
  `UI/src/lib/paraglide/` is generated by `npm run check`; never edit it by hand.
- **Community locales** live in `UI/messages/community/<locale>.json`. They are not
  compiled. Only the selected catalog is loaded, as its own chunk, before the app
  starts (`src/hooks.client.ts`). Missing keys fall back to English. This keeps the
  shared bundle the same size as with the full locales alone.
- Native names, support level and caveats live in `UI/src/lib/features/i18n/locales.ts`.

| Level | Locales | Notes |
|---|---|---|
| Full | en, it, zh-CN, zh-TW | Maintained; new strings are translated with the change. |
| Community | es, pt-BR, pt-PT, de, fr, ja, ko, nl, tr, vi, id, ms, hi, sv, da, nb, ca | Machine-assisted, unreviewed. |
| Community, plural caveat | ru, pl, uk, cs, ro, hu, el, fi, ar | Messages use one plural form (`{count}` style). |
| Community, line-break caveat | th | No word spaces; wrapping is not tuned. |
| Community, RTL caveat | ar, he, fa | Translated text, left-to-right layout. |

Rules:

- Every catalog must have exactly the keys of `en.json`, in the same order, with
  the same `{placeholders}` (the English plural helpers `{s}` and `{plural}` may be
  dropped). Community values are plain strings with `{name}` placeholders only.
  `node --test UI/tests/unit/locale-catalogs.test.mjs` enforces this.
- When adding a string, add it to every catalog. Translate it properly for the full
  locales; for community locales a best-effort translation is acceptable.
- To add a community locale: add `messages/community/<code>.json` and an entry in
  `locales.ts`, and list it in the README "Supported languages" section. To promote one
  to full: move the catalog to `messages/`, add the tag to `settings.json` and set
  `support: 'full'`.
- Native-speaker reviews of community catalogs are welcome.
