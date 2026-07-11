<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';

	const meta = {
		title: 'Kuayle Open-Source Model — Apache 2.0, One Edition',
		description: 'How Kuayle uses the Apache 2.0 license, what is included in its public repository, and which self-hosting costs remain yours.',
		canonical: url('/open-source'),
		modifiedAt: '2026-07-11'
	};

	const crumbs = breadcrumbsFrom('open-source', 'Open Source');

	const sections = [
		{
			heading: 'What Apache 2.0 permits',
			body: 'Kuayle is distributed under the Apache License 2.0. The license permits use, modification and redistribution, including commercial use, subject to its notice and attribution requirements. It also includes an explicit patent grant from contributors.',
			list: [
				'Commercial and internal use',
				'Modification and redistribution',
				'No requirement to publish private modifications',
				'License and notice obligations still apply'
			]
		},
		{
			heading: 'What is in the public repository',
			body: 'The current backend, frontend, database migrations, Docker Compose deployment and implemented product features are maintained in one public repository. Kuayle does not currently publish a separate enterprise edition or commercial feature package.',
			list: [
				'Go backend and SvelteKit frontend',
				'Database migrations and self-hosting configuration',
				'No license-key checks in the product',
				'No Contributor License Agreement in the repository today'
			]
		},
		{
			heading: 'What “no paid tier” means',
			body: 'Kuayle does not charge a software license fee, meter users or require a commercial key to enable implemented features. It does not mean that operating an instance is cost-free.',
			list: [
				'No per-seat software fee',
				'No paid feature switch',
				'You pay for infrastructure and storage',
				'You operate backups, monitoring and updates'
			]
		},
		{
			heading: 'What to verify before adoption',
			body: 'A permissive license answers legal questions; it does not guarantee product maturity, support response times or suitability for your environment. Evaluate the current release and operating requirements directly.',
			list: [
				'Review the source and open issues',
				'Test backup and restore procedures',
				'Confirm missing features against your requirements',
				'Review the Apache 2.0 text with your legal adviser when needed'
			]
		}
	];
</script>

<StandalonePage
	{meta}
	heading="Open Source"
	intro="Kuayle’s application code and self-hosting configuration are public under Apache 2.0. Here is the practical scope of that statement, without treating open source as a substitute for product due diligence."
	{sections}
	breadcrumbs={crumbs}
/>
