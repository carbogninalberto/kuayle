<script lang="ts">
	import type { WorkspaceMember } from '$lib/types/workspace';
	import Avatar from './Avatar.svelte';
	import * as HoverCard from '$lib/components/ui/hover-card';

	let {
		member,
		label,
		class: className = ''
	}: {
		member: WorkspaceMember;
		label?: string;
		class?: string;
	} = $props();

	const displayName = $derived(member.name || member.email);
</script>

<HoverCard.Root openDelay={150} closeDelay={100}>
	<HoverCard.Trigger class={className}>
		{label ?? displayName}
	</HoverCard.Trigger>
	<HoverCard.Content class="w-64 p-3">
		<div class="flex items-center gap-3">
			<Avatar name={displayName} size="md" />
			<div class="min-w-0">
				<div class="truncate text-sm font-medium text-[var(--color-text-primary)]">{displayName}</div>
				<div class="truncate text-xs text-[var(--color-text-tertiary)]">{member.email}</div>
				<div class="mt-0.5 text-[11px] capitalize text-[var(--color-text-tertiary)]">{member.role}</div>
			</div>
		</div>
	</HoverCard.Content>
</HoverCard.Root>
