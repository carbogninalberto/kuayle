<script lang="ts">
	import { page } from '$app/state';
	import PageLayout from '$lib/components/PageLayout.svelte';
	import { HUBS, breadcrumbsFrom } from '$lib/data/routes';
	import { url } from '$lib/config/site';
	import { alternatives } from '$lib/data/alternatives';

	const slug = $derived(page.params.slug ?? '');

	const content = $derived(slug ? alternatives[slug] : undefined);
	const parent = $derived(HUBS.alternatives);

	const meta = $derived(content ? {
		title: content.title,
		description: content.description,
		canonical: url(`/alternatives/${slug}`),
		ogType: 'article' as const,
		modifiedAt: '2026-07-11'
	} : { title: '', description: '', canonical: url('/') });

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
	/>
{/if}
