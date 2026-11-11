<script lang="ts">
	import { LoaderCircle, Square } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask } from '$lib/api/agent-tasks';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import type { ConversationState } from './conversation-state.svelte';
	import { m } from '$lib/features/i18n/messages';

	// Stops a task from where it is watched: one click and one confirmation.
	// It is offered while the task can still be stopped, to the person who
	// started it (the server refuses anyone else), and gives way to the task's
	// own "cancelling" state once the request is in.
	let {
		chat,
		task,
		identifier,
		onerror
	}: {
		chat: ConversationState;
		task: AgentTask;
		identifier: string;
		onerror: (message: string) => void;
	} = $props();

	const mine = $derived(task.kind !== 'review' && !!authState.user && task.creator_id === authState.user.id);
	const stoppable = $derived(mine && !['completed', 'cancelled', 'cancelling', 'failed'].includes(task.state));
	// A cancellation whose cleanup did not finish is asked for again here too,
	// without another confirmation: the person already decided.
	const retryable = $derived(mine && task.state === 'cancelling');
	let open = $state(false),
		busy = $state(false);

	async function stop() {
		if (busy) return;
		busy = true;
		onerror('');
		// The dialog closes before the task changes: accepting the cancelling
		// task removes this button, and the dialog with it.
		open = false;
		const previous = chat.liveTasks[task.id] ?? null;
		try {
			await agentTasks.cancel(chat.slug, task.id);
			chat.acceptTask(await agentTasks.get(chat.slug, task.id), false, previous);
		} catch (error) {
			onerror(getErrorMessage(error, m['tasks.stop_error']()));
		} finally {
			busy = false;
		}
	}
</script>

{#if stoppable}
	<Button variant="outline" size="sm" class="shrink-0" disabled={busy} onclick={() => (open = true)}>
		{#if busy}<LoaderCircle class="animate-spin" />{:else}<Square />{/if}{m['tasks.stop']()}
	</Button>
	<AlertDialog.Root bind:open>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>{m['tasks.stop_title']({ identifier })}</AlertDialog.Title>
				<AlertDialog.Description>{m['tasks.stop_desc']()}</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
				<AlertDialog.Action disabled={busy} onclick={() => void stop()}>{m['tasks.stop']()}</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{:else if retryable}
	<Button variant="outline" size="sm" class="shrink-0" disabled={busy} onclick={() => void stop()}>
		{#if busy}<LoaderCircle class="animate-spin" />{/if}{m['tasks.retry_cancel']()}
	</Button>
{/if}
