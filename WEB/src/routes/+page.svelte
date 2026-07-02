<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Hero from '$lib/components/Hero.svelte';
	import OpenSource from '$lib/components/OpenSource.svelte';
	import Features from '$lib/components/Features.svelte';
	import Keyboard from '$lib/components/Keyboard.svelte';
	import Pricing from '$lib/components/Pricing.svelte';
	import Deploy from '$lib/components/Deploy.svelte';
	import Footer from '$lib/components/Footer.svelte';

	const title = 'Kuayle: the open source issue tracker with no paid tier';
	const description =
		'Kuayle is a keyboard-driven, self-hosted issue tracker. Apache 2.0 licensed, no enterprise edition, no feature gates. Cycles, projects, GitHub integration and real-time sync in one public repo.';
</script>

<svelte:head>
	<title>{title}</title>
	<meta name="description" content={description} />
	<meta property="og:title" content={title} />
	<meta property="og:description" content={description} />
	<meta property="og:type" content="website" />
	<meta property="og:image" content="/product-screenshot.png" />
	<meta name="twitter:card" content="summary_large_image" />
</svelte:head>

<Nav />

<main>
	<Hero />
	<OpenSource />
	<Features />
	<Keyboard />
	<Pricing />
	<Deploy />
</main>

<Footer />
