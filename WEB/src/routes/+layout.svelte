<script lang="ts">
	import '../app.css';
	import CookieBanner from '$lib/components/CookieBanner.svelte';
	import { SITE, url } from '$lib/config/site';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
	<link rel="apple-touch-icon" href="/favicon_logo.svg" />
	<meta name="theme-color" content={SITE.themeColor} />
	<meta name="color-scheme" content="dark" />
	<link rel="manifest" href="/manifest.json" />
</svelte:head>

{@render children()}

<CookieBanner />
