# GitHub Integration

Connects GitHub repositories to Kuayle workspaces, automatically linking pull requests, branches, and commits to issues and transitioning issue status based on GitHub activity.

## How It Works

- **Issue linking**: Branch names, PR titles/bodies, and commit messages are scanned for issue identifiers matching the pattern `[A-Z][A-Z0-9]+-\d+` (e.g., `PROJ-42`, `ENG-123`). The match is case-insensitive — `kua-1`, `KUA-1`, and `Kua-1` all work. When a match is found, the GitHub activity is linked to the corresponding issue.
- **PR bot comments**: A newly opened PR (including drafts and agent-authored PRs) receives a comment linking to its Kuayle issue. PR association requires exactly one existing public-team issue across the branch, title, and body; repeated case-insensitive references are fine. Ordinary tokens such as `SHA-256` are ignored unless they identify an issue in the workspace. Multiple matching issues, unknown issues, private-team issues, and inactive/unlinked repositories produce no comment. More than 20 distinct candidate identifiers is skipped without choosing a partial result.
- **Auto-transitions**: When GitHub events occur (branch created, PR opened, PR merged), issues are automatically moved to configured statuses (e.g., In Progress, In Review, Done).
- **Real-time updates**: GitHub activity is pushed to connected clients via WebSocket.

## Dev Machines Repository Tokens

Dev Machines use GitHub App installation tokens, not personal access tokens. Issue worktrees resolve their linked repository from development defaults in this order: issue, project, team, workspace. One machine has affinity to one linked repository and can hold multiple issue worktrees from that repository; use a separate machine for another repository or development environment.

For checkout, agent push, and pull request operations, Kuayle mints a short-lived installation token restricted to the selected linked repository and injects it only into the developer or agent container that needs Git.

## Deployment Modes

### SaaS Mode (Shared App)

A single GitHub App is pre-configured and shared across all workspaces. Users only need to install the app on their GitHub account/org — no manifest setup required.

**Setup:**

1. [Create a GitHub App](https://github.com/settings/apps/new) manually with these settings:

   **Permissions:**
   | Permission | Access |
   |---|---|
   | Pull requests | Read and write |
   | Contents | Read and write |
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

## PR bot comments (KUA-60)

The existing GitHub App posts `Linked Kuayle issue: [KUA-60](https://your-domain.com/your-workspace/issue/KUA-60)` on `pull_request.opened`. It does not edit the PR description, include the issue title/description, or depend on the PR author's account type. Issue access still requires the normal Kuayle authorization. There is no historical backfill, and edits/reopens do not create new comments.

Set `FRONTEND_URL` to the externally reachable Kuayle URL. Both the shared app and per-workspace manifest app use their existing installation credentials. **Pull requests: Read and write** permits PR comments through GitHub's issue-comments endpoint; **Issues: write** is not required. Existing installations need no permission change if they already grant the documented permissions. The app must subscribe to **Pull request**, and the repository must be linked and active in the workspace.

Duplicate deliveries are serialized with a PostgreSQL advisory lock keyed by repository and PR number, including across API replicas. Before posting, the bot scans all comment pages for its workspace marker **and its GitHub App ID and bot author**, so a copied marker from another author does not suppress the bot. Redelivery also reconciles a successful POST whose response was lost. This uses the existing schema and requires no new migration. A deleted bot comment is recreated if the original opened event is redelivered.

The bot acquires the existing workspace publication guard before the PR advisory lock and rechecks public-team visibility. A privacy change waits before the database write barrier, so it cannot race publication and ordinary issue edits remain available during the GitHub request. It never publishes private-team issue links, even when the repository itself is private. Other authenticated Kuayle access controls remain in effect.

An API/network/permission failure returns a failed webhook response while retaining the PR's existing linkage. Redelivering an opened event for an already stored PR does not reapply its opened status transition. Sanitized comment failures are logged with workspace, repository, PR number and the comment API HTTP status when available. Fix the app permissions or installation access, or wait for the GitHub rate limit/outage to recover, then redeliver the failed `opened` event from the GitHub App's webhook delivery page. GitHub does **not** automatically redeliver failed deliveries. Token acquisition, lock waits and comment requests share an eight-second deadline. Comments use bounded requests and do not blindly retry POSTs; GitHub supplies no atomic idempotency key, so reconciliation depends on the accepted comment becoming visible to its comments API. Do not delete the marker when editing the bot comment.

References: [GitHub issue-comment API and permissions](https://docs.github.com/en/rest/issues/comments#create-an-issue-comment), [failed webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries).
