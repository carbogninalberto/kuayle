<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		members,
		value = [],
		onchange,
		trigger,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		shortcutKey,
	}: {
		open?: boolean;
		members: WorkspaceMember[];
		value: string[];
		onchange: (memberUserId: string) => void;
		trigger: Snippet;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search members..." emptyMessage="No members." {width} {align} {shortcutKey} {trigger}>
	{#each members as member (member.user_id)}
		{@const isAssigned = value.includes(member.user_id)}
		<Command.Item
			value={member.name || member.email}
			keywords={[member.email]}
			onSelect={() => onchange(member.user_id)}
			class="flex items-center gap-2"
		>
			<Checkbox checked={isAssigned} />
			<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-[var(--app-accent-foreground)] shrink-0">
				{(member.name || member.email).charAt(0).toUpperCase()}
			</div>
			{member.name || member.email}
		</Command.Item>
	{/each}
</ComboboxPopover>
