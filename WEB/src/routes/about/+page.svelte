<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';

	const meta = {
		title: 'About Kuayle — Product, Maintainer and Current State',
		description: 'Learn why Kuayle exists, who maintains it, how it is built, and what is implemented in the current release.',
		canonical: url('/about'),
		modifiedAt: '2026-07-11'
	};

	const crumbs = breadcrumbsFrom('about', 'About');

	const sections = [
		{
			heading: 'What Kuayle is',
			body: 'Kuayle is a self-hosted issue tracker with keyboard shortcuts, multi-assignee issues, cycles, projects and GitHub automation. The name comes from 快乐 (kuàilè), the Chinese word for happiness or joy.',
			list: [
				'Name: 快乐 (kuàilè) — happiness, joy',
				'Current version: v0.1.0',
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
			body: 'The application uses a Go API with SQL repositories, a SvelteKit frontend, PostgreSQL for application data and Redis for cache and coordination. The self-hosting configuration adds Caddy for reverse proxying and TLS.',
			list: [
				'Go API and explicit SQL migrations',
				'SvelteKit user interface',
				'PostgreSQL and Redis',
				'Docker Compose deployment with Caddy'
			]
		},
		{
			heading: 'Current state',
			body: 'Version 0.1.0 is a runnable MVP, not a mature enterprise platform. Core issue workflows, cycles, projects, GitHub integration, public sharing and workspace roles are available. A dedicated analytics interface, data import/export and enterprise identity integrations are not implemented.',
			list: [
				'Available: issues, labels, comments, history, sub-issues and relations',
				'Available: cycles with charts and projects with a Gantt view',
				'Available: GitHub activity linking and configurable status transitions',
				'Not available: SSO, SCIM, LDAP, import/export and analytics UI'
			]
		}
	];
</script>

<StandalonePage
	{meta}
	heading="About Kuayle"
	intro="Kuayle is maintained by Bakney srl as an open-source, self-hosted issue tracker. This page describes the product, the implementation and the limits of the current release."
	{sections}
	breadcrumbs={crumbs}
/>
