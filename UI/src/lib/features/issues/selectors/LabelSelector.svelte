<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import type { Label } from '$lib/types/label';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		labels,
		value = [],
		onchange,
		trigger,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
	}: {
		open?: boolean;
		labels: Label[];
		value: string[];
		onchange: (labelId: string) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search labels..." emptyMessage="No labels." {width} {align} {trigger}>
	{#each labels as label (label.id)}
		{@const isSelected = value.includes(label.id)}
		<Command.Item
			value={label.name}
			onSelect={() => onchange(label.id)}
			class="flex items-center gap-2"
		>
			<Checkbox checked={isSelected} />
			<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
			<span class="truncate">{label.name}</span>
		</Command.Item>
	{/each}
</ComboboxPopover>
