# Kuayle docs

## Operators

- [Self-hosting](self-hosting.md): install, update, storage, optional services
  (Dev Machines, voice, web search), private teams, workspace transfer and
  upgrade notes.
- [GitHub integration](github-integration.md): GitHub App setup, webhooks on
  private networks and local development.
- [Forge integrations](forge-integrations.md): GitLab, Gitea and Forgejo
  connections, webhook setup, token scopes and network safety.
- [Dev Machines](dev-machines.md): architecture, modes and machine sizes.

## Users

- [Kuayle CLI](cli.md): install with checksum verification, sign in with a token,
  issues, wiki and registry from the terminal or a coding agent.
- [MCP server](mcp.md): connect Claude Code, Claude Desktop, Cursor or Codex with a
  token; tools, resources and authorization.

## Integrators

- [API reference](api.md): the OpenAPI 3.1 document every instance serves at
  `/api/openapi.json`, how it is generated and tested, and a generated-client
  smoke test.
- [API tokens](api-tokens.md): which endpoints and verbs a personal access
  token can reach, the scope for each route, session-only reasons, and the
  `kuayle_cli` connection-link contract.

## Contributors

- [Development](development.md): run from source, `make` commands, tech stack,
  project layout and [translations](development.md#translations).
- [TECHNICAL.md](../TECHNICAL.md): Dev Machines specification and system reference.
- Backend references in [BE/docs](../BE/docs): [assistant conversations](../BE/docs/conversations.md),
  [agent tasks](../BE/docs/agent-tasks.md), [GitHub integration](../BE/docs/github-integration.md),
  [forge integrations](../BE/docs/forge-integrations.md), [Telegram](../BE/docs/telegram.md)
  and [passkeys](../BE/docs/passkeys.md) (endpoints,
  relying party configuration, threat model).

## Design notes

- [KUA-62 control migration audit](kua-62-controls.md)
- [KUA-67 diff panel controls](kua-67-diff-panel.md)
