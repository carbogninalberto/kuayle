<script lang="ts">
	import { flip } from 'svelte/animate';
	import { slide } from 'svelte/transition';
	import { prefersReducedMotion } from 'svelte/motion';
	import { workflows, type WorkflowList } from '$lib/api/workflows';
	import type { ConversationState } from './conversation-state.svelte';
	import ApprovalCard from './ApprovalCard.svelte';

	let { chat, focusComposer }: { chat: ConversationState; focusComposer: () => void } = $props();
	// Oldest first. IDs break ties, including after history reloads and stream replays.
	const pending = $derived(
		Object.values(chat.approvals)
			.filter((a) => a.status === 'pending')
			.sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id))
	);
	let workflowList = $state<WorkflowList | null>(null);
	const needsWorkflows = $derived(pending.some((a) => a.action === 'delegate_issue'));
	$effect(() => {
		if (!needsWorkflows || workflowList) return;
		let current = true;
		workflows
			.list(chat.slug)
			.then((list) => {
				if (current) workflowList = list;
			})
			.catch(() => {});
		return () => {
			current = false;
		};
	});
</script>

<div class="max-h-[calc((100dvh-var(--assistant-keyboard-inset,0px))*0.45)] overflow-y-auto overscroll-contain" data-testid="approval-stack">
	{#each pending as approval (approval.id)}
		<div animate:flip={{ duration: prefersReducedMotion.current ? 0 : 160 }}>
			<div transition:slide={{ duration: prefersReducedMotion.current ? 0 : 180 }} class="pb-2">
				<ApprovalCard
					{approval}
					slug={chat.slug}
					{workflowList}
					onresolve={async (item, decision, edit) => {
						const focused = document.activeElement;
						await chat.resolveApproval(item, decision, edit);
						if (
							focused?.closest('[data-testid="approval-card"]') &&
							(document.activeElement === focused || document.activeElement === document.body)
						)
							focusComposer();
					}}
				/>
			</div>
		</div>
	{/each}
</div>
