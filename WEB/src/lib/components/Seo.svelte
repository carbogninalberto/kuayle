<script lang="ts">
	import { SITE, resolveMeta, type PageMeta } from '$lib/config/site';

	let { meta }: { meta: PageMeta } = $props();

	const resolved = $derived(resolveMeta(meta));

	function jsonLdString(ld: Record<string, unknown> | Record<string, unknown>[]): string {
		return JSON.stringify(ld).replace(/</g, '\\u003c');
	}
</script>

<svelte:head>
	<title>{resolved.title}</title>
	<meta name="description" content={resolved.description} />
	<link rel="canonical" href={resolved.canonical} />
	{#if resolved.noindex}
		<meta name="robots" content="noindex, follow" />
	{:else}
		<meta name="robots" content="index, follow" />
	{/if}

	<!-- Open Graph -->
	<meta property="og:site_name" content={SITE.name} />
	<meta property="og:title" content={resolved.title} />
	<meta property="og:description" content={resolved.description} />
	<meta property="og:type" content={resolved.ogType} />
	<meta property="og:url" content={resolved.canonical} />
	<meta property="og:image" content={resolved.image} />
	<meta property="og:image:type" content="image/png" />
	<meta property="og:image:alt" content={resolved.imageAlt} />
	<meta property="og:image:width" content={String(resolved.imageWidth)} />
	<meta property="og:image:height" content={String(resolved.imageHeight)} />
	<meta property="og:locale" content={SITE.locale} />

	<!-- Twitter -->
	<meta name="twitter:card" content="summary_large_image" />
	<meta name="twitter:title" content={resolved.title} />
	<meta name="twitter:description" content={resolved.description} />
	<meta name="twitter:image" content={resolved.image} />
	<meta name="twitter:image:alt" content={resolved.imageAlt} />

	{#if resolved.ogType === 'article' && resolved.publishedAt}
		<meta property="article:published_time" content={resolved.publishedAt} />
	{/if}
	{#if resolved.ogType === 'article' && resolved.modifiedAt}
		<meta property="article:modified_time" content={resolved.modifiedAt} />
	{/if}

	<!-- JSON-LD -->
	{#if resolved.jsonLd}
		{@const items = Array.isArray(resolved.jsonLd) ? resolved.jsonLd : [resolved.jsonLd]}
		{#each items as item}
			{@html `<script type="application/ld+json">${jsonLdString(item)}</script>`}
		{/each}
	{/if}
</svelte:head>
