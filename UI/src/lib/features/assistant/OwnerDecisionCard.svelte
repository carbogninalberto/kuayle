<script lang="ts">
	import { tick } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { agentTasks, type OwnerDecision, type AgentTask } from '$lib/api/agent-tasks';
	import { workflows, type WorkflowDecision } from '$lib/api/workflows';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';
	import type { ConversationState } from './conversation-state.svelte';
	import type { TaskRun } from './task-run.svelte';
	import { decisionAnchor, decisionOptions } from './task-room';

	let { chat, task, item, run }: { chat: ConversationState; task: AgentTask; item: OwnerDecision; run: TaskRun } =
		$props();
	let busy = $state(false),
		replying = $state(false),
		draft = $state('');
	let input = $state<HTMLTextAreaElement>();
	const split = $derived(item.kind === 'request' && item.data.action === 'split');
	const options = $derived(item.kind === 'ask' || item.kind === 'request' ? decisionOptions(item.body) : []);
	const title = $derived(
		split
			? m['tasks.decision_split']()
			: item.kind === 'approval'
				? m['tasks.decision_merge']({ commit: item.data.commit?.slice(0, 7) ?? '' })
				: item.kind === 'pause'
					? m['tasks.decision_pause']()
					: item.kind === 'request'
						? m['tasks.room_request']({ action: item.data.action ?? '' })
						: item.body.split('\n')[0]
	);
	const detail = $derived.by(() => {
		if (item.kind === 'ask')
			return item.body
				.slice(title.length)
				.split('\n')
				.filter(
					(line) =>
						!options.includes(
							line
								.trim()
								.replace(/^[-*] /, '')
								.replaceAll('**', '')
						)
				)
				.join('\n')
				.trim();
		if (split && item.body === 'Create the proposed sub-issues') return '';
		return item.body.replace(/^Workflow · /, '');
	});

	async function answer(action: 'approve' | 'decline' | 'reply' | 'continue' | 'stop', body = '') {
		if (busy) return;
		busy = true;
		try {
			if (item.kind === 'approval' || item.kind === 'pause') {
				await workflows.decide(chat.slug, task.id, item.seq, action as WorkflowDecision);
			} else if (split && action === 'approve') {
				await agentTasks.approveSplit(chat.slug, task.id, item.seq);
			} else if (action === 'approve' || action === 'decline' || action === 'reply') {
				const post = await agentTasks.postRoom(chat.slug, task.id, {
					kind: 'say',
					reply_to: item.seq,
					to: [item.asker],
					data: { answer: action },
					body: body || (action === 'approve' ? m['tasks.decision_approved']() : m['tasks.decision_declined']())
				});
				chat.acceptRoomMessages(task.id, [post]);
			}
			const previous = chat.liveTasks[task.id];
			chat.acceptTask(await agentTasks.get(chat.slug, task.id), false, previous);
			await run.loadRoom();
			await run.load();
		} catch (error) {
			appToast.apiError(error, m['workflows.task.decision_failed']());
		} finally {
			busy = false;
		}
	}
	async function openReply() {
		replying = true;
		await tick();
		input?.focus();
	}
</script>

<article
	id={decisionAnchor(task.id, item.kind, item.seq)}
	tabindex="-1"
	aria-label={title}
	data-testid="owner-decision"
	class="space-y-2 rounded-md border border-[var(--status-warning)]/45 bg-background p-3 text-sm"
>
	<p class="text-xs text-muted-foreground">{m['tasks.decision_asked_by']({ name: item.asker })}</p>
	<h3 class="font-medium [overflow-wrap:anywhere]">{title}</h3>
	{#if detail}<p class="whitespace-pre-wrap text-muted-foreground [overflow-wrap:anywhere]">{detail}</p>{/if}
	{#if split && item.data.issues?.length}
		<ol class="list-decimal space-y-1 ps-5">
			{#each item.data.issues as issue}<li class="[overflow-wrap:anywhere]">
					<strong class="font-medium">{issue.title}</strong> · {issue.description}
				</li>{/each}
		</ol>
	{/if}
	<div class="flex flex-wrap gap-2">
		{#if item.kind === 'pause'}
			<Button size="sm" disabled={busy} onclick={() => answer('continue')}>{m['workflows.task.continue']()}</Button>
			<Button size="sm" variant="outline" disabled={busy} onclick={() => answer('stop')}
				>{m['workflows.task.stop']()}</Button
			>
		{:else}
			{#if item.kind !== 'ask'}
				<Button size="sm" disabled={busy} onclick={() => answer('approve')}>{m['workflows.task.approve']()}</Button>
				{#if item.kind === 'request'}<Button
						size="sm"
						variant="outline"
						disabled={busy}
						onclick={() => answer('decline')}>{m['tasks.decision_decline']()}</Button
					>{/if}
			{/if}
			{#each options as option}<Button
					size="sm"
					variant="outline"
					class="h-auto max-w-full whitespace-normal py-1 text-start [overflow-wrap:anywhere]"
					disabled={busy}
					onclick={() => answer('reply', option)}>{option}</Button
				>{/each}
			{#if !replying && item.kind !== 'approval'}<Button size="sm" variant="outline" disabled={busy} onclick={openReply}
					>{m['tasks.decision_reply']()}</Button
				>{/if}
		{/if}
	</div>
	{#if replying}<form
			class="space-y-2"
			onsubmit={(event) => {
				event.preventDefault();
				if (draft.trim()) void answer('reply', draft.trim());
			}}
		>
			<Textarea bind:ref={input} bind:value={draft} aria-label={m['tasks.decision_reply']()} maxlength={4000} />
			<Button size="sm" type="submit" disabled={busy || !draft.trim()}>{m['tasks.decision_send_reply']()}</Button>
			<Button size="sm" variant="ghost" disabled={busy} onclick={() => (replying = false)}
				>{m['common.cancel']()}</Button
			>
		</form>{/if}
</article>
