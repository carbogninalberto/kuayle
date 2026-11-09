<script lang="ts">
	/**
	 * Sets the browser tab title to "Kuayle - Part - Part". Callers pass the parts
	 * most specific first (page, then its parents); the title reads in reverse, so
	 * ['Launch', 'Engineering'] becomes "Kuayle - Engineering - Launch".
	 * Empty parts (data still loading) are skipped, so the title never shows
	 * "undefined"; with no parts at all it falls back to plain "Kuayle".
	 */
	let { parts }: { parts: (string | null | undefined | false)[] } = $props();

	const title = $derived(
		['Kuayle', ...parts.filter((p): p is string => typeof p === 'string' && p.trim().length > 0).reverse()].join(' - ')
	);
</script>

<svelte:head>
	<title>{title}</title>
</svelte:head>
