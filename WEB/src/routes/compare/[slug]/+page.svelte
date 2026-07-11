<script lang="ts">
	import { page } from '$app/state';
	import PageLayout from '$lib/components/PageLayout.svelte';
	import { HUBS, breadcrumbsFrom } from '$lib/data/routes';
	import { url } from '$lib/config/site';
	import { compare } from '$lib/data/compare';

	const slug = $derived(page.params.slug ?? '');

	const content = $derived(slug ? compare[slug] : undefined);
	const parent = $derived(HUBS.compare);

	const meta = $derived(content ? {
		title: content.title,
		description: content.description,
		canonical: url(`/compare/${slug}`),
		ogType: 'article' as const,
		modifiedAt: '2026-07-11'
	} : { title: '', description: '', canonical: url('/') });

	const crumbs = $derived(breadcrumbsFrom('compare', 'Compare', content?.heading, slug));
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
		hubTitle="Compare"
		currentSlug={slug}
	/>
{/if}
