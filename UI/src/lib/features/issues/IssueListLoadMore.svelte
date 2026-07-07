<script lang="ts">
	import { onMount } from 'svelte';
	import { issuesState } from './issues.state.svelte';

	let sentinel: HTMLDivElement | undefined;

	onMount(() => {
		const observer = new IntersectionObserver(
			(entries) => {
				if (issuesState.hasMore && entries.some((entry) => entry.isIntersecting)) {
					issuesState.loadMore();
				}
			},
			{ rootMargin: '600px 0px' }
		);
		if (sentinel) observer.observe(sentinel);
		return () => observer.disconnect();
	});
</script>

<div bind:this={sentinel} class={issuesState.hasMore ? 'flex justify-center py-4' : 'hidden'}>
	{#if issuesState.hasMore}
		<button
			type="button"
			onclick={() => issuesState.loadMore()}
			disabled={issuesState.loadingMore}
			class="rounded-md border border-[var(--app-border)] px-4 py-1.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] disabled:opacity-60"
		>
			{issuesState.loadingMore ? 'Loading...' : 'Load more'}
		</button>
	{/if}
</div>
