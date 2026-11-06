<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { RELEASES_PAGE_URL, useLatestRelease } from '$lib/release.svelte';

	const release = useLatestRelease();

	function formatDate(value: string | null): string {
		if (!value) return '';
		const date = new Date(value);
		return Number.isNaN(date.getTime())
			? ''
			: date.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });
	}
</script>

<section id="releases" class="py-16" aria-labelledby="releases-heading">
	<div class="mx-auto max-w-6xl px-6">
		<div class="flex flex-wrap items-end justify-between gap-3">
			<div>
				<p class="font-mono text-xs tracking-widest text-brand-300 uppercase">Changelog</p>
				<h2 id="releases-heading" class="mt-2 text-2xl font-bold tracking-tight">Recently shipped</h2>
			</div>
			<a href={RELEASES_PAGE_URL} target="_blank" rel="noopener" class="inline-flex items-center gap-1 text-sm font-medium text-brand-300 hover:text-brand-200">
				All releases <ArrowRight class="size-4" aria-hidden="true" />
			</a>
		</div>
		{#if release.recent.length > 0}
			<ol class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				{#each release.recent as item (item.version)}
					<li>
						<a href={item.url} target="_blank" rel="noopener" class="block h-full rounded-xl border border-border bg-card/50 p-4 transition-colors hover:border-brand-400/40">
							<p class="flex items-center justify-between font-mono text-xs">
								<span class="text-brand-200">{item.version}</span>
								<span class="text-muted-foreground">{formatDate(item.publishedAt)}</span>
							</p>
							<p class="mt-2 line-clamp-3 text-sm leading-snug text-muted-foreground">{item.summary ?? 'See the release notes'}</p>
						</a>
					</li>
				{/each}
			</ol>
		{:else}
			<p class="mt-6 rounded-xl border border-dashed border-white/15 p-4 text-sm text-muted-foreground">
				Kuayle ships often. Read every release note on
				<a href={RELEASES_PAGE_URL} target="_blank" rel="noopener" class="text-brand-300 underline-offset-4 hover:underline">GitHub Releases</a>.
			</p>
		{/if}
	</div>
</section>
