<script lang="ts">
	import type { WorkspaceMember } from '$lib/types/workspace';
	import UserHoverCard from '$lib/components/shared/UserHoverCard.svelte';

	let {
		value,
		displayValue,
		members
	}: {
		value: string | null;
		displayValue?: string | null;
		members: WorkspaceMember[];
	} = $props();

	const ids = $derived(value?.split(',').map((id) => id.trim()).filter(Boolean) ?? []);
	const fallbackLabels = $derived(displayValue?.split(',').map((label) => label.trim()) ?? []);
</script>

<span class="inline-flex min-w-0 flex-wrap items-center gap-1">
	{#if ids.length === 0}
		<code class="rounded bg-[var(--color-bg-tertiary)] px-1 py-0.5 text-[11px] text-[var(--color-text-secondary)]">None</code>
	{:else}
		{#each ids as id, index (`${id}-${index}`)}
			{@const member = members.find((candidate) => candidate.user_id === id)}
			{#if member}
				<UserHoverCard
					{member}
					class="inline-flex cursor-pointer rounded bg-[var(--color-bg-tertiary)] px-1 py-0.5 font-mono text-[11px] text-[var(--color-text-secondary)] outline-none transition-colors hover:text-[var(--color-text-primary)] focus-visible:ring-1 focus-visible:ring-[var(--app-accent)]"
				/>
			{:else}
				<code class="rounded bg-[var(--color-bg-tertiary)] px-1 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{fallbackLabels[index] || 'Former user'}</code>
			{/if}
		{/each}
	{/if}
</span>
