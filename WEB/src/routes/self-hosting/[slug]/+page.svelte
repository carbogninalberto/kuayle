<script lang="ts">
	import { page } from '$app/state';
	import PageLayout from '$lib/components/PageLayout.svelte';
	import { HUBS, breadcrumbsFrom } from '$lib/data/routes';
	import { url } from '$lib/config/site';
	import { selfHosting } from '$lib/data/self-hosting';
	import { metaForStandalone } from '$lib/data/routes';

	const slug = $derived(page.params.slug ?? '');

	const content = $derived(slug ? selfHosting[slug] : undefined);
	const parent = $derived(HUBS.selfHosting);

	const meta = $derived(content ? {
		title: content.title,
		description: content.description,
		canonical: url(`/self-hosting/${slug}`),
		ogType: 'article' as const,
		modifiedAt: '2026-07-11'
	} : metaForStandalone('self-hosting')!);

	const crumbs = $derived(breadcrumbsFrom('self-hosting', 'Self-Hosting', content?.heading, slug));
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
		hubTitle="Self-Hosting"
		currentSlug={slug}
	/>
{/if}
