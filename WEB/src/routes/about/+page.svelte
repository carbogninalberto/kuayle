<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';
	import { useLatestRelease } from '$lib/release.svelte';

	const release = useLatestRelease();

	const meta = {
		title: 'About Kuayle — Product, Maintainer and Current State',
		description: 'Learn why Kuayle exists, who maintains it, how it is built, and what is implemented in the current release.',
		canonical: url('/about'),
		modifiedAt: '2026-07-11'
	};

	const crumbs = breadcrumbsFrom('about', 'About');

	const sections = $derived([
		{
			heading: 'What Kuayle is',
			body: 'Kuayle is a self-hosted issue tracker with keyboard shortcuts, multi-assignee issues, cycles, projects, analytics and GitHub automation. An opt-in Dev Machines subsystem adds agentic coding environments. The name comes from 快乐 (kuàilè), the Chinese word for happiness or joy.',
			list: [
				'Name: 快乐 (kuàilè) — happiness, joy',
				`Current version: ${release.version}`,
				'Deployment: self-hosted with Docker Compose',
				'License: Apache 2.0'
			]
		},
		{
			heading: 'Why we built it',
			body: 'Kuayle started from a specific product requirement: keep the fast, keyboard-oriented workflow of tools such as Linear while adding multi-assignee issues, self-hosting and an open-source license. It began as an internal tool and was released as a public project.',
			list: [
				'Keyboard shortcuts for issue creation, search, navigation and issue updates',
				'Multiple assignees on one issue',
				'Self-hosted deployment with PostgreSQL and Redis',
				'Public source under Apache 2.0'
			]
		},
		{
			heading: 'Who is behind it',
			body: 'Kuayle is developed and maintained by Bakney srl, an Italian software company based in Monteforte d\'Alpone (VR). The project is led by Alberto Carbognin.',
			list: [
				'Company: Bakney srl (Italy, EU)',
				'Founder: Alberto Carbognin',
				'Contact: support@bakney.com',
				'GitHub: github.com/carbogninalberto/kuayle'
			]
		},
		{
			heading: 'How it is built',
			body: 'The application uses a Go API with SQL repositories, a SvelteKit frontend, PostgreSQL for application data and Redis as a required service reserved for cache and job use. The self-hosting configuration adds Caddy for reverse proxying and TLS; the opt-in Dev Machines profile adds a Go gateway and manager for container runtimes.',
			list: [
				'Go API and explicit SQL migrations',
				'SvelteKit user interface',
				'PostgreSQL and Redis',
				'Docker Compose deployment with Caddy'
			]
		},
		{
			heading: 'Current state',
			body: `Version ${release.version} is a runnable MVP, not a mature enterprise platform. Core issue workflows, cycles, projects, analytics, GitHub integration, public sharing and workspace roles are available, and the Dev Machines subsystem is implemented as opt-in infrastructure. Data import/export and enterprise identity integrations are not implemented.`,
			list: [
				'Available: issues, labels, comments, history, sub-issues and relations',
				'Available: cycles with charts, projects with a Gantt view, and insights analytics',
				'Available: GitHub activity linking and configurable status transitions',
				'Available: opt-in Dev Machines with agent providers and issue worktrees',
				'Not available: SSO, SCIM, LDAP and import/export workflows'
			]
		}
	]);
</script>

<StandalonePage
	{meta}
	heading="About Kuayle"
	intro="Kuayle is maintained by Bakney srl as an open-source, self-hosted issue tracker. This page describes the product, the implementation and the limits of the current release."
	{sections}
	breadcrumbs={crumbs}
/>
