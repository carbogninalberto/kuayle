<script lang="ts">
	import { page } from '$app/state';
	import PageLayout from '$lib/components/PageLayout.svelte';
	import { HUBS, breadcrumbsFrom } from '$lib/data/routes';
	import { url } from '$lib/config/site';
	import { features } from '$lib/data/features';
	import { metaForStandalone } from '$lib/data/routes';

	const slug = $derived(page.params.slug ?? '');

	const content = $derived(slug ? features[slug] : undefined);
	const parent = $derived(HUBS.features);

	const meta = $derived(content ? {
		title: content.title,
		description: content.description,
		canonical: url(`/features/${slug}`),
		ogType: 'article' as const,
		modifiedAt: '2026-07-11'
	} : metaForStandalone('features')!);

	const crumbs = $derived(breadcrumbsFrom('features', 'Features', content?.heading, slug));
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
		hubTitle="Features"
		currentSlug={slug}
	/>
{/if}
