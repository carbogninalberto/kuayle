<script lang="ts">
	import type { Snippet } from 'svelte';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { reveal } from '$lib/actions/reveal';

	let {
		id,
		number,
		eyebrow,
		title,
		body,
		points = [],
		link,
		reverse = false,
		visual
	}: {
		id: string;
		number: string;
		eyebrow: string;
		title: string;
		body: string;
		points?: string[];
		link?: { label: string; href: string };
		reverse?: boolean;
		visual: Snippet;
	} = $props();
</script>

<section {id} class="py-20 sm:py-28" aria-labelledby="{id}-heading">
	<div class="mx-auto grid max-w-6xl items-center gap-12 px-6 lg:grid-cols-2 lg:gap-16">
		<div class="min-w-0 {reverse ? 'lg:order-2' : ''}" use:reveal>
			<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">
				<span class="text-muted-foreground">{number}</span> · {eyebrow}
			</p>
			<h2 id="{id}-heading" class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">{title}</h2>
			<p class="mt-4 text-lg leading-relaxed text-muted-foreground">{body}</p>
			{#if points.length > 0}
				<ul class="mt-6 space-y-2 border-l border-dashed border-white/15 pl-4 text-sm text-muted-foreground">
					{#each points as point}<li>{point}</li>{/each}
				</ul>
			{/if}
			{#if link}
				<a href={link.href} class="mt-6 inline-flex items-center gap-1 text-sm font-medium text-brand-300 hover:text-brand-200">
					{link.label}
					<ArrowRight class="size-4" aria-hidden="true" />
				</a>
			{/if}
		</div>
		<div class="min-w-0 {reverse ? 'lg:order-1' : ''}" use:reveal={{ delay: 100 }}>
			{@render visual()}
		</div>
	</div>
</section>
