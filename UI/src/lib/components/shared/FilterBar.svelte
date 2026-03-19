<script lang="ts">
	import type { IssueStatus } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';

	let {
		filters = $bindable({}),
		onchange
	}: {
		filters: Record<string, string>;
		onchange: (filters: Record<string, string>) => void;
	} = $props();

	function setFilter(key: string, value: string) {
		if (value) {
			filters[key] = value;
		} else {
			delete filters[key];
		}
		filters = { ...filters };
		onchange(filters);
	}
</script>

<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-2">
	<select
		class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]"
		onchange={(e) => setFilter('status', (e.target as HTMLSelectElement).value)}
	>
		<option value="">All statuses</option>
		{#each Object.entries(STATUS_LABELS) as [value, label]}
			<option {value}>{label}</option>
		{/each}
	</select>

	<select
		class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]"
		onchange={(e) => setFilter('priority', (e.target as HTMLSelectElement).value)}
	>
		<option value="">All priorities</option>
		{#each Object.entries(PRIORITY_LABELS) as [value, label]}
			<option {value}>{label}</option>
		{/each}
	</select>

	{#if Object.keys(filters).length > 0}
		<button
			onclick={() => {
				filters = {};
				onchange({});
			}}
			class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			Clear filters
		</button>
	{/if}
</div>
