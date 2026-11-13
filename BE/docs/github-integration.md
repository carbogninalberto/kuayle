# GitHub Integration

Connects GitHub repositories to Kuayle workspaces, automatically linking pull requests, branches, and commits to issues and transitioning issue status based on GitHub activity.

GitLab, Gitea and Forgejo use the same linking and automation rules through a
workspace connection instead of a GitHub App; see
[Forge integrations](forge-integrations.md).

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
   | Checks | Read |
   | Commit statuses | Read |
   | Actions | Read |
   | Members (organization permission) | Read |

   **Events to subscribe to:**
   - Pull request
   - Pull request review
   - Check suite
   - Issue comment (mention-triggered reviews)
   - Push
   - Create (branch/tag)

   **Callback URL:** `https://your-domain.com/{workspace-slug}/settings/github`
   **Setup URL:** Same as callback URL (enable "Redirect on update")
   **Request user authorization (OAuth) during installation:** enabled. Every
   workspace shares this App, so Kuayle binds an installation only after the
   installing user's OAuth code proves, via `GET /user/installations`, that the
   user can access it. Without it, an `installation_id` in the redirect could
   name another organization's installation.

   **Organization permission — Members: Read-only.** GitHub lists an
   installation to every user who can reach one repository it covers, so
   Kuayle also requires the installing user to be the account the App is
   installed on, or an owner (`role: admin`, active) of that organization,
   read from `GET /user/memberships/orgs/{org}`. That request needs this
   permission. Without it, installations on personal accounts still connect,
   but organization installations are refused with
   `GITHUB_INSTALLATION_UNVERIFIED`. After adding the permission to an existing
   App, each organization must accept the new permission before one of its
   owners can connect it. An organization member who is not an owner cannot
   connect the installation, even when allowed to install the App on
   repositories they administer; an owner has to do it.
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

3. Each workspace user clicks "Install on GitHub" in Settings > GitHub, selects their org/account, and chooses which repos to grant access to. The user must be that account, or an owner of that organization.

### Self-Hosted Mode (Per-Workspace App)

When no `GITHUB_APP_ID` is set, each workspace creates its own GitHub App via the [manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest). The user clicks "Set up" and GitHub creates the app automatically — no manual configuration needed.

This mode stores app credentials (encrypted with AES-256-GCM) per-workspace in the database.

## Webhook Configuration

Webhooks are how GitHub notifies Kuayle about events (PRs, pushes, branches).

