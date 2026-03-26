<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import type { Cycle } from '$lib/types/cycle';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		cycles,
		value,
		onchange,
		trigger,
		showNone = true,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		shortcutKey,
	}: {
		open?: boolean;
		cycles: Cycle[];
		value: string | null | undefined;
		onchange: (cycleId: string | null) => void;
		trigger: Snippet;
		showNone?: boolean;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search cycles..." emptyMessage="No cycles." {width} {align} {shortcutKey} {trigger}>
	{#if showNone}
		<Command.Item
			value="No cycle"
			onSelect={() => { onchange(null); open = false; }}
			class="text-[var(--color-text-tertiary)]"
		>
			No cycle
		</Command.Item>
	{/if}
	{#each cycles as cycle (cycle.id)}
		<Command.Item
			value={cycle.name}
			onSelect={() => { onchange(cycle.id); open = false; }}
			data-checked={value === cycle.id}
			class="flex items-center gap-2"
		>
			{cycle.name}
		</Command.Item>
	{/each}
</ComboboxPopover>
