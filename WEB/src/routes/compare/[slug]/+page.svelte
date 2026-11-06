<script lang="ts">
	import { url } from '$lib/config/site';

	let { data } = $props();
	const target = $derived(url(data.moved.to));
</script>

<svelte:head>
	<title>This comparison moved to {data.moved.label} | Kuayle</title>
	<link rel="canonical" href={target} />
	<meta http-equiv="refresh" content={`0; url=${data.moved.to}`} />
</svelte:head>

<main id="main" class="mx-auto max-w-xl px-6 pt-40 pb-20 text-center">
	<h1 class="text-2xl font-semibold">This comparison has moved</h1>
	<p class="mt-4 text-muted-foreground">
		It now lives at <a class="text-brand-300 underline underline-offset-4" href={data.moved.to}>{data.moved.label}</a>.
	</p>
</main>
