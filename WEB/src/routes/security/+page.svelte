<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';

	const meta = {
		title: 'Kuayle Security and Self-Hosting Responsibilities',
		description: 'Technical security details for Kuayle self-hosting: authentication, network exposure, credential storage, updates, and vulnerability reporting.',
		canonical: url('/security'),
		modifiedAt: contentModifiedAt('/security')
	};

	const crumbs = breadcrumbsFrom('security', 'Security');

	const sections = [
		{
			heading: 'Shared responsibility',
			body: 'Kuayle supplies application code and a reference Docker Compose deployment. Instance operators control the host, DNS, network rules, secrets, backups, monitoring and update schedule. Review the source and configuration against your security requirements before production use.',
			list: [
				'Kuayle: application code, migrations and reference configuration',
				'Operator: host hardening, firewall, DNS, secrets and access logs',
				'Operator: database, upload and certificate backups',
				'Operator: testing and applying updates'
			]
		},
		{
			heading: 'Authentication',
			body: 'Kuayle hashes passwords with bcrypt. Authentication uses signed access and refresh tokens; refresh-token hashes and expiry times are stored in PostgreSQL. Set a unique JWT_SECRET of at least 32 characters before starting the backend.',
			list: [
				'Access and refresh tokens signed with JWT_SECRET',
				'Passwords hashed with bcrypt before storage',
				'Expiring access and refresh tokens',
				'Role-based access control: Owner, Admin, Member, Guest'
			]
		},
		{
			heading: 'Transport security',
			body: 'The reference deployment exposes Caddy on ports 80 and 443. Caddy obtains and renews TLS certificates when DOMAIN points to the server and the host is reachable. PostgreSQL, Redis and the application containers remain on the internal Compose network unless you alter the configuration.',
			list: [
				'Caddy reverse proxy with automatic Let\'s Encrypt TLS',
				'Public ingress through Caddy on ports 80 and 443',
				'PostgreSQL and Redis have no published host ports in the reference Compose file',
				'TLS depends on correct DNS and network configuration'
			]
		},
		{
			heading: 'Stored data and credentials',
			body: 'Application records are stored in PostgreSQL. Uploads use either a Docker volume or an S3-compatible backend. Per-workspace GitHub App credentials are encrypted with AES-256-GCM using a key derived from the configured secret. Disk-level encryption and backup encryption are operator decisions.',
			list: [
				'PostgreSQL for application records and token hashes',
				'Local volume or S3-compatible storage for uploads',
				'AES-256-GCM for stored GitHub App credentials',
				'No built-in backup scheduler: back up the database, uploads and Caddy state externally'
			]
		},
		{
			heading: 'Reporting vulnerabilities',
			body: 'If you discover a security vulnerability in Kuayle, please report it responsibly. Do not open a public issue. Send the affected version, reproduction steps, impact, and any suggested mitigation to support@bakney.com.',
			list: [
				'Report to: support@bakney.com',
				'Do not disclose publicly until a fix is available',
				'Allow time to investigate and prepare a fix before public disclosure',
				'Include only the minimum sensitive data needed to reproduce the issue'
			]
		},
		{
			heading: 'Updates and dependency checks',
			body: 'The repository runs backend tests, frontend checks and security scanning in GitHub Actions. The self-hosting update script fetches the current branch, rebuilds the backend and frontend containers, recreates the application services and applies database migrations.',
			list: [
				'CI configuration is public under .github/workflows',
				'Update flow rebuilds images and applies migrations',
				'Review release changes and back up data before updating',
				'No published uptime or patch-response SLA'
			]
		}
	];
</script>

<StandalonePage
	{meta}
	heading="Security"
	intro="A factual summary of the controls present in the repository and the security work that remains with each self-hosting operator."
	{sections}
	breadcrumbs={crumbs}
/>
