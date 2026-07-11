<script lang="ts">
	import { onMount, tick } from 'svelte';
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import Avatar from '$lib/components/shared/Avatar.svelte';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';

	let {
		anchor,
		kind,
		label,
		member,
		issue
	}: {
		anchor: DOMRect;
		kind: 'user' | 'issue';
		label: string;
		member?: WorkspaceMember;
		issue?: Issue;
	} = $props();

	let card: HTMLDivElement;
	let left = $state(0);
	let top = $state(0);
	const displayName = $derived(member?.name || member?.email || label);

	onMount(() => {
		left = anchor.left;
		top = anchor.bottom + 6;
		void tick().then(() => {
			const rect = card.getBoundingClientRect();
			if (rect.right > window.innerWidth - 8) left = Math.max(8, anchor.right - rect.width);
			if (rect.bottom > window.innerHeight - 8) top = Math.max(8, anchor.top - rect.height - 6);
		});
	});
</script>

<div
	bind:this={card}
	class="fixed z-[200] w-64 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-sm text-[var(--color-text-primary)] shadow-xl pointer-events-none"
	style:left="{left}px"
	style:top="{top}px"
	role="tooltip"
>
	{#if kind === 'issue'}
		<div class="flex items-start gap-2">
			<IssueStatusIcon status={issue?.status} category={issue?.status_info?.category} color={issue?.status_info?.color} size={14} />
			<div class="min-w-0">
				<div class="text-xs font-medium text-[var(--color-text-tertiary)]">{issue?.identifier ?? label.split(' ')[0]}</div>
				<div class="mt-0.5 break-words font-medium">{issue?.title ?? label.replace(/^\S+\s*/, '')}</div>
			</div>
		</div>
	{:else}
		<div class="flex items-center gap-3">
			<Avatar name={displayName} size="md" />
			<div class="min-w-0">
				<div class="truncate font-medium">{displayName}</div>
				{#if member}
					<div class="truncate text-xs text-[var(--color-text-tertiary)]">{member.email}</div>
					<div class="mt-0.5 text-[11px] capitalize text-[var(--color-text-tertiary)]">{member.role}</div>
				{/if}
			</div>
		</div>
	{/if}
</div>
