# MCP server

Every Kuayle instance serves a [Model Context Protocol](https://modelcontextprotocol.io)
server at `/mcp`, so external agents (Claude Code, Claude Desktop, Cursor, Codex and
other MCP clients) can read and update issues and wiki pages.

## Connect

1. Create a personal access token under **Settings → API Tokens**. Give it only the
   scopes the agent needs: the read scopes for a read-only agent, plus `issue:create`,
   `issue:update` and `wiki:write` to let it write.
2. Open **Settings → MCP server** and copy the snippet for your client. For Claude Code:

   ```sh
   claude mcp add --transport http kuayle "https://kuayle.example.com/mcp?workspace=acme" \
     --header "Authorization: Bearer kuayle_pat_YOUR_TOKEN"
   ```

`?workspace=<slug>` selects the workspace the tools and resources act on. It can be
left out when the token reaches a single workspace, and every tool also accepts a
`workspace` argument that takes precedence.

Claude Desktop starts local servers only, so its snippet runs the endpoint through
[`mcp-remote`](https://github.com/geelen/mcp-remote) (needs Node.js). Codex reads the
token from the `KUAYLE_TOKEN` environment variable.

## Tools and resources

| Tool | Scope needed | What it does |
| --- | --- | --- |
| `list_workspaces` | `workspaces:read` | Workspaces the token can reach |
| `list_teams`, `list_statuses` | `teams:read` | Teams and a team's statuses |
| `list_projects` | `projects:read` | Projects |
| `list_cycles` | `cycles:read` | A team's cycles |
| `list_labels` | `labels:read` | Labels |
| `list_members` | `members:read` | Workspace members |
| `list_issues`, `get_issue` | `issues:read` | Search and filter issues; one issue by identifier |
| `list_comments` | `comments:read` | Comments of an issue |
| `create_issue`, `add_comment` | `issue:create` | New issue; new comment |
| `update_issue` | `issue:update` | Status, assignees, priority, labels, cycle, project, due date, title, description |
| `search_wiki_pages`, `get_wiki_page` | `wiki:read` | Pages of a team or project wiki, by title or content |
| `create_wiki_page`, `update_wiki_page` | `wiki:write` | New page; versioned update |

Resources: `kuayle://issue/{identifier}` (for example `kuayle://issue/KUA-12`) and
`kuayle://wiki/{team|project}/{scope_id}/{page_id}`.

`create_issue`, `add_comment` and `create_wiki_page` are not idempotent: a client
that retries one creates a second issue, comment or page. `update_wiki_page` needs
the version read with `get_wiki_page` and is rejected if the page changed since.

## Authorization

The server is a thin layer over the HTTP API: each tool call and resource read is
dispatched in-process to the API route named above, with the caller's token. The
route's own checks decide the outcome, so

- a token acts as its user, limited to its scopes and that user's workspace role;
- private teams, their issues and their wikis stay invisible to non-members;
- validation and error messages are the API's, returned as tool errors;
- changes appear in issue history and wiki versions under the token's user.

Only personal access tokens are accepted; browser sessions are refused. OAuth 2.1
sign-in for MCP clients is not available yet.

Requests to `/mcp` are limited to 10 per second per client address (bursts of 20);
a client over the limit gets `429` with `Retry-After`.

## Protocol

Streamable HTTP, stateless, protocol version `2025-06-18`: one JSON-RPC message per
`POST`, answered with JSON. The server offers no event stream, so `GET /mcp` returns
`405`. Behind your own reverse proxy, route `/mcp` to the backend like `/api/*`.
The bundled configurations already do: the all-in-one image, the web image
(`UI/Caddyfile`) and `selfhosting/Caddyfile`.

Because the exact path `/mcp` belongs to the server, `mcp` is not accepted as the
slug of a new or imported workspace. A workspace created with that slug before
this release keeps working: the bundled proxy configurations send `/mcp` to the
backend unless the request asks for a page (`Accept: text/html`), so a browser
still opens the workspace. Behind your own reverse proxy, apply the same rule if
you have such a workspace.

## Develop

Tools are rows in [`BE/internal/mcp/tools.go`](../BE/internal/mcp/tools.go): a name,
a description, an API route and its arguments. Add a tool by adding a row that
points at a `scoped` route; do not call services from the MCP package, which would
bypass the route's permission checks. Tests: `go test ./internal/mcp/` and, with
`DATABASE_URL`, `go test ./cmd/server -run TestMCP`.
