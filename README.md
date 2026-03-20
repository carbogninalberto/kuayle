<a id="readme-top"></a>

<!-- PROJECT SHIELDS -->
[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![AGPL v3 License][license-shield]][license-url]



<!-- PROJECT LOGO -->
<br />
<div align="center">
  <a href="https://github.com/carbogninalberto/carbon">
    <img src="assets/logo.png" alt="Logo" width="80" height="80">
  </a>

  <h3 align="center">Carbon</h3>

  <p align="center">
    A fast, keyboard-driven issue tracker inspired by Linear.
    <br />
    <br />
    <a href="https://github.com/carbogninalberto/carbon/issues/new?labels=bug">Report Bug</a>
    &middot;
    <a href="https://github.com/carbogninalberto/carbon/issues/new?labels=enhancement">Request Feature</a>
  </p>
</div>



<!-- DISCLAIMER -->
> [!WARNING]
> **This project is vibecoded.** The entire codebase was built through AI-assisted development — I designed the architecture, made every decision, and guided AI agents to write the code to my specification. It works, it's structured well, but expect rough edges. Use at your own risk.



<!-- TABLE OF CONTENTS -->
<details>
  <summary>Table of Contents</summary>
  <ol>
    <li>
      <a href="#about-the-project">About The Project</a>
      <ul>
        <li><a href="#features">Features</a></li>
        <li><a href="#built-with">Built With</a></li>
      </ul>
    </li>
    <li>
      <a href="#getting-started">Getting Started</a>
      <ul>
        <li><a href="#prerequisites">Prerequisites</a></li>
        <li><a href="#docker">Docker</a></li>
        <li><a href="#local-development">Local Development</a></li>
      </ul>
    </li>
    <li><a href="#usage">Usage</a></li>
    <li><a href="#project-structure">Project Structure</a></li>
    <li><a href="#roadmap">Roadmap</a></li>
    <li><a href="#contributing">Contributing</a></li>
    <li><a href="#license">License</a></li>
    <li><a href="#contact">Contact</a></li>
    <li><a href="#acknowledgments">Acknowledgments</a></li>
  </ol>
</details>



<!-- ABOUT THE PROJECT -->
## About The Project

