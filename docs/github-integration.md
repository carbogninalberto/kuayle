# GitHub integration

This is the operator setup guide. For the shared-App mode, required permissions, webhook verification, PR bot comments and the setup check, see the [backend reference](../BE/docs/github-integration.md).

Kuayle connects to GitHub through a **GitHub App**. By default, each workspace uses GitHub's App Manifest flow and stores the returned credentials encrypted. Deployments with shared App credentials can let workspaces install a preconfigured App instead (see `.env.example` for the `GITHUB_APP_*` variables).

## What it does

- **Links development activity** — mention `ENG-123` in a branch name, PR title/body, or commit message (case-insensitive)
- **Auto-transitions** — branch created → In Progress, PR opened → In Review, PR merged → Done (configurable)
- **Activity feed** — linked PRs, branches, and commits on the matching issue detail page

## Publicly reachable instance

If GitHub can reach your Kuayle instance (for example `https://kuayle.yourcompany.com`), the manifest flow can populate the callback and webhook URLs:

1. Go to **Settings → GitHub** in your workspace
2. Click **Set up GitHub App** — redirects to GitHub with a pre-filled form
3. Click **Create GitHub App** on GitHub
4. Redirected back — credentials saved automatically, webhook URL configured
5. Click **Install on GitHub** to grant access to your repos
6. Select which repositories to link

GitHub events appear after the App is installed on repositories, webhook delivery succeeds, and an issue identifier is present in supported activity.

## Private network / no public domain

If your instance runs on a private network (for example behind a VPN), GitHub cannot send webhooks directly. Use a relay or expose only the webhook endpoint through a controlled tunnel.

**Option A: Webhook proxy with smee.io**

[smee.io](https://smee.io) is a free webhook relay that forwards GitHub events to your private instance.

1. Go to [smee.io/new](https://smee.io/new) and copy your channel URL (e.g. `https://smee.io/abc123`)
2. Run the proxy on your server:
   ```sh
   npx smee-client --url https://smee.io/abc123 --target http://localhost:8080/api/github/webhook
   ```
3. Set up the GitHub App from **Settings → GitHub** (same steps as above)
4. After the app is created, go to its settings on GitHub:
   **GitHub → Settings → Developer settings → GitHub Apps → your app → General**
5. Set **Webhook URL** to your smee channel URL (`https://smee.io/abc123`)
6. Check **Active** and save

The smee client must stay running to receive events. You can run it as a systemd service:

```ini
# /etc/systemd/system/smee-kuayle.service
[Unit]
Description=Smee webhook proxy for Kuayle
After=network.target

[Service]
ExecStart=/usr/bin/npx smee-client --url https://smee.io/abc123 --target http://localhost:8080/api/github/webhook
Restart=always
User=kuayle

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now smee-kuayle
```

**Option B: Reverse tunnel**

Use a reverse tunnel to expose just the webhook endpoint. With [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/):

```sh
cloudflared tunnel --url http://localhost:8080
```

Or with [ngrok](https://ngrok.com):

```sh
ngrok http 8080
```

Then set the generated public URL as the webhook URL in your GitHub App settings (append `/api/github/webhook`).

## Local development

For local development, the GitHub App is created without a webhook URL (since localhost isn't reachable). To receive webhook events during development:

1. Set up the GitHub App from Settings → GitHub (works on localhost)
2. Start a smee proxy:
   ```sh
   npx smee-client --url https://smee.io/your-channel --target http://localhost:8080/api/github/webhook
   ```
3. Update the webhook URL in your GitHub App settings to your smee channel URL
4. Events will now flow through to your local instance
