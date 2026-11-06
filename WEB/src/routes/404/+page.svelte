<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Footer from '$lib/components/Footer.svelte';
	import Nav from '$lib/components/Nav.svelte';
	import { Button } from '$lib/components/ui/button';
</script>

<!-- Served by the web server with HTTP 404 for unknown paths (WEB/deploy/Caddyfile). Not indexed, not in the sitemap. -->
<svelte:head>
	<title>Page not found | Kuayle</title>
	<meta name="robots" content="noindex, follow" />
</svelte:head>

<Nav />
<main id="main" tabindex="-1" class="mx-auto max-w-2xl px-6 pt-40 pb-28 text-center outline-none">
	<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">404</p>
	<h1 class="mt-3 text-4xl font-bold tracking-tight">This page does not exist</h1>
	<p class="mt-4 text-lg text-muted-foreground">The link may be old or mistyped. These pages are a good place to start.</p>
	<div class="mt-8 flex flex-wrap justify-center gap-3">
		<Button href="/" class="bg-brand-400 text-white hover:bg-brand-500">Home <ArrowRight /></Button>
		<Button href="/compare" variant="outline">Comparisons</Button>
		<Button href="/self-hosting" variant="outline">Self-hosting guide</Button>
	</div>
</main>
<Footer />