- **SaaS mode**: Configure the webhook URL when creating the GitHub App (see above).
- **Self-hosted mode**: The webhook URL is auto-derived from `FRONTEND_URL`. For localhost/dev, webhooks are disabled by default.
- **Dev override**: Set `GITHUB_WEBHOOK_URL` to a [smee.io](https://smee.io) URL to receive webhooks during local development.

Webhook payloads are verified using HMAC-SHA256 signatures before processing.

### Replay protection

A signature stays valid forever, so each delivery is processed at most once.
After the signature check the handler requires GitHub's `X-GitHub-Delivery`
header (a signed request without a usable one gets `400`) and records it in
`github_webhook_deliveries` (migration 135), keyed by the installation ID from
the signed payload and the delivery ID:

- A delivery that was already processed, or is being processed by another
  request, is answered with `200` and not processed again.
- The ID is claimed before processing and released when processing fails, so a
  failed delivery can be redelivered from GitHub (a redelivery reuses the
  delivery ID). A claim left by a request that died is taken over after ten
  minutes.
- When the marker cannot be recorded the delivery fails with `500` instead of
  running unprotected; redeliver it from GitHub.
- Markers are removed after seven days by an hourly cleanup. GitHub signs no
  timestamp, so a delivery captured earlier than that can be replayed once
  more. Rotating the webhook secret invalidates every captured delivery.

Redelivering a delivery that **succeeded** therefore does nothing for seven
days. Use GitHub's redelivery for failed deliveries only.
A relay or tunnel in front of the endpoint must forward `X-GitHub-Delivery`
unchanged (smee-client and plain reverse proxies do).

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
| `GITHUB_APP_CLIENT_ID` | SaaS mode | GitHub App client ID; required to verify installations |
| `GITHUB_APP_CLIENT_SECRET` | SaaS mode | GitHub App client secret; required to verify installations |
| `GITHUB_APP_WEBHOOK_SECRET` | No | Webhook signature secret |
| `GITHUB_WEBHOOK_URL` | No | Override webhook URL (e.g., smee.io for dev) |

## PR bot comments (KUA-60)

The existing GitHub App posts `Linked Kuayle issue: [KUA-60](https://your-domain.com/your-workspace/issue/KUA-60)` on `pull_request.opened`. It does not edit the PR description, include the issue title/description, or depend on the PR author's account type. Issue access still requires the normal Kuayle authorization. There is no historical backfill, and edits/reopens do not create new comments.

Set `FRONTEND_URL` to the externally reachable Kuayle URL. Both the shared app and per-workspace manifest app use their existing installation credentials. **Pull requests: Read and write** permits PR comments through GitHub's issue-comments endpoint; **Issues: write** is not required. Existing installations need no permission change if they already grant the documented permissions. The app must subscribe to **Pull request**, and the repository must be linked and active in the workspace.

Duplicate deliveries are serialized with a PostgreSQL advisory lock keyed by repository and PR number, including across API replicas. Before posting, the bot scans all comment pages for its workspace marker **and its GitHub App ID and bot author**, so a copied marker from another author does not suppress the bot. Redelivery also reconciles a successful POST whose response was lost. This uses the existing schema and requires no new migration. A successfully processed `opened` delivery is not processed again when redelivered (see [replay protection](#replay-protection)), so a deleted bot comment is not recreated.

The bot acquires the existing workspace publication guard before the PR advisory lock and rechecks public-team visibility. A privacy change waits before the database write barrier, so it cannot race publication and ordinary issue edits remain available during the GitHub request. It never publishes private-team issue links, even when the repository itself is private. Other authenticated Kuayle access controls remain in effect.

An API/network/permission failure returns a failed webhook response while retaining the PR's existing linkage. Redelivering an opened event for an already stored PR does not reapply its opened status transition. Sanitized comment failures are logged with workspace, repository, PR number and the comment API HTTP status when available. Fix the app permissions or installation access, or wait for the GitHub rate limit/outage to recover, then redeliver the failed `opened` event from the GitHub App's webhook delivery page. GitHub does **not** automatically redeliver failed deliveries. Token acquisition, lock waits and comment requests share an eight-second deadline. Comments use bounded requests and do not blindly retry POSTs; GitHub supplies no atomic idempotency key, so reconciliation depends on the accepted comment becoming visible to its comments API. Do not delete the marker when editing the bot comment.

References: [GitHub issue-comment API and permissions](https://docs.github.com/en/rest/issues/comments#create-an-issue-comment), [failed webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries).

## Agent task CI and review feedback

Coding-agent pull requests use **Checks: Read** with the **Check suite** and
**Pull request review** events. Manifest-created apps request them automatically.
Existing apps and the shared app must add them in the GitHub App settings, and
each installation owner must accept the updated permissions. Without them, tasks
still publish and complete on merge but never receive CI or review signals. See
the [agent task contract](agent-tasks.md#ci-and-review-feedback).

### Who can steer an agent from GitHub

A `changes_requested` review that queues a follow-up, and a mention that starts
a review, are only accepted from someone who can push to the linked repository.
The webhook's author association is not enough, because it does not say what
the author may do:

| Association | Decision |
|---|---|
| `OWNER` | accepted (owns the repository or its organization) |
| `MEMBER`, `COLLABORATOR` | Kuayle asks GitHub (`GET /repos/{owner}/{repo}/collaborators/{username}/permission`, with the installation token, for the repository Kuayle linked) and accepts `admin`, `maintain` or `write`. Organization members and outside collaborators limited to read or triage are refused |
| anything else | refused without a lookup |

The lookup needs **Metadata: Read**, which the app already has, so no
permission change is required. Nothing is cached: a revoked permission applies
to the next event. When the lookup fails, nothing is started or recorded and
the delivery fails, so it can be redelivered from GitHub once the API works
again. Reviews with another outcome are recorded without a lookup, since they
cannot queue work.

The assistant's `get_pull_request_checks` also reads legacy commit statuses and
needs **Commit statuses: Read**; apps created before it must add that permission
the same way (see [assistant action policies](conversations.md#assistant-action-policies)).

The assistant's release tools need **Contents: Read and write** (already
requested) and, to follow release builds with `get_workflow_run`, **Actions:
Read**. Apps created before them must add **Actions: Read** in the GitHub App
settings and each installation owner must accept it; until then the setup check
lists it as missing and `get_workflow_run` names it instead of calling GitHub
(see [release tools](conversations.md#release-tools)).

### GitHub App setup check

GitHub has no API for an app to change its own events or permissions, so Kuayle
detects a stale app instead. Workspace owners see a **GitHub App setup** card on
Settings → GitHub once the app is installed. It calls
`GET /api/workspaces/{slug}/github/app-setup` (`workspace:manage`), which signs an
app JWT and compares `GET /app` (what the app's owner configured) and
`GET /app/installations/{id}` (what the installation accepted) against the
manifest's permission map and event list, the single source of these
requirements in `BE/internal/service/github_app_setup.go`. A higher level
satisfies a lower one (`write` covers `read`). The response `state` is:

| State | Meaning and fix |
|---|---|
| `ok` | Agents receive CI results and review requests. |
| `app_missing` | The app lacks the listed events/permissions. **Fix on GitHub** opens `github.com/settings/apps/{slug}/permissions` (or `github.com/organizations/{owner}/settings/apps/{slug}/permissions` for an organization-owned app). Tick the listed items, click **Save changes** and confirm **Update permissions** in GitHub's dialog; the change only takes effect once the installation accepts it. With the shared app (`managed: true`) the card asks the workspace to contact the Kuayle operator instead. |
| `installation_pending` | The app requests them but the installation has not accepted. **Accept on GitHub** opens the installation's `html_url`; click **Review request**, then **Accept new permissions**. Events that need no new permission may apply without acceptance, which the next check reflects. |
| `installation_suspended` / `installation_missing` | The installation is suspended, or GitHub no longer has it (uninstalled); GitHub sends no events. Unsuspend it, or disconnect and install again. |
| `not_configured` / `not_installed` | No app or installation for the workspace. |
| `unavailable` | GitHub could not be reached or rejected the request; details are only logged. |

The card re-checks on **Check again** and whenever the window regains focus
while the state is not `ok`.

## Kuayle bot identity and abilities (KUA-96)

Settings > GitHub > **Kuayle bot** customizes the workspace's GitHub bot.
Members read the settings (`GET /api/workspaces/:slug/github/bot`, session only,
guests refused); `workspace:manage` changes them (`PATCH /github/bot`,
`POST /github/bot/avatar`). They live in `workspace_github_bot_settings`
(migration 119); no row means the defaults below.

**Identity.** A display name (1–64 characters, no line breaks or
`` < > @ ` [ ] \ * _ ~ | # ``) and a profile picture, either uploaded (multipart
`file`) or linked by an `https://` URL. Both must be PNG, JPEG, WebP or GIF
content of at most 2 MB, detected from the bytes; a linked image is downloaded
once through a public-address-only client without redirects before it is saved.
GitHub always shows the GitHub App as the comment author and offers no API to
change that per comment, so Kuayle starts its comments (issue link and agent
review findings) and the descriptions of pull requests agent tasks open (KUA-112;
the machine manager reads the settings with the publication) with the configured
picture and **name**. Default settings add nothing, so existing comments are
unchanged. An uploaded picture is served publicly at
`/api/github-bot/:workspaceId/avatar/:file` (only the file the settings
reference) so GitHub can load it; it is linked only when `FRONTEND_URL` is
`https://`. Replacing or removing a picture deletes the upload.

**App logo.** The avatar GitHub shows as the author is the GitHub App's own
logo. Neither the app manifest nor the API accepts a logo, so it is uploaded by
hand under Display information in the app's settings. Settings > GitHub offers
the Kuayle logo (`/icons/icon-512.png`) before the app is created and, for a
workspace-owned installed app, an **Update logo on GitHub** link to that page.
Operator-managed apps are changed by the operator.
New apps get a default description from the manifest (`GitHubAppDescription`);
for an existing app, paste it into the same Display information section.

**Abilities.** Each is a toggle refused where the GitHub write happens, so a
turned-off ability also refuses actions someone approves in Kuayle:

| Key | Default | Gates |
|---|---|---|
| `link_issues` | on | the issue link comment on opened pull requests |
| `task_feedback` | on | `check_suite`/`pull_request_review` signals to agent tasks |
| `review_comments` | on | posting requested agent review findings |
| `pull_request_actions` | on | reply/resolve review threads, update branch, mark ready |
| `merge_pull_requests` | on | merging a task's pull request |
| `releases` | on | creating GitHub releases |
| `mention_reviews` | **off** | reviews started by a mention (below) |

Status transitions keep their own per-event switches under Automations.
Opening pull requests from agent tasks is part of task publication, not a bot
ability.

**Mention-triggered reviews.** With `mention_reviews` on, a new
`issue_comment` on a pull request of a linked, active repository that mentions
the app (`@<app-slug>` or `@<app-slug>[bot]`) and contains the word "review"
starts an agent review. The commenter must be able to push to the repository
(see [who can steer an agent](#who-can-steer-an-agent-from-github)); bots, edits
and issue comments are ignored. The pull request must
be one a Kuayle implementing task published and that awaits review. The review
goes through the same `RequestReview` path as an approved `request_agent_review`
but without an approval card (enabling the ability is the approval): it acts for
the task's creator under their current membership, issue visibility and coding
agent policy, is pinned to the published commit, posts its findings back on the
pull request, and uses the configured harness and model or, when empty, chat's
defaults (the implementing task's harness and the default model server's model).
The request ID derives from the comment, so a redelivered webhook converges on
one review. Problems a retry cannot fix (no task, lost access, harness not
allowed) are logged and acknowledged. Apps must subscribe to **Issue comment**;
the app setup check reports it when missing.
