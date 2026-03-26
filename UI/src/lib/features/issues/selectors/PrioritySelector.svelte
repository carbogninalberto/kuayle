<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { PRIORITY_LABELS, type IssuePriority } from '$lib/types/issue';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		value,
		onchange,
		trigger,
		width = 'w-40',
		align = 'start' as 'start' | 'center' | 'end',
		shortcutKey,
	}: {
		open?: boolean;
		value: IssuePriority;
		onchange: (priority: IssuePriority) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
	} = $props();

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
</script>

<ComboboxPopover bind:open showSearch={false} {width} {align} {shortcutKey} {trigger}>
	{#each priorityValues as p (p)}
		<Command.Item
			value={PRIORITY_LABELS[p]}
			onSelect={() => { onchange(p); open = false; }}
			data-checked={value === p}
			class="flex items-center gap-2"
		>
			<IssuePriorityIcon priority={p} size={14} />
			{PRIORITY_LABELS[p]}
		</Command.Item>
	{/each}
</ComboboxPopover>
