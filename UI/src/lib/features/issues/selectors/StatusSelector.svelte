<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import type { TeamStatus } from '$lib/types/team-status';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		statuses,
		value,
		onchange,
		trigger,
		width = 'w-44',
		align = 'start' as 'start' | 'center' | 'end',
	}: {
		open?: boolean;
		statuses: TeamStatus[];
		value: string | undefined;
		onchange: (statusId: string) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search statuses..." {width} {align} {trigger}>
	{#each statuses as ts (ts.id)}
		<Command.Item
			value={ts.name}
			keywords={[ts.category]}
			onSelect={() => { onchange(ts.id); open = false; }}
			data-checked={value === ts.id}
			class="flex items-center gap-2"
		>
			<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
			{ts.name}
		</Command.Item>
	{/each}
</ComboboxPopover>
