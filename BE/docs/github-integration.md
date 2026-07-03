# GitHub Integration

Connects GitHub repositories to Kuayle workspaces, automatically linking pull requests, branches, and commits to issues and transitioning issue status based on GitHub activity.

## How It Works

- **Issue linking**: Branch names, PR titles/bodies, and commit messages are scanned for issue identifiers matching the pattern `[A-Z][A-Z0-9]+-\d+` (e.g., `PROJ-42`, `ENG-123`). The match is case-insensitive — `kua-1`, `KUA-1`, and `Kua-1` all work. When a match is found, the GitHub activity is linked to the corresponding issue.
- **Auto-transitions**: When GitHub events occur (branch created, PR opened, PR merged), issues are automatically moved to configured statuses (e.g., In Progress, In Review, Done).
- **Real-time updates**: GitHub activity is pushed to connected clients via WebSocket.

## Deployment Modes

### SaaS Mode (Shared App)

A single GitHub App is pre-configured and shared across all workspaces. Users only need to install the app on their GitHub account/org — no manifest setup required.

**Setup:**

1. [Create a GitHub App](https://github.com/settings/apps/new) manually with these settings:

   **Permissions:**
   | Permission | Access |
   |---|---|
   | Pull requests | Read |
   | Contents | Read |
   | Metadata | Read |
   | Issues | Read |

   **Events to subscribe to:**
   - Pull request
   - Push
   - Create (branch/tag)

   **Callback URL:** `https://your-domain.com/{workspace-slug}/settings/github`
   **Setup URL:** Same as callback URL (enable "Redirect on update")
   **Webhook URL:** `https://your-api-domain.com/api/github/webhook`

2. Set environment variables:

   ```bash
   GITHUB_APP_ID=123456
   GITHUB_APP_SLUG=your-app-slug
   GITHUB_APP_PRIVATE_KEY=<base64-encoded PEM>
   GITHUB_APP_CLIENT_ID=Iv1.abc123
   GITHUB_APP_CLIENT_SECRET=secret
   GITHUB_APP_WEBHOOK_SECRET=webhook-secret
   ```

   To base64-encode the private key:
   ```bash
   cat private-key.pem | base64
   ```

3. Each workspace user clicks "Install on GitHub" in Settings > GitHub, selects their org/account, and chooses which repos to grant access to.

### Self-Hosted Mode (Per-Workspace App)

When no `GITHUB_APP_ID` is set, each workspace creates its own GitHub App via the [manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest). The user clicks "Set up" and GitHub creates the app automatically — no manual configuration needed.

This mode stores app credentials (encrypted with AES-256-GCM) per-workspace in the database.

## Webhook Configuration

Webhooks are how GitHub notifies Kuayle about events (PRs, pushes, branches).

- **SaaS mode**: Configure the webhook URL when creating the GitHub App (see above).
- **Self-hosted mode**: The webhook URL is auto-derived from `FRONTEND_URL`. For localhost/dev, webhooks are disabled by default.
- **Dev override**: Set `GITHUB_WEBHOOK_URL` to a [smee.io](https://smee.io) URL to receive webhooks during local development.

Webhook payloads are verified using HMAC-SHA256 signatures before processing.

## Auto-Transitions

Default rules are created when a workspace installs the GitHub App:

| GitHub Event | Issue Status |
|---|---|
| Branch created (matching issue identifier) | In Progress |
| PR opened | In Review |
| PR merged | Done |

Users can enable/disable each rule in Settings > GitHub > Automations.

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `GITHUB_APP_ID` | No | GitHub App ID (enables SaaS mode) |
| `GITHUB_APP_SLUG` | No | GitHub App slug (for install URL) |
| `GITHUB_APP_PRIVATE_KEY` | No | Base64-encoded PEM private key |
| `GITHUB_APP_CLIENT_ID` | No | GitHub App client ID |
| `GITHUB_APP_CLIENT_SECRET` | No | GitHub App client secret |
| `GITHUB_APP_WEBHOOK_SECRET` | No | Webhook signature secret |
| `GITHUB_WEBHOOK_URL` | No | Override webhook URL (e.g., smee.io for dev) |
