<script lang="ts">
	import type { Snippet } from 'svelte';

	let { title, description, action, compact = false }: {
		title: string;
		description?: string;
		action?: Snippet;
		compact?: boolean;
	} = $props();
</script>

<header class="flex w-full min-w-0 flex-wrap items-center {compact ? 'gap-2' : 'justify-between gap-x-4 gap-y-3'}">
	<div class="min-w-0 space-y-1.5 wrap-anywhere">
		{#if compact}
			<span class="text-sm font-medium">{title}</span>
		{:else}
			<h1 class="text-2xl font-semibold tracking-tight">{title}</h1>
		{/if}
		{#if description}
			<p class="text-sm text-[var(--color-text-secondary)]">{description}</p>
		{/if}
	</div>
	{#if action}
		<div class="flex max-w-full flex-wrap items-center gap-2" class:order-first={compact}>{@render action()}</div>
	{/if}
</header>
