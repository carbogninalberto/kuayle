# Forge integrations

This is the operator setup guide for GitLab, Gitea and Forgejo. For the shared
linking rules, security model and extension points, see the
[backend reference](../BE/docs/forge-integrations.md).

Workspace owners add a connection in **Settings → Forge integrations** with the
forge base URL, a webhook secret and an optional access token. Kuayle then shows
the connection's webhook URL:

```
POST https://<kuayle-host>/api/forges/webhook/<connection-id>
```

The connection ID selects the secret, so a request with an unknown ID, a bad
token or a bad signature is rejected with `401` before any payload field is
trusted. Set `FORGE_WEBHOOK_BASE_URL` when the API origin differs from
`FRONTEND_URL`; without it the URL follows `FRONTEND_URL` (with `:5173` mapped
to `:8080` in development). Link each repository by its numeric external ID and
full name (`group/project` or `owner/repository`).

Deliveries are processed once per connection. A GitLab delivery without
`X-Gitlab-Event-UUID` or `Idempotency-Key` is identified by the SHA-256 of its
body, so an identical redelivery is ignored rather than rejected.

## GitLab

- **Settings → Webhooks** in the project or group.
- URL: the connection's webhook URL; secret: the connection's webhook secret.
- Events: **Merge request events** and **Push events**.
- Token: a project/group access token with `api` scope (or a personal access
  token) for the linked-issue comment.

## Gitea and Forgejo

- **Settings → Webhooks → Add webhook → Gitea**, content type JSON.
- URL: the connection's webhook URL; secret: the connection's webhook secret.
- Events: **Push**, **Create** and **Pull request**.
- Token: repository write access for the linked-issue comment.

## Automation and network

Forge connections reuse the workspace's GitHub automation rules
(`branch_created`, `pr_opened`, `pr_merged`); edit them in **Settings →
GitHub**. The first connection seeds the defaults only when they are missing.

Outbound calls refuse private addresses and redirects by default. For a forge on
a private network, set `FORGE_ALLOW_PRIVATE_NETWORK=true` (http is accepted only
with this opt-in). Agent publishing and merging on non-GitHub forges, and
Bitbucket, are not implemented yet.
