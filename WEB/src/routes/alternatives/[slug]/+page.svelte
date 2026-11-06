<script lang="ts">
	import { page } from '$app/state';
	import PageLayout from '$lib/components/PageLayout.svelte';
	import { HUBS, breadcrumbsFrom } from '$lib/data/routes';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { alternatives } from '$lib/data/alternatives';
	import CompetitorMatrix from '$lib/components/CompetitorMatrix.svelte';
	import { competitors } from '$lib/data/competitors';

	const MATRIX: Record<string, { heading: string; filter: (c: (typeof competitors)[number]) => boolean; caption: string }> = {
		'open-source-issue-trackers': {
			heading: 'Every open source tracker compared',
			filter: (c) => c.category === 'open-source',
			caption: 'Licences from each project’s own repository; checked 6 November 2026.'
		},
		'self-hosted-issue-trackers': {
			heading: 'Every self-hostable tracker compared',
			filter: (c) => c.hosting === 'self-hosted' || c.hosting === 'both',
			caption: 'Includes proprietary products with a self-managed edition; checked 6 November 2026.'
		}
	};

	const slug = $derived(page.params.slug ?? '');

	const content = $derived(slug ? alternatives[slug] : undefined);
	const parent = $derived(HUBS.alternatives);

	const meta = $derived(
		content
			? {
					title: content.title,
					description: content.description,
					canonical: url(`/alternatives/${slug}`),
					ogType: 'article' as const,
					modifiedAt: contentModifiedAt(`/alternatives/${slug}`)
				}
			: { title: '', description: '', canonical: url('/') }
	);

	const crumbs = $derived(breadcrumbsFrom('alternatives', 'Alternatives', content?.heading, slug));
</script>

{#if content}
	<PageLayout
		{meta}
		heading={content.heading}
		intro={content.intro}
		sections={content.sections}
		footnotes={content.footnotes}
		breadcrumbs={crumbs}
		hubLinks={parent.children}
		hubTitle="Alternatives"
		currentSlug={slug}
	>
		{#if MATRIX[slug]}
			<section aria-labelledby="matrix-heading">
				<h2 id="matrix-heading" class="flex items-center gap-3 text-2xl font-semibold tracking-tight">
					<span class="h-px w-6 bg-brand-400/60"></span>
					{MATRIX[slug].heading}
				</h2>
				<div class="mt-6">
					<CompetitorMatrix competitors={competitors.filter(MATRIX[slug].filter)} caption={MATRIX[slug].caption} />
				</div>
			</section>
		{/if}
	</PageLayout>
{/if}
