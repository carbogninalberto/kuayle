<script lang="ts">
	import { onMount } from 'svelte';
	import { issuesState } from './issues.state.svelte';

	let sentinel: HTMLDivElement | undefined;

	onMount(() => {
		const scrollContainer = sentinel?.parentElement;
		if (!scrollContainer) return;

		const handleScroll = () => {
			const distanceToBottom = scrollContainer.scrollHeight - scrollContainer.scrollTop - scrollContainer.clientHeight;
			if (distanceToBottom <= 300) void issuesState.loadMore();
		};

		scrollContainer.addEventListener('scroll', handleScroll, { passive: true });
		return () => scrollContainer.removeEventListener('scroll', handleScroll);
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
