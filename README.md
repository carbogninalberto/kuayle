<div align="center">
  <img src="assets/logo_primary.svg" alt="Kuayle" width="250">
  <p><strong>快乐 (kuàilè) · happiness, joy</strong></p>
	<p><strong>The issue tracker with no paid tier.</strong></p>
	<p>Keyboard-driven, self-hosted, and available in one public Apache 2.0 repository.</p>

[![Latest release](https://img.shields.io/github/v/release/carbogninalberto/kuayle)](https://github.com/carbogninalberto/kuayle/releases/latest)
[![License: Apache 2.0](https://img.shields.io/github/license/carbogninalberto/kuayle)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-%E2%9D%A4-ea4aaa?logo=githubsponsors)](https://github.com/sponsors/carbogninalberto)

[Report Bug](https://github.com/carbogninalberto/kuayle/issues/new?labels=bug) · [Request Feature](https://github.com/carbogninalberto/kuayle/issues/new?labels=enhancement)

</div>

<br />

[![Kuayle Screenshot](assets/product-screenshot.png)](https://github.com/carbogninalberto/kuayle)

## What is Kuayle

Kuayle is an open-source issue tracker you run on your own server. It is fast and
keyboard-driven, covers issues, cycles, projects and wikis, and comes with an AI
assistant that can hand issues to coding agents running on your own machines.

## Why Kuayle

It started with a narrow requirement: keep a fast, keyboard-oriented issue
workflow, and add multiple assignees, self-hosting and full source access.

The distribution model is deliberately simple:

- one public repository under Apache 2.0;
- no paid tier, enterprise edition, feature gate or license key;
- no per-user software fee;
- infrastructure, backups, monitoring and updates are the operator's job.

Kuayle is intentionally smaller than broad project-management suites. It does not
include enterprise identity (SSO) or modules.

## Features

**Issues and planning**

- Workspaces with owner, admin, member and guest roles; teams with their own
  statuses and triage; private teams.
- Issues with priority, due dates, multiple assignees, hierarchical labels,
  sub-issues, relations (blocking, duplicate, related), comments, templates and a
  title/description version history shown as diffs.
- Cycles with burndown and velocity charts; cross-team projects with a Gantt view.
- Saved views (personal, team or workspace), favorites, a
  notification inbox (read state, snooze, archive), a command palette with global
  search, keyboard shortcuts, and a Tiptap editor with code blocks, slash
  commands, mentions, task lists and file uploads.
- Analytics: workspace and team overviews, burn-up trends, configurable insights.
- Real-time updates and presence over WebSockets; read-only public links for
  issues and views.
- UI in English, Italian, and Simplified and Traditional Chinese.

**AI assistant**

- Workspace chat backed by any OpenAI-compatible provider you configure.
- Tools to search, create, update, plan and link issues, read and edit wiki
  pages, delegate issues to coding agents, and follow pull requests and releases.
  Writes and destructive actions need your approval.
- Read-only research: web search (Brave, Tavily or self-hosted SearXNG) and
  repository scans.
- Workspace and team skills (Markdown guidance) and custom script tools.
- The same assistant on Telegram, and voice dictation transcribed on your server.

**Agents and Dev Machines** (opt-in, disabled by default)

- Multi-container dev machines with code-server, a native terminal and an
  in-browser Chrome, behind an authenticated gateway, with one Git worktree per issue.
- Coding agents: Claude Code, Codex, OpenCode, Qwen Code, Crush, Goose and Kimi,
  plus admin-configured CLIs.
- Delegated tasks run an independent check, open a pull request, and pick up CI
  failures and review feedback. Blocked issues wait for their blockers.
- Automations start agents on issues matching a label or status.

**Agent workflows** (Settings → Agents workflows)

- A visual editor to design the steps a coding task follows: implement, test,
  open a pull request, agent review, CI, your go-ahead and merge. Start from
  templates (Standard, Review loop, Full cycle) or a blank workflow.
- Defaults per workspace, team or project; the most specific one wins.
- Agent profiles: a role, coding agent, model and instructions, plus skills from
  the workspace library delivered as `SKILL.md` files. Profiles never grant
  permissions; every step runs under the workspace policy.

**Wiki and registry**

- Team and project wikis: pages in a folder tree, autosave, AI rework of a
  selection, and version history with diffs and restore.
- Publish a page (and optionally its children) to an internal reader or a public
  link.
- A daily registry log, with AI overviews on demand or on a daily/weekly schedule.

**Integrations**

- GitHub App: links branches, commits and pull requests to issues, moves issues
  through configurable status rules, and comments on pull requests.
- Outgoing webhooks, personal access tokens with scopes, and Telegram.
- Workspace export/import between instances, with assets and ID remapping.

**Self-hosting**

- Reference Docker Compose stack: Caddy, PostgreSQL, Redis, backend and frontend.
- Update script, optional one-click updater from Settings → Version, a
  release-image updater, and an in-app release changelog.
- Optional voice transcription, SearXNG web search and S3-compatible storage.

## Current state

Releases are runnable MVPs, not a mature enterprise platform. The repository has
the Go API, the SvelteKit frontend, migrations, development tooling and a
reference self-hosting setup. Evaluate and test it against your requirements
before production use.

## Quick start

**Docker** (everything in containers):

```sh
cp .env.example .env
make docker-up
```

App at `http://localhost:5173`, API at `http://localhost:8080`.

**Local development** (backend and Vite on the host, services in Docker):

```sh
cp .env.example .env
make reset-dev
make seed
make dev-start-services
make dev
```

See [development](docs/development.md) for all `make` commands, the tech stack and
the project layout.

## Self-hosting

The reference stack lives in [`selfhosting/`](selfhosting/). You need a Linux
server with Docker Compose v2 and a domain; Caddy handles HTTPS. Clone, fill in
`selfhosting/.env`, run `docker compose up --build -d`, then apply migrations.

The [self-hosting guide](docs/self-hosting.md) covers install, updates, storage,
Dev Machines, voice, web search, private teams and workspace transfer. GitHub
setup is in [GitHub integration](docs/github-integration.md). All docs are
indexed in [docs/](docs/README.md).

## Community and ecosystem

- [kuayle_cli](https://github.com/uiYzzi/kuayle_cli) by [@uiYzzi](https://github.com/uiYzzi):
  an unofficial Rust CLI and SDK for using Kuayle from the terminal, by people or
  coding agents.

## Contributing

Pull requests are welcome. Keep changes focused, explain the behavior you change
and include relevant validation: fork, branch, commit, open a PR.

## Sponsoring

If Kuayle is useful to you, you can support it through
[GitHub Sponsors](https://github.com/sponsors/carbogninalberto).

## Contributors

<!-- contributors:start -->
<a href="https://github.com/carbogninalberto"><img src="https://avatars.githubusercontent.com/u/17618905?v=4&amp;s=64" width="64" height="64" alt="@carbogninalberto" title="@carbogninalberto"></a>
<a href="https://github.com/carbninai"><img src="https://avatars.githubusercontent.com/u/346896133?v=4&amp;s=64" width="64" height="64" alt="@carbninai" title="@carbninai"></a>
<a href="https://github.com/uiYzzi"><img src="https://avatars.githubusercontent.com/u/40852301?v=4&amp;s=64" width="64" height="64" alt="@uiYzzi" title="@uiYzzi"></a>
<a href="https://github.com/kasperhartwich"><img src="https://avatars.githubusercontent.com/u/616231?v=4&amp;s=64" width="64" height="64" alt="@kasperhartwich" title="@kasperhartwich"></a>
<a href="https://github.com/MarioGK"><img src="https://avatars.githubusercontent.com/u/8379079?v=4&amp;s=64" width="64" height="64" alt="@MarioGK" title="@MarioGK"></a>
<a href="https://github.com/manelpb"><img src="https://avatars.githubusercontent.com/u/1784206?v=4&amp;s=64" width="64" height="64" alt="@manelpb" title="@manelpb"></a>
<!-- contributors:end -->

## License

Apache 2.0, see [`LICENSE`](LICENSE).

## Contact

Alberto Carbognin, [@carbogninalberto](https://github.com/carbogninalberto)

---

> **Project note:** Kuayle has been developed with AI-assisted tooling under human
> direction and review.