[![Carbon Screenshot][product-screenshot]](https://github.com/carbogninalberto/carbon)

Carbon is a full-stack issue tracker built to mirror the speed and workflow of tools like Linear. It's opinionated about how engineering teams should organize work — fast navigation, keyboard shortcuts, and a clean UI that stays out of your way.

<p align="right">(<a href="#readme-top">back to top</a>)</p>



### Features

| | Feature | Description |
|---|---|---|
| **Workspaces** | Multi-tenant | Role-based access (owner, admin, member, guest) |
| **Teams** | Custom workflows | Each team gets its own statuses, estimate scales, and triage settings |
| **Issues** | Full-featured | Priority, estimates, due dates, sub-tasks, multi-assignee, labels, comments, audit history |
| **Cycles** | Sprint planning | Time-boxed iterations per team — upcoming, active, completed |
| **Projects** | Cross-team | Group work across multiple teams under a single initiative |
| **Labels** | Hierarchical | Workspace-scoped, nested labels with soft delete |
| **Views** | Saved filters | Shareable filtered perspectives with JSONB-based filter persistence |
| **Notifications** | Inbox | Snooze, read status, archive |
| **Webhooks** | Integrations | External integration support |
| **Real-time** | WebSockets | Live updates across all connected clients |

<p align="right">(<a href="#readme-top">back to top</a>)</p>



### Built With

* [![Go][Go-badge]][Go-url]
* [![Echo][Echo-badge]][Echo-url]
* [![PostgreSQL][PostgreSQL-badge]][PostgreSQL-url]
* [![Redis][Redis-badge]][Redis-url]
* [![Svelte][Svelte-badge]][Svelte-url]
* [![SvelteKit][SvelteKit-badge]][SvelteKit-url]
* [![TailwindCSS][TailwindCSS-badge]][TailwindCSS-url]
* [![TypeScript][TypeScript-badge]][TypeScript-url]
* [![Docker][Docker-badge]][Docker-url]

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- GETTING STARTED -->
## Getting Started

### Prerequisites

* [Go 1.21+](https://go.dev/dl/)
* [Node.js 20+](https://nodejs.org/)
* [Docker](https://docs.docker.com/get-docker/) (optional, for containerized setup)

### Docker

The fastest way to get running:

```sh
cp .env.example .env
make docker-up
```

App at `http://localhost:5173` | API at `http://localhost:8080`

### Local Development

```sh
cp .env.example .env

# Start Postgres and Redis
docker compose up postgres redis -d

# Run migrations and seed
make migrate-up
make seed

# Start both backend and frontend
make dev
```

#### Available Commands

| Command | Description |
|---|---|
| `make dev` | Run backend + frontend concurrently |
| `make dev-backend` | Backend only |
| `make dev-frontend` | Frontend only |
| `make migrate-up` | Apply database migrations |
| `make migrate-down` | Roll back migrations |
| `make seed` | Seed the database |
| `make test` | Run all tests |
| `make test-backend` | Backend tests |
| `make test-frontend` | Frontend tests |
| `make lint` | Lint everything |
| `make docker-up` | Start all services (Docker) |
| `make docker-down` | Stop all services (Docker) |

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- USAGE -->
## Usage

Once running, create a workspace and start organizing your work:

1. **Create a workspace** — your top-level container
2. **Add teams** — each team gets its own status workflow and settings
3. **Create issues** — assign priority, labels, estimates, and due dates
4. **Plan cycles** — group issues into time-boxed sprints
5. **Track projects** — organize cross-team initiatives

[![Carbon Board View][board-screenshot]](https://github.com/carbogninalberto/carbon)

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- PROJECT STRUCTURE -->
## Project Structure

```
carbon/
├── BE/                          # Backend (Go)
│   ├── cmd/server/              # CLI entrypoint (server, migrate, seed)
│   └── internal/
│       ├── config/              # Configuration
│       ├── domain/              # Domain models
│       ├── dto/                 # Data transfer objects
│       ├── handler/             # HTTP handlers
│       ├── service/             # Business logic
│       ├── repository/          # Data access (raw SQL)
│       ├── middleware/          # Auth & request middleware
│       ├── realtime/            # WebSocket support
│       └── worker/              # Background jobs (Asynq)
├── UI/                          # Frontend (SvelteKit)
│   └── src/
│       ├── routes/              # Pages
│       └── lib/
│           ├── api/             # API client
│           ├── components/      # UI components (shadcn-svelte)
│           ├── features/        # Feature modules
│           ├── types/           # TypeScript types
│           └── utils/           # Utilities
├── docker-compose.yml
├── Makefile
└── .env.example
```

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- ROADMAP -->
## Roadmap

- [x] Workspaces with role-based access
- [x] Teams with custom status workflows
- [x] Full issue management (priority, labels, estimates, sub-tasks)
- [x] Cycle-based sprint planning
- [x] Cross-team projects
- [x] Real-time WebSocket updates
- [x] Notification inbox
- [ ] Keyboard shortcuts
- [ ] Bulk issue operations
- [ ] Issue templates
- [ ] GitHub/GitLab integration
- [ ] Activity feed
- [ ] Dark mode

See the [open issues](https://github.com/carbogninalberto/carbon/issues) for a full list of proposed features and known issues.

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- CONTRIBUTING -->
## Contributing

Contributions are **greatly appreciated**. This is a vibecoded project — if you find something janky, that's expected. Fix it and send a PR.

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- LICENSE -->
## License

Distributed under the GNU Affero General Public License v3.0. See `LICENSE` for more information.

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- CONTACT -->
## Contact

Alberto Carbognin - [@carbogninalberto](https://github.com/carbogninalberto)

Project Link: [https://github.com/carbogninalberto/carbon](https://github.com/carbogninalberto/carbon)

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- ACKNOWLEDGMENTS -->
## Acknowledgments

* [Echo - High performance Go web framework](https://echo.labstack.com/)
* [SvelteKit](https://kit.svelte.dev/)
* [shadcn-svelte](https://www.shadcn-svelte.com/)
* [Tailwind CSS](https://tailwindcss.com/)
* [Asynq - Distributed task queue for Go](https://github.com/hibiken/asynq)
* [sqlx](https://github.com/jmoiron/sqlx)
* [Img Shields](https://shields.io)

<p align="right">(<a href="#readme-top">back to top</a>)</p>



<!-- MARKDOWN LINKS & IMAGES -->
[contributors-shield]: https://img.shields.io/github/contributors/carbogninalberto/carbon.svg?style=for-the-badge
[contributors-url]: https://github.com/carbogninalberto/carbon/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/carbogninalberto/carbon.svg?style=for-the-badge
[forks-url]: https://github.com/carbogninalberto/carbon/network/members
[stars-shield]: https://img.shields.io/github/stars/carbogninalberto/carbon.svg?style=for-the-badge
[stars-url]: https://github.com/carbogninalberto/carbon/stargazers
[issues-shield]: https://img.shields.io/github/issues/carbogninalberto/carbon.svg?style=for-the-badge
[issues-url]: https://github.com/carbogninalberto/carbon/issues
[license-shield]: https://img.shields.io/github/license/carbogninalberto/carbon.svg?style=for-the-badge
[license-url]: https://github.com/carbogninalberto/carbon/blob/main/LICENSE
[product-screenshot]: assets/screenshot.png
[board-screenshot]: assets/board.png

[Go-badge]: https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white
[Go-url]: https://go.dev/
[Echo-badge]: https://img.shields.io/badge/Echo-00ADD8?style=for-the-badge&logo=go&logoColor=white
[Echo-url]: https://echo.labstack.com/
[PostgreSQL-badge]: https://img.shields.io/badge/PostgreSQL-316192?style=for-the-badge&logo=postgresql&logoColor=white
[PostgreSQL-url]: https://www.postgresql.org/
[Redis-badge]: https://img.shields.io/badge/Redis-DC382D?style=for-the-badge&logo=redis&logoColor=white
[Redis-url]: https://redis.io/
[Svelte-badge]: https://img.shields.io/badge/Svelte-4A4A55?style=for-the-badge&logo=svelte&logoColor=FF3E00
[Svelte-url]: https://svelte.dev/
[SvelteKit-badge]: https://img.shields.io/badge/SvelteKit-FF3E00?style=for-the-badge&logo=svelte&logoColor=white
[SvelteKit-url]: https://kit.svelte.dev/
[TailwindCSS-badge]: https://img.shields.io/badge/Tailwind_CSS-38B2AC?style=for-the-badge&logo=tailwind-css&logoColor=white
[TailwindCSS-url]: https://tailwindcss.com/
[TypeScript-badge]: https://img.shields.io/badge/TypeScript-007ACC?style=for-the-badge&logo=typescript&logoColor=white
[TypeScript-url]: https://www.typescriptlang.org/
[Docker-badge]: https://img.shields.io/badge/Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white
[Docker-url]: https://www.docker.com/
