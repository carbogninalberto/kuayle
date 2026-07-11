<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Nav from '$lib/components/Nav.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import { reveal } from '$lib/actions/reveal';
	import { url } from '$lib/config/site';
	import { HUBS, breadcrumbsFrom, metaForStandalone, webPageLd } from '$lib/data/routes';

	const hub = HUBS.alternatives;
	const meta = metaForStandalone('alternatives')!;
	const crumbs = breadcrumbsFrom('alternatives', 'Alternatives');
	const jsonLd = webPageLd(meta.title, meta.description, url('/alternatives'), crumbs);
</script>

<Seo meta={{ ...meta, jsonLd }} />
<Nav />

<main class="mx-auto max-w-5xl px-6 pt-28 pb-20">
	<Breadcrumbs breadcrumbs={crumbs} />
	<article class="mt-8">
		<div use:reveal>
			<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Evaluation guides</p>
			<h1 class="mt-3 text-4xl font-bold tracking-tight">Issue Tracker Alternatives</h1>
			<p class="mt-4 max-w-3xl text-lg leading-relaxed text-muted-foreground">
				Separate focused issue trackers from broader project-management suites and source-hosting
				platforms. Compare license, edition model, deployment footprint and operator responsibility.
			</p>
		</div>

		<div class="mt-12 grid gap-5 sm:grid-cols-2">
			{#each hub.children as child, i}
				<a href={child.href} use:reveal={{ delay: i * 75 }} class="group rounded-2xl border border-border bg-card/60 p-6 transition-all hover:-translate-y-1 hover:border-brand-400/40">
					<h2 class="text-xl font-semibold">{child.label}</h2>
					<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{child.description}</p>
					<span class="mt-5 inline-flex items-center gap-1 text-sm font-medium text-brand-300">Read guide <ArrowRight class="size-4" /></span>
				</a>
			{/each}
		</div>

		<CtaSection />
	</article>
</main>

<Footer />
