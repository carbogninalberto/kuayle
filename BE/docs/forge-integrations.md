# Forge integrations: GitLab, Gitea and Forgejo

Kuayle links issues to merge/pull requests, branches and commits from GitHub,
GitLab, Gitea and Forgejo. The GitHub integration keeps its GitHub App setup;
the other forges use an admin-configured connection with a webhook secret and an
optional access token.

## How linking works

Issue identifiers are read from branch names, commit messages and merge request
titles and descriptions, using the same rules as GitHub. Exactly one existing
issue must be named, otherwise the item is stored without a link. Words that
look like identifiers but do not exist in the workspace (`SHA-256`, `node-18`)
are not links.

Status automation reuses the workspace's `github_auto_transitions` rules:

| Event key | Default target | When it runs |
|---|---|---|
| `branch_created` | In Progress | a branch is created |
| `pr_opened` | In Review | a merge request opens (or becomes ready) for the first time |
| `pr_merged` | Done | a merge request merges |

The rules are shared with GitHub and are edited in **Settings → GitHub**. The
first forge connection seeds the three defaults only when they are missing; it
never overwrites rules an admin changed.

Unknown or unlinked repositories, inactive repositories and unsupported events
are acknowledged with `200` and change nothing. Every delivery is processed at
most once per connection.

## Configure a connection

An owner (the API requires `workspace:manage`) opens **Settings → Forge
integrations**, chooses the provider, and enters:

- **Base URL**: `https://gitlab.com` or the self-managed instance root, and the
  Gitea/Forgejo instance root. A trailing slash is removed. https is required
  unless the operator set `FORGE_ALLOW_PRIVATE_NETWORK=true`, which also allows
  http and private/loopback addresses.
- **Webhook secret**: the shared secret configured on the forge.
- **Access token** (optional): used only to comment the linked Kuayle issue on
  a newly opened merge request.

The page shows the webhook URL for that connection:

```
POST https://<kuayle-host>/api/forges/webhook/<connection-id>
```

The secret is looked up by the connection ID before any payload field is read.
A bad secret or signature answers `401`; a missing delivery ID answers `400`.

Then link each repository by its external ID and full name. The external ID is
the numeric project ID GitLab reports in `project.id` and the numeric
repository ID Gitea/Forgejo report in `repository.id`; the full name is
`group/project` or `owner/repository`.

## GitLab

1. In the project (or group), open **Settings → Webhooks**.
2. URL: the connection's webhook URL.
3. Secret token: the same webhook secret.
4. Enable **Merge request events** and **Push events**.
5. Create a project/group access token with `api` scope (or a personal access
   token) and paste it as the connection's access token if issue comments are
   wanted. The comment endpoint is
   `POST /api/v4/projects/:id/merge_requests/:iid/notes` with `PRIVATE-TOKEN`.

GitLab has no branch-created event. Kuayle treats a push whose `before` commit
is all zeroes as the creation of that branch.

## Gitea and Forgejo

1. In the repository, open **Settings → Webhooks → Add webhook → Gitea**.
2. URL: the connection's webhook URL; content type `application/json`.
3. Secret: the same webhook secret.
4. Enable **Push**, **Create** and **Pull request** events.
5. Create an access token with repository write access and paste it as the
   connection's access token if issue comments are wanted. The comment endpoint
   is `POST /api/v1/repos/:owner/:repo/issues/:number/comments` with
   `Authorization: token …`.

Gitea and Forgejo share one implementation; the connection's provider decides
which header names are preferred (`X-Forgejo-*` before `X-Gitea-*`). The
signature is the hex HMAC-SHA256 of the raw body.

## Network safety

Outbound forge API calls dial through `validate.PublicAddressControl` and do not
follow redirects. With the default `FORGE_ALLOW_PRIVATE_NETWORK=false`, a base
URL that resolves to a private, loopback, link-local or metadata address is
refused. Operators who run a forge on their LAN can opt in:

```
FORGE_ALLOW_PRIVATE_NETWORK=true
```

This trusts every workspace owner with the API server's internal network
access, like `AI_ALLOW_PRIVATE_NETWORK` does for AI providers.

## Extension points

`domain.ForgePublisher` declares opening and merging a merge/pull request. It is
intentionally unimplemented: agent publishing and merging on non-GitHub forges
is a follow-up phase. Bitbucket is reserved as `domain.ForgeBitbucket` and can
add another `domain.ForgeWebhook`/`ForgeCommenter`/`ForgePublisher`
implementation without changing the webhook service.
