# Carbon

A Linear-style issue tracker built with Go (Echo) and SvelteKit.

## Tech Stack

- **Backend:** Go, Echo v4, sqlx/pgx, Redis (Asynq), PostgreSQL
- **Frontend:** SvelteKit 2, Svelte 5, Tailwind CSS v4, shadcn-svelte

## Quick Start

### With Docker

```bash
cp .env.example .env
make docker-up
```

The app will be available at `http://localhost:5173` with the API at `http://localhost:8080`.

### Local Development

```bash
cp .env.example .env

# Start Postgres and Redis (or use Docker for just the services)
docker compose up postgres redis -d

# Run migrations
make migrate-up

# Start both backend and frontend
make dev
```

### Available Commands

| Command | Description |
|---|---|
| `make dev` | Run backend and frontend concurrently |
| `make dev-backend` | Run backend only |
| `make dev-frontend` | Run frontend only |
| `make migrate-up` | Apply database migrations |
| `make migrate-down` | Roll back database migrations |
| `make seed` | Seed the database |
| `make test` | Run all tests |
| `make test-backend` | Run backend tests |
| `make test-frontend` | Run frontend tests |
| `make lint` | Lint backend and frontend |
| `make docker-up` | Start all services with Docker |
| `make docker-down` | Stop all Docker services |
