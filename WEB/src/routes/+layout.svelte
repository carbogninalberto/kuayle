<script lang="ts">
	import '../app.css';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
	<meta name="theme-color" content="#0b0b0f" />
</svelte:head>

{@render children()}
