/**
 * Content registry for self-hosting guide pages.
 * Each entry maps to /self-hosting/[slug].
 *
 * Content is aligned with the actual README and selfhosting/ configs.
 */
import type { ContentRegistry } from './routes';

export const selfHosting: ContentRegistry = {
	'docker-compose': {
		slug: 'docker-compose',
		title: 'Docker Compose Setup — Self-Hosting Kuayle',
		description:
			'Deploy Kuayle with the reference Docker Compose stack: Caddy, PostgreSQL 17, Redis 7, the Go API, and the SvelteKit frontend.',
		heading: 'Docker Compose Setup',
		intro:
			'The `selfhosting/` directory contains Kuayle’s reference deployment. It runs five services behind Caddy and supports automatic TLS when DNS and network access are configured correctly.',
		sections: [
			{
				heading: 'Architecture overview',
				body: 'The Compose file defines Caddy, PostgreSQL 17, Redis 7, the Go API and the SvelteKit frontend. Caddy terminates HTTPS and sends application and API requests to the appropriate container.',
				list: [
					"Caddy — reverse proxy with automatic Let's Encrypt TLS",
					'PostgreSQL 17 — primary data store',
					'Redis 7 — required configuration value, reserved for future cache and job use',
					'Backend — Go API server on port 8080',
					'Frontend — SvelteKit static app served behind Caddy'
				]
			},
			{
				heading: 'Quick start',
				body: 'Clone the repository, copy the example environment file and replace the placeholder credentials before starting the stack. The launch command builds the local backend and frontend images.',
				list: [
					'git clone https://github.com/carbogninalberto/kuayle.git',
					'cd kuayle/selfhosting',
					'cp .env.example .env',
					'# Edit .env: set DOMAIN, POSTGRES_PASSWORD, JWT_SECRET',
					'docker compose up --build -d'
				]
			},
			{
				heading: 'Initialize the database',
				body: 'After the containers start, apply the database migrations and run the seed command from the `selfhosting` directory.',
				list: ['docker compose exec backend /app/server migrate up', 'docker compose exec backend /app/server seed']
			},
			{
				heading: 'Volumes and data persistence',
				body: 'Named volumes persist PostgreSQL data, local uploads and Caddy state across container recreation. Persistence is not a backup: copy and test restores for each volume before relying on the deployment.',
				list: [
					'pgdata — PostgreSQL data directory',
					'uploads — user-uploaded files and assets',
					'caddy_data / caddy_config — TLS certificates and Caddy state'
				]
			}
		]
	},

	requirements: {
		slug: 'requirements',
		title: 'Requirements — Self-Hosting Kuayle',
		description:
			'Server prerequisites for running Kuayle: a host with Docker and Docker Compose, a domain name, and HTTPS connectivity.',
		heading: 'Requirements',
		intro:
			'Kuayle’s documented deployment uses Docker Compose. Review the host, DNS, network and storage requirements before exposing an instance.',
		sections: [
			{
				heading: 'Server prerequisites',
				body: 'You need a host that can run Docker Engine and the Docker Compose v2 plugin. A Linux server is the conventional production target. Point a domain at the host if you want Caddy to provision public TLS certificates.',
				list: [
					'A host capable of running the current Docker Engine',
					'Docker Compose v2 plugin',
					'A domain name pointing to your server (for HTTPS)'
				]
			},
			{
				heading: 'Capacity planning',
				body: 'Capacity depends on concurrent users, issue volume, uploads, integrations, and backup retention. Start with enough headroom for PostgreSQL, Redis, image builds, and the application containers, then monitor CPU, memory, and disk use before scaling.',
				list: [
					'Allow temporary CPU and disk headroom for container builds and updates',
					'Monitor PostgreSQL, Redis, and application memory after launch',
					'Allocate storage for uploads, database growth, and backups'
				]
			},
			{
				heading: 'Network requirements',
				body: 'For a public HTTPS deployment, point the domain to the host and allow inbound traffic on ports 80 and 443. Private deployments can use a different TLS and routing design, but must adapt the supplied Caddy configuration.',
				list: [
					"Port 80 open — HTTP challenge for Let's Encrypt",
					'Port 443 open — HTTPS traffic',
					'Outbound access — image builds, certificate issuance and optional GitHub API calls'
				]
			}
		]
	},

	configuration: {
		slug: 'configuration',
		title: 'Configuration — Self-Hosting Kuayle',
		description:
			'Configure Kuayle via environment variables: domain, database credentials, JWT secret, storage backend, GitHub App, and sysadmin settings.',
		heading: 'Configuration',
		intro:
			'Kuayle reads deployment settings from `selfhosting/.env`. Start from `.env.example`; the groups below cover the values that must be reviewed before a public deployment.',
		sections: [
			{
				heading: 'Production values',
				body: 'JWT_SECRET is required by the backend. DOMAIN and POSTGRES_PASSWORD have development-oriented defaults in the Compose file and must be replaced for a public deployment.',
				list: [
					'DOMAIN — your public domain (e.g. kuayle.yourcompany.com)',
					'POSTGRES_PASSWORD — strong random password for the database',
					'JWT_SECRET — random string, at least 32 characters, for signing auth tokens'
				]
			},
			{
				heading: 'Database and cache',
				body: 'The Compose service names are used by the default connection settings. Change these values only when you also change the corresponding database or Redis deployment.',
				list: [
					'POSTGRES_USER — database user (default: kuayle)',
					'POSTGRES_DB — database name (default: kuayle)',
					'REDIS_URL — Redis connection string (default: redis://redis:6379)'
				]
			},
			{
				heading: 'Backend settings',
				body: 'FRONTEND_URL must be the exact public browser origin, including scheme and any non-default port; it controls public callbacks, allowed origins, GitHub webhook URLs and pull-request comment links. SYSADMINS accepts user UUIDs that may access the system update controls.',
				list: [
					'ENVIRONMENT — production or development (default: production)',
					'FRONTEND_URL — the exact public origin of your instance',
					'SYSADMINS — comma-separated user IDs allowed to trigger updates',
					'DISABLE_REGISTRATION=true — require a valid invite link to sign up (default: open registration)'
				]
			},
			{
				heading: 'Optional: shared GitHub App',
				body: 'By default each workspace creates its own GitHub App through the manifest flow. Deployments that prefer one preconfigured App can set the GITHUB_APP_* variables; workspaces then only install that App on their account or organization.',
				list: [
					'GITHUB_APP_ID and GITHUB_APP_SLUG',
					'GITHUB_APP_PRIVATE_KEY — base64-encoded PEM',
					'GITHUB_APP_CLIENT_ID and GITHUB_APP_CLIENT_SECRET',
					'GITHUB_APP_WEBHOOK_SECRET'
				]
			},
			{
				heading: 'Optional: system updater',
				body: 'The optional updater sidecar exposes the repository’s update script to authorized sysadmins through Settings → Version. It mounts the Docker socket and repository, so review that trust boundary before enabling it.',
				list: [
					'SYSTEM_UPDATER_URL — internal updater endpoint (default: http://updater:8081)',
					'SYSTEM_UPDATER_TOKEN — strong random token for updater auth',
					'The updater runs the same selfhosting/update.sh script'
				]
			}
		]
	},

	updating: {
		slug: 'updating',
		title: 'Updating Kuayle — Self-Hosting',
		description:
			'Update a self-hosted Kuayle instance with the repository script or optional updater sidecar, including image rebuilds and database migrations.',
		heading: 'Updating Kuayle',
		intro:
			'The repository update script fetches the current branch, rebuilds the application images, stops the backend while pending migrations run, then starts only the new version. Back up PostgreSQL and upload storage and review the release notes before running it.',
		sections: [
			{
				heading: 'Manual update via script',
				body: 'Run the script from a clean repository checkout. It uses `git pull --ff-only`, serves an upgrade page, rebuilds the backend and frontend, stops the backend, applies migrations and then starts the new containers.',
				list: [
					'cd kuayle',
					'bash selfhosting/update.sh',
					'The script: pulls latest code → rebuilds images → stops the backend → runs migrations → starts the new version'
				]
			},
			{
				heading: 'One-click update from the UI',
				body: 'Authorized sysadmins can invoke the same script from Settings → Version after the updater sidecar and token are configured. This grants the sidecar access to the Docker socket and repository checkout.',
				list: [
					'Add your user ID to SYSADMINS in selfhosting/.env',
					'Set SYSTEM_UPDATER_TOKEN to a strong random string',
					'Start the updater sidecar with the updater profile',
					'Use the Update button in Settings → Version'
				]
			},
			{
				heading: 'What happens during an update',
				body: 'Application requests receive an upgrade page while the update runs. No old backend writes to the database during migrations. If a migration fails, the script leaves the backend stopped behind the upgrade page instead of restarting an older version against a partially migrated schema; failures before that point restore the previous containers. A running Dev Machines control plane is rebuilt, paused during migrations and restarted.',
				list: [
					'Upgrade page served during the update',
					'Backend stopped before migrations, so mixed old/new writers cannot run',
					'Failed migrations keep the backend stopped and preserve logs for inspection',
					'PostgreSQL and Redis are not recreated by the script',
					'Disabled optional profiles stay disabled'
				]
			},
			{
				heading: 'Version notices and changelog',
				body: 'The application reads the published release manifest to show a multi-release changelog and the latest version in Settings → Version. Release notes list required migrations and upgrade steps; read them before updating, especially for releases that change authorization or privacy rules.',
				list: [
					'In-app changelog rendered from the release manifest',
					'Settings → Version shows the update command or the Update button',
					'Upgrade notes call out migrations and configuration changes'
				]
			}
		]
	},

	storage: {
		slug: 'storage',
		title: 'Storage — Self-Hosting Kuayle',
		description:
			'Configure Kuayle storage: local filesystem (default) or S3-compatible backends including AWS S3, Cloudflare R2, MinIO, and SeaweedFS.',
		heading: 'Storage Configuration',
		intro:
			'Kuayle stores uploads either in a local Docker volume or through an S3-compatible API. Choose based on your backup, access-control and deployment requirements.',
		sections: [
			{
				heading: 'Local filesystem (default)',
				body: 'By default, the backend writes uploads to `/app/uploads`, persisted by the `uploads` Docker volume. Include that volume in backup and restore procedures.',
				list: [
					'STORAGE_TYPE=local (default)',
					'Files stored in /app/uploads inside the backend container',
					'Persisted via the uploads Docker volume',
					'Simple — no external services needed'
				]
			},
			{
				heading: 'S3-compatible storage',
				body: 'The S3 backend uses the AWS SDK with a configurable endpoint and path-style requests. It is intended for AWS S3 and compatible services, but provider behavior should be tested with your chosen endpoint.',
				list: [
					'STORAGE_TYPE=s3',
					'S3_ENDPOINT — your storage endpoint URL',
					'S3_BUCKET — bucket name for uploads',
					'S3_REGION — e.g. us-east-1',
					'S3_ACCESS_KEY and S3_SECRET_KEY — credentials',
					'S3_PUBLIC=true — generate public URLs for images',
					'S3_CDN_BASE_URL — optional CDN base for public files'
				]
			},
			{
				heading: 'SeaweedFS template',
				body: 'The Compose file includes a commented SeaweedFS service as a starting point. Enabling it also requires bucket, credentials, persistence and backup configuration; the commented block is not a complete production storage setup.',
				list: [
					'SeaweedFS service included in docker-compose.yml (commented out)',
					'Set STORAGE_TYPE=s3 and S3_ENDPOINT=http://seaweedfs:8333',
					'Configure and test the bucket before enabling uploads'
				]
			}
		]
	},

	'github-app': {
		slug: 'github-app',
		title: 'GitHub App Setup — Self-Hosting Kuayle',
		description:
			'Configure Kuayle’s GitHub App manifest flow, repository access, webhook delivery, issue linking, and status-transition rules.',
		heading: 'GitHub App Setup',
		intro:
			'Kuayle uses a GitHub App for repository access and webhook delivery. Public instances can use the generated webhook URL; private instances need a relay or tunnel.',
		sections: [
			{
				heading: 'Publicly reachable instance',
				body: 'Settings → GitHub starts GitHub’s App Manifest flow with callback, permission and webhook values generated from your instance URL. After GitHub returns the manifest code, Kuayle stores the App credentials encrypted.',
				list: [
					'Go to Settings → GitHub in your workspace',
					'Click "Set up GitHub App"',
					'Authorize on GitHub — pre-filled manifest',
					'Webhook URL generated from the configured instance URL',
					'Click "Install on GitHub" to grant repo access'
				]
			},
			{
				heading: 'Private network support',
				body: 'GitHub must reach `/api/github/webhook` to deliver branch, push and pull-request events. For private instances, run a smee.io relay or expose the webhook endpoint through a controlled tunnel such as cloudflared or ngrok.',
				list: [
					'smee.io — third-party webhook relay',
					'cloudflared or ngrok — public tunnel to the webhook endpoint',
					'Without webhook delivery, GitHub events and status transitions are not processed'
				]
			},
			{
				heading: 'What the integration does',
				body: 'Kuayle scans branch names, pull-request titles and bodies, and commit messages for issue identifiers. Supported webhook events can record activity and move matched issues according to enabled automation rules.',
				list: [
					'Auto-links PRs, branches, and commits via issue key matching',
					'Configurable auto-transitions based on GitHub events',
					'Webhook signature verification before processing',
					'Linked activity displayed in the issue detail view'
				]
			},
			{
				heading: 'Pull-request bot comments',
				body: 'On pull_request.opened, the App comments with a link to the matching public-team issue. The App needs Pull requests: Read and write and the Pull request webhook subscription, and the repository must be linked and active. Set FRONTEND_URL to the externally reachable Kuayle URL so links resolve. GitHub does not redeliver failed webhooks automatically: after fixing a permission or API failure, redeliver the failed opened event from the App’s webhook delivery page.',
				list: [
					'Permissions: Pull requests read/write, Contents read/write, Metadata read, Issues read',
					'Events: Pull request, Push, Create',
					'No historical backfill; edits and reopens do not add comments',
					'Private-team issues are never linked'
				]
			}
		]
	},

	'dev-machines': {
		slug: 'dev-machines',
		title: 'Dev Machines Setup — Self-Hosting Kuayle',
		description:
			'Enable the opt-in Dev Machines subsystem: a separate wildcard domain, wildcard TLS, encrypted secrets, runtime images, and the gateway/manager control plane.',
		heading: 'Dev Machines Setup',
		intro: `Dev Machines are an opt-in subsystem, included in releases since v0.1.13, and remain disabled in the default five-service deployment. Enabling them adds a machine gateway, a Docker manager and per-machine container runtimes. Read TECHNICAL.md in the repository before enabling the subsystem.`,
		sections: [
			{
				heading: 'Prerequisites',
				body: 'Machine workloads run on a separate registrable domain — not a sibling subdomain of the main application — with wildcard DNS pointing to the host. Production wildcard TLS requires a custom Caddy build with a DNS-01 module or an operator-provided wildcard certificate; stock Caddy cannot issue wildcard certificates over HTTP-01. The Docker data root must use XFS project quotas.',
				list: [
					'DEV_MACHINE_DOMAIN on a separate registrable domain',
					'Wildcard DNS for *.DEV_MACHINE_DOMAIN',
					'Wildcard TLS via DNS-01 Caddy build or an imported certificate',
					'Docker data root on XFS mounted with pquota or prjquota',
					'A GitHub App with scoped write permissions for agent push/PR workflows'
				]
			},
			{
				heading: 'Required configuration',
				body: 'When DEV_MACHINES_ENABLED=true, the backend validates the machine domain, frontend URL, encryption key, ingestion URL and related runtime settings. The Compose provisioning prerequisite separately requires the gateway database password. FRONTEND_URL must be the exact public Kuayle origin; the gateway requires the native terminal WebSocket Origin header to match it exactly.',
				list: [
					'DEV_MACHINES_ENABLED=true',
					'DEV_MACHINE_ENCRYPTION_KEY — independent random value, at least 32 characters',
					'DEV_MACHINE_INGEST_URL — public HTTPS collector ingestion URL',
					'DEV_MACHINE_GATEWAY_DB_PASSWORD — independent credential for the restricted gateway role',
					'FRONTEND_URL — exact public origin, scheme included'
				]
			},
			{
				heading: 'Build and start',
				body: 'Runtime images are built locally, migrations applied, and the optional control plane started through Compose profiles.',
				list: [
					'docker compose --profile dev-machine-images build',
					'docker compose exec backend /app/server migrate up',
					'docker compose --profile dev-machines run --rm machine-gateway-db-provision',
					'docker compose --profile dev-machines up -d'
				]
			},
			{
				heading: 'Security model',
				body: 'The Machine Manager is the only Dev Machines runtime component with Docker socket access; the gateway is unprivileged. The separately optional system updater also mounts the socket when enabled. Machine services publish no host ports and run on isolated per-machine networks with an egress policy proxy. Docker hardening reduces risk but is not hostile multi-tenant isolation — the subsystem targets trusted self-hosted workspaces.',
				list: [
					'One-time launch tickets and host-restricted machine sessions',
					'AES-256-GCM encrypted secrets delivered through tmpfs',
					'Egress proxy blocks private address ranges; optional domain allowlists',
					'Workspace policies control concurrency, runtime, providers and repositories'
				]
			},
			{
				heading: 'Private teams',
				body: 'Dev Machines are disabled in workspaces that use private teams, and the restriction remains if those teams are later made public or deleted. Remove existing development machines, environments and development defaults before enabling team privacy.',
				list: [
					'Tear down machines and environments before enabling private teams',
					'The restriction is workspace-wide and persistent'
				]
			}
		]
	},

	'workspace-transfer': {
		slug: 'workspace-transfer',
		title: 'Workspace Transfer — Self-Hosting Kuayle',
		description:
			'Export a Kuayle workspace as a portable .kuayle.zip archive and import it into another instance, with ID remapping, asset copies and explicit secret exclusions.',
		heading: 'Workspace Transfer',
		intro:
			'Workspace owners and admins can export one workspace as a versioned `.kuayle.zip` archive and import it on another Kuayle instance. Import always creates a separate workspace and never overwrites an existing one. Transfer is not a substitute for operator backups.',
		sections: [
			{
				heading: 'Export',
				body: 'Download the archive from Settings → General → Workspace transfer. It contains a logical manifest, the workspace data and the uploaded asset bytes. Workspace export is disabled in workspaces that use private teams, and imports reject archives containing private teams.',
				list: [
					'Teams, statuses, labels, projects, cycles and issues with relationships and history',
					'Templates, views, favorites, shared links and workspace notifications',
					'Integration metadata, AI prompts and safe Dev Machine policy settings',
					'Uploaded files and images'
				]
			},
			{
				heading: 'Import',
				body: 'A signed-in user without a workspace can choose Import workspace during setup; an owner or admin can also start an import from the transfer settings. Database and asset IDs are regenerated, and references, including protected asset URLs, are remapped.',
				list: [
					'Users are matched to existing accounts by normalized email',
					'The importing user becomes the owner of the new workspace',
					'Other referenced users must register on the target instance first',
					'The preview lists missing emails without creating partial data'
				]
			},
			{
				heading: 'What is never exported',
				body: 'Credentials and runtime state stay on the source instance. Imported webhooks, shared links, GitHub repositories and automation, and Dev Machine policy are disabled or rotated where appropriate, so integrations must be reconfigured on the target.',
				list: [
					'Password hashes, refresh tokens and personal preferences',
					'Webhook secrets, GitHub App credentials and access tokens, AI API keys',
					'Dev Machine credentials, active machines, volumes, networks and environment images',
					'Logs, sessions and runtime artifacts'
				]
			},
			{
				heading: 'Transfer is not a backup',
				body: 'The archive moves one logical workspace. Disaster recovery and whole-instance migration still require PostgreSQL backups, object-storage or upload-volume backups, deployment configuration and secrets, and any required Docker volumes or Development Environment images.',
				list: [
					'Back up PostgreSQL and uploads on a schedule',
					'Store deployment secrets separately',
					'Test restores, not only exports'
				]
			}
		]
	}
};
