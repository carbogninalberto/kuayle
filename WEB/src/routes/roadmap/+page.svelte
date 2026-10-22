<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';
	import { useLatestRelease } from '$lib/release.svelte';

	const release = useLatestRelease();

	const meta = {
		title: 'Kuayle Roadmap and Development Status',
		description: 'Kuayle development status, known product gaps, the opt-in Dev Machines subsystem, and where to follow proposed work.',
		canonical: url('/roadmap'),
		modifiedAt: contentModifiedAt('/roadmap')
	};

	const crumbs = breadcrumbsFrom('roadmap', 'Roadmap');

	const sections = $derived([
		{
			heading: 'No invented release dates',
			body: 'Kuayle does not publish committed release dates or promise features against version numbers. Open issues and merged pull requests are the reliable record of proposed and completed work. This page documents the current product boundary rather than a sales roadmap.',
			list: [
				'Open issues describe proposed work',
				'Pull requests and releases describe shipped work',
				'No feature on this page has a guaranteed delivery date',
				'Last reviewed: October 22, 2026'
			]
		},
		{
			heading: `Available in ${release.version}`,
			body: 'The current release includes the issue workflow, workspace roles, public and private teams, custom statuses, comments, sub-issues, relations, labels, issue version history, cycles, projects, saved views, analytics, notifications, public sharing, webhooks, personal access tokens, invite links, workspace transfer and GitHub integration.',
			list: [
				'Private teams with membership-based access (v0.1.19)',
				'Issue title and description version history (v0.1.22)',
				'Cycle and project pages with the full issue list, list/board layouts and an issue picker (v0.1.23)',
				'GitHub pull-request comments linking the matching issue (v0.1.24)',
				'Workspace export/import, invite links and personal access tokens (v0.1.17)',
				'Cycle burndown and velocity charts, project Gantt view and workspace insights',
				'Interface in English, Italian, Simplified and Traditional Chinese'
			]
		},
		{
			heading: 'Known gaps',
			body: 'These capabilities are not available in the current user interface. Their absence is documented so teams can evaluate Kuayle against present requirements rather than future assumptions.',
			list: [
				'Importers from other trackers (workspace transfer only moves data between Kuayle instances)',
				'Enterprise identity: SSO, SAML, SCIM and LDAP',
				'GitLab and chat integrations',
				'Email notifications (notifications are delivered in-app)',
				'Restoring an earlier issue version (history is read-only)',
				'Saved views, view-based public links, workspace export, AI expansion, outgoing webhooks, Dev Machines and live presence in workspaces that use private teams'
			]
		},
		{
			heading: 'Dev Machines are opt-in infrastructure',
			body: 'Releases since v0.1.13 include the opt-in multi-container Dev Machines subsystem. It adds Caddy wildcard ingress, an unprivileged authenticated gateway, a dedicated Docker manager, isolated machine networks, PostgreSQL reconciliation, scoped secrets, activity collection, issue-linked agent runs, and Claude Code, OpenCode, Codex, and custom CLI adapters. Self-hosted operators must explicitly configure the machine domain, TLS, runtime images, GitHub permissions, and capacity.',
			list: [
				'Disabled by default',
				'Requires wildcard DNS and TLS',
				'Trusted self-hosted workspace model'
			]
		},
		{
			heading: 'Follow or propose work',
			body: 'Use the GitHub repository to review active work, report a reproducible bug or propose a feature with a concrete use case.',
			list: [
				'GitHub: github.com/carbogninalberto/kuayle',
				'Bug reports: include version, environment and reproduction steps',
				'Feature requests: describe the workflow and expected outcome',
				'Implementation status: verify merged code and release notes'
			]
		}
	]);
</script>

<StandalonePage
	{meta}
	heading="Roadmap and Development Status"
	intro="What Kuayle implements now, what it does not implement, and where proposed work is tracked. There are no promised dates or invented release milestones."
	{sections}
	breadcrumbs={crumbs}
/>
