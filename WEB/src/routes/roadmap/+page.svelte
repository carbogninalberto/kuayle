<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';
	import { useLatestRelease } from '$lib/release.svelte';

	const release = useLatestRelease();

	const meta = {
		title: 'Kuayle Roadmap and Development Status',
		description: 'Kuayle development status, known product gaps, the implemented Dev Machines subsystem, and where to follow proposed work.',
		canonical: url('/roadmap'),
		modifiedAt: '2026-07-11'
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
				'Last reviewed: July 11, 2026'
			]
		},
		{
			heading: `Available in ${release.version}`,
			body: 'The current release includes the issue workflow, workspace roles, teams, custom statuses, comments, sub-issues, relations, labels, cycles, projects, saved views, analytics, notifications, public sharing, webhooks and GitHub integration.',
			list: [
				'Cycle burndown and velocity charts',
				'Project issue list and Gantt view',
				'Workspace insights, lifecycle metrics and burn-up trends',
				'GitHub activity linking and configurable status transitions',
				'WebSocket updates and issue presence'
			]
		},
		{
			heading: 'Known gaps',
			body: 'These capabilities are not available in the current user interface. Their absence is documented so teams can evaluate Kuayle against present requirements rather than future assumptions.',
			list: [
				'Data import and export workflows',
				'Enterprise identity: SSO, SAML, SCIM and LDAP',
				'GitLab and chat integrations'
			]
		},
		{
			heading: 'Dev Machines is opt-in infrastructure',
			body: 'The repository now includes the opt-in multi-container Dev Machines subsystem: Caddy wildcard ingress, an unprivileged authenticated gateway, a dedicated Docker manager, isolated machine networks, PostgreSQL reconciliation, scoped secrets, activity collection, issue-linked agent runs, and Claude Code, OpenCode, Codex, and custom CLI adapters. Self-hosted operators must explicitly configure the machine domain, TLS, runtime images, GitHub permissions, and capacity.',
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
