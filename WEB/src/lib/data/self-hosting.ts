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
					'Caddy — reverse proxy with automatic Let\'s Encrypt TLS',
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
				list: [
					'docker compose exec backend /app/server migrate up',
					'docker compose exec backend /app/server seed'
				]
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
		description: 'Server prerequisites for running Kuayle: a host with Docker and Docker Compose, a domain name, and HTTPS connectivity.',
		heading: 'Requirements',
		intro: 'Kuayle’s documented deployment uses Docker Compose. Review the host, DNS, network and storage requirements before exposing an instance.',
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
					'Port 80 open — HTTP challenge for Let\'s Encrypt',
					'Port 443 open — HTTPS traffic',
					'Outbound access — image builds, certificate issuance and optional GitHub API calls'
				]
			}
		]
	},

	configuration: {
		slug: 'configuration',
		title: 'Configuration — Self-Hosting Kuayle',
		description: 'Configure Kuayle via environment variables: domain, database credentials, JWT secret, storage backend, GitHub App, and sysadmin settings.',
		heading: 'Configuration',
		intro: 'Kuayle reads deployment settings from `selfhosting/.env`. Start from `.env.example`; the groups below cover the values that must be reviewed before a public deployment.',
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
				body: 'FRONTEND_URL controls public callbacks and allowed origins. SYSADMINS accepts user UUIDs that may access the system update controls.',
				list: [
					'ENVIRONMENT — production or development (default: production)',
					'FRONTEND_URL — the public URL of your instance',
					'SYSADMINS — comma-separated user IDs allowed to trigger updates'
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
		description: 'Update a self-hosted Kuayle instance with the repository script or optional updater sidecar, including image rebuilds and database migrations.',
		heading: 'Updating Kuayle',
		intro: 'The repository update script fetches the current branch, rebuilds the application images, recreates Caddy and the app services, then applies pending migrations. Back up and review changes before running it.',
		sections: [
			{
				heading: 'Manual update via script',
				body: 'Run the script from a clean repository checkout. It uses `git pull --ff-only`, serves a maintenance page, rebuilds the backend and frontend, recreates the application containers and runs migrations.',
				list: [
					'cd kuayle',
					'bash selfhosting/update.sh',
					'The script: pulls latest code → rebuilds images → recreates containers → runs migrations'
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
				body: 'The script leaves the PostgreSQL and Redis services running while it recreates Caddy, the backend and the frontend. Application requests receive a maintenance page during the refresh.',
				list: [
					'Maintenance page served during update',
					'Backend and frontend images rebuilt',
					'Pending migrations applied after container recreation',
					'PostgreSQL and Redis are not recreated by the script'
				]
			}
		]
	},

	storage: {
		slug: 'storage',
		title: 'Storage — Self-Hosting Kuayle',
		description: 'Configure Kuayle storage: local filesystem (default) or S3-compatible backends including AWS S3, Cloudflare R2, MinIO, and SeaweedFS.',
		heading: 'Storage Configuration',
		intro: 'Kuayle stores uploads either in a local Docker volume or through an S3-compatible API. Choose based on your backup, access-control and deployment requirements.',
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
		description: 'Configure Kuayle’s GitHub App manifest flow, repository access, webhook delivery, issue linking, and status-transition rules.',
		heading: 'GitHub App Setup',
		intro: 'Kuayle uses a GitHub App for repository access and webhook delivery. Public instances can use the generated webhook URL; private instances need a relay or tunnel.',
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
			}
		]
	}
};
