<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Hero from '$lib/components/Hero.svelte';
	import OpenSource from '$lib/components/OpenSource.svelte';
	import Features from '$lib/components/Features.svelte';
	import Keyboard from '$lib/components/Keyboard.svelte';
	import Pricing from '$lib/components/Pricing.svelte';
	import Deploy from '$lib/components/Deploy.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import { url, SITE } from '$lib/config/site';
	import { ORGANIZATION_LD, WEBSITE_LD, SOFTWARE_APP_LD, webPageLd } from '$lib/data/routes';

	const title = 'Kuayle — The Issue Tracker With No Paid Tier';
	const description = SITE.description;
	const canonical = url('/');

	const jsonLd = [
		ORGANIZATION_LD,
		WEBSITE_LD,
		SOFTWARE_APP_LD,
		webPageLd(title, description, canonical)
	];
</script>

<Seo
	meta={{
		title,
		description,
		canonical,
		jsonLd
	}}
/>

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
