<script lang="ts">
	/**
	 * Sets the browser tab title to "Page < Parent < Kuayle". Callers pass the
	 * parts most specific first (page, then its parents), so ['Launch', 'Engineering']
	 * becomes "Launch < Engineering < Kuayle". Browsers cut long tab titles from
	 * the end, so the page name stays visible.
	 * Empty parts (data still loading) are skipped, so the title never shows
	 * "undefined"; with no parts at all it falls back to plain "Kuayle".
	 * The title is rendered as Svelte text, so "<" and part contents are escaped.
	 */
	let { parts }: { parts: (string | null | undefined | false)[] } = $props();

	const title = $derived(
		[...parts.filter((p): p is string => typeof p === 'string' && p.trim().length > 0), 'Kuayle'].join(' < ')
	);
</script>

<svelte:head>
	<title>{title}</title>
</svelte:head>
