<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import type { Team } from '$lib/types/team';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		teams,
		value,
		onchange,
		trigger,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
	}: {
		open?: boolean;
		teams: Team[];
		value: string | undefined;
		onchange: (teamId: string) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search teams..." {width} {align} {trigger}>
	{#each teams as team (team.id)}
		<Command.Item
			value={team.name}
			keywords={[team.key]}
			onSelect={() => { onchange(team.id); open = false; }}
			data-checked={value === team.id}
			class="flex items-center gap-2"
		>
			<span class="flex h-5 w-5 items-center justify-center rounded bg-[var(--color-bg-tertiary)] text-[10px] font-medium shrink-0">
				{team.key.charAt(0)}
			</span>
			{team.name}
		</Command.Item>
	{/each}
</ComboboxPopover>
