# Slack integration

A Kuayle workspace owner or admin can connect one Slack workspace and choose a bot channel for each team or project in **Settings → Slack**. Each Slack workspace can belong to one Kuayle workspace on the instance. This release supports installation, channel mappings, and removal. Issue creation, thread sync, mentions, unfurls, and notifications are planned.

## Configure the Slack app

1. Create a Slack app with a bot user. Under **OAuth & Permissions**, add the bot scopes `channels:read` and `groups:read`. Leave token rotation off. Kuayle supports workspace installations with long-lived bot tokens.
2. Register the exact redirect URL `https://<kuayle-host>/integrations/slack/callback`. Set `FRONTEND_URL` to that public HTTPS origin. The UI and its API proxy must both be reachable there.
3. Set `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`, and `SLACK_SIGNING_SECRET` from the app's **Basic Information** in the API environment. Restart the API after changing these settings. Keep the instance's `JWT_SECRET` stable because it also protects stored integration tokens.
4. Apply the database migrations. Under **Event Subscriptions**, enable events and enter `https://<kuayle-host>/api/slack/events`. Slack verifies the endpoint with a signed challenge. Subscribe to the bot event `app_uninstalled` and save.
5. To install the same app in other Slack workspaces, enable distribution in **Manage Distribution**. Slack's own workspace approval policy still applies. [Slack OAuth installation guide](https://docs.slack.dev/authentication/installing-with-oauth/)

## Connect and select channels

1. Open **Settings → Slack → Connect**, approve the Slack install, and return in the same browser tab. Kuayle binds the Slack installer ID from OAuth to the signed-in Kuayle user. Each authorization expires after ten minutes and can be used once.
2. Invite the bot to a public or private Slack channel, then reload the settings page. Select a team or project under **Scope**, choose a **Channel**, and save. Select **None** to clear a mapping. The API checks bot membership again on save. [Slack channel membership](https://docs.slack.dev/reference/methods/users.conversations/)
3. Choose **Disconnect** to uninstall the Slack app from that workspace and remove its installation, mappings, identities, and thread links. Removing the app inside Slack also triggers cleanup. [Slack uninstall behavior](https://docs.slack.dev/reference/methods/apps.uninstall/)

## Event handling and storage

The public endpoint authenticates the original request body with Slack's v0 HMAC-SHA256 signature and a five-minute timestamp window. A verified uninstall is recorded by event ID for the matching installation before the API acknowledges it. The API's background worker checks the current token with Slack. A transient failure leaves the receipt available for retry after one minute. A delayed uninstall for a still-authorized replacement installation leaves it connected. Other event types are acknowledged without storing their payloads. [Slack request verification](https://docs.slack.dev/authentication/verifying-requests-from-slack/) · [Slack uninstall events](https://docs.slack.dev/reference/events/app_uninstalled/)

Migration 201 stores bot tokens with the same AES-256-GCM helper as the forge integrations and a Slack-specific key. Workspace foreign keys constrain channel bindings, user identities and thread links. Only an interactive owner or admin can use the settings API. Rolling back migration 201 deletes local Slack integration data. Disconnect first to revoke the remote installation.
