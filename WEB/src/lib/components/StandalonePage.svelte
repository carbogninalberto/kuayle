<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import Check from '@lucide/svelte/icons/check';
	import { reveal } from '$lib/actions/reveal';
	import { url, type PageMeta } from '$lib/config/site';
	import { webPageLd, type Crumb } from '$lib/data/routes';

	interface Section {
		heading?: string;
		body?: string;
		list?: string[];
		links?: { label: string; href: string }[];
	}

	let {
		meta: metaInput,
		heading,
		intro,
		sections,
		footnotes,
		breadcrumbs: crumbs
	}: {
		meta: PageMeta;
		heading: string;
		intro?: string;
		sections: Section[];
		footnotes?: string;
		breadcrumbs: Crumb[];
	} = $props();

	const jsonLd = $derived(webPageLd(metaInput.title, metaInput.description, metaInput.canonical ?? url('/'), crumbs));
</script>

<Seo meta={{ ...metaInput, jsonLd }} />

<Nav />

<main class="mx-auto max-w-3xl px-6 pt-28 pb-20">
	<div class="mb-8">
		<Breadcrumbs breadcrumbs={crumbs} />
	</div>

	<article>
		<div use:reveal>
			<h1 class="text-4xl font-bold tracking-tight sm:text-5xl">
				<span class="gradient-text">{heading}</span>
			</h1>
			{#if intro}
				<p class="mt-4 text-lg leading-relaxed text-muted-foreground">{intro}</p>
			{/if}
		</div>

		<div class="mt-12 space-y-14">
			{#each sections as section, i}
				<section use:reveal={{ delay: i * 50 }}>
					{#if section.heading}
						<h2 class="flex items-center gap-3 text-2xl font-semibold tracking-tight">
							<span class="h-px w-6 bg-brand-400/60"></span>
							{section.heading}
						</h2>
					{/if}
					{#if section.body}
						<p class="mt-3 leading-relaxed text-muted-foreground">{section.body}</p>
					{/if}
					{#if section.list && section.list.length > 0}
						<ul class="mt-4 space-y-2.5 text-sm text-muted-foreground">
							{#each section.list as item}
								<li class="flex items-start gap-2.5">
									<Check class="mt-0.5 size-4 shrink-0 text-brand-300" />
									<span>{item}</span>
								</li>
							{/each}
						</ul>
					{/if}
					{#if section.links && section.links.length > 0}
						<ul class="mt-4 space-y-2 text-sm">
							{#each section.links as link}
								<li><a class="text-brand-300 underline-offset-4 hover:underline" href={link.href} target="_blank" rel="noopener noreferrer">{link.label}</a></li>
							{/each}
						</ul>
					{/if}
				</section>
			{/each}
		</div>

		{#if footnotes}
			<aside class="mt-12 rounded-lg border border-white/5 bg-white/[0.02] p-4 text-xs text-muted-foreground">
				{footnotes}
			</aside>
		{/if}

		<CtaSection />
	</article>
</main>

<Footer />
