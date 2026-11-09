<script lang="ts">
	/**
	 * Sets the browser tab title to "Part - Part - Kuayle".
	 * Empty parts (data still loading) are skipped, so the title never shows
	 * "undefined"; with no parts at all it falls back to plain "Kuayle".
	 */
	let { parts }: { parts: (string | null | undefined | false)[] } = $props();

	const title = $derived(
		[...parts.filter((p): p is string => typeof p === 'string' && p.trim().length > 0), 'Kuayle'].join(' - ')
	);
</script>

<svelte:head>
	<title>{title}</title>
</svelte:head>
