<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { alternativePages, competitorBySlug } from '$lib/data/alternative-pages';
	import { alternativePath } from '$lib/data/competitor-types';

	const featured = ['linear', 'jira', 'plane', 'asana', 'clickup', 'gitlab', 'youtrack', 'trello'].filter((slug) =>
		alternativePages.some((page) => page.competitor === slug)
	);
</script>

<section aria-labelledby="switching-heading" class="border-y border-white/5 bg-white/[0.015] py-10">
	<div class="mx-auto flex max-w-6xl flex-col gap-5 px-6 md:flex-row md:items-center md:justify-between">
		<h2 id="switching-heading" class="shrink-0 text-sm font-medium text-muted-foreground">Switching from</h2>
		<ul class="flex flex-wrap gap-2">
			{#each featured as slug}
				{@const competitor = competitorBySlug(slug)}
				<li>
					<a
						href={alternativePath(competitor)}
						class="inline-flex items-center gap-1.5 rounded-full border border-white/10 px-3 py-1.5 text-sm transition-colors hover:border-brand-400/40 hover:text-foreground"
					>
						{competitor.displayName ?? competitor.name}
						<ArrowRight class="size-3.5 text-muted-foreground" aria-hidden="true" />
					</a>
				</li>
			{/each}
			<li>
				<a href="/compare" class="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm text-brand-300 hover:text-brand-200">
					All 28 tools
					<ArrowRight class="size-3.5" aria-hidden="true" />
				</a>
			</li>
		</ul>
	</div>
</section>
