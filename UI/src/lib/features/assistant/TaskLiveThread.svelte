<script lang="ts">
	import { untrack } from 'svelte';
	import { Bot, CircleHelp, LoaderCircle, MessagesSquare, Send, User } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { agentTasks, type AgentLiveMessage, type AgentTask } from '$lib/api/agent-tasks';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import type { ConversationState } from './conversation-state.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// The task's live channel on its card: what its creator (or the assistant
	// for them) told the agent and what the agent wrote back, with a composer to
	// message the agent directly. The backend decides who may read or send; the
	// agent's text is untrusted and only shown as plain text.
	let { chat, task }: { chat: ConversationState; task: Pick<AgentTask, 'id' | 'state'> } = $props();
	let messages = $state<AgentLiveMessage[]>([]),
		draft = $state(''),
		sending = $state(false),
		error = $state(''),
		showAll = $state(false),
		unavailable = $state(false);
	let disposed = false;
	$effect(() => () => (disposed = true));
	const open = $derived(!['completed', 'cancelled', 'cancelling'].includes(task.state));
	const visible = $derived(showAll ? messages : messages.slice(-4));
	const tooLong = $derived(draft.length > 4000);
	async function load() {
		try {
			const result = await agentTasks.liveMessages(chat.slug, task.id);
			if (!disposed) messages = result.messages ?? [];
		} catch (e) {
			// Another member's task, or a revoked chat: the thread is not shown.
			if (!disposed && [401, 403, 404].includes((e as { status?: number }).status ?? 0)) unavailable = true;
		}
	}
	$effect(() => {
		void [task.id, chat.taskLiveCursor[task.id]];
		untrack(() => void load());
	});
	async function send(event?: Event) {
		event?.preventDefault();
		const body = draft.trim();
		if (!body || tooLong || sending) return;
		sending = true;
		error = '';
		try {
			const sent = await agentTasks.sendLiveMessage(chat.slug, task.id, body);
			if (disposed) return;
			draft = '';
			if (!messages.some((message) => message.id === sent.id)) messages = [...messages, sent];
		} catch (e) {
			if (!disposed) error = getErrorMessage(e, m['tasks.live_send_error']());
		} finally {
			if (!disposed) sending = false;
		}
	}
	const sender = (message: AgentLiveMessage) =>
		message.sender === 'agent'
			? m['tasks.live_from_agent']()
			: message.sender === 'orchestrator'
				? m['tasks.live_from_orchestrator']()
				: m['tasks.live_from_you']();
	function status(message: AgentLiveMessage) {
		if (message.state === 'queued') return m['tasks.live_state_queued']();
		if (message.state === 'rejected') return m['tasks.live_state_rejected']();
		if (message.state === 'delivered')
			return message.delivered_via === 'prompt' ? m['tasks.live_state_prompt']() : m['tasks.live_state_inbox']();
		return '';
	}
	const when = (message: AgentLiveMessage) =>
		new Date(message.created_at).toLocaleTimeString(getLocale(), { hour: '2-digit', minute: '2-digit' });
</script>

{#if !unavailable && (messages.length || open)}<section
		aria-label={m['tasks.live_messages']()}
		class="min-w-0 space-y-2"
	>
		<div class="flex min-w-0 items-center gap-2">
			<MessagesSquare size={14} class="text-muted-foreground" aria-hidden="true" />
			<h3 class="text-xs font-medium text-muted-foreground">{m['tasks.live_messages']()}</h3>
		</div>
		{#if messages.length}
			{#if messages.length > visible.length}<Button
					variant="ghost"
					size="sm"
					class="h-auto px-0 text-xs"
					onclick={() => (showAll = true)}>{m['tasks.live_show_all']({ count: messages.length })}</Button
				>{/if}
			<ol class="space-y-1.5">
				{#each visible as message (message.id)}
					{@const fromAgent = message.direction === 'from_agent'}
					<li
						class="min-w-0 rounded-md border px-3 py-2 {fromAgent ? 'bg-muted/40' : ''} {message.kind === 'question'
							? 'border-[var(--status-warning)]/40'
							: ''}"
						aria-label={sender(message)}
					>
						<div class="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
							{#if fromAgent}<Bot size={12} aria-hidden="true" />{:else}<User size={12} aria-hidden="true" />{/if}
							<span class="font-medium text-foreground">{sender(message)}</span>
							{#if message.kind === 'question'}<Badge
									variant="outline"
									class="h-5 gap-1 border-[var(--status-warning)]/40 px-1.5 text-[11px] text-[var(--status-warning)]"
									><CircleHelp size={11} aria-hidden="true" />{m['assistant.agent_question']()}</Badge
								>{/if}
							<time datetime={message.created_at}>{when(message)}</time>
							{#if status(message)}<span>· {status(message)}</span>{/if}
						</div>
						<p class="mt-1 text-sm break-words whitespace-pre-wrap">{message.body}</p>
						{#if message.state === 'rejected' && message.reject_reason}<p class="mt-1 text-xs text-muted-foreground">
								{m['tasks.live_rejected_reason']({ reason: message.reject_reason })}
							</p>{/if}
					</li>
				{/each}
			</ol>
		{/if}
		{#if open}
			<form class="flex min-w-0 items-end gap-2" onsubmit={send} aria-label={m['tasks.live_composer']()}>
				<Textarea
					bind:value={draft}
					rows={1}
					maxlength={4000}
					class="min-h-9 flex-1 text-sm"
					placeholder={m['tasks.live_placeholder']()}
					aria-label={m['tasks.live_composer']()}
					aria-invalid={tooLong || undefined}
					disabled={sending}
					onkeydown={(event) => {
						if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) void send(event);
					}}
				/>
				<Button type="submit" size="sm" disabled={sending || !draft.trim() || tooLong}
					>{#if sending}<LoaderCircle class="animate-spin" />{:else}<Send />{/if}{m['tasks.live_send']()}</Button
				>
			</form>
			<p class="text-xs text-muted-foreground">
				{task.state === 'running' ? m['tasks.live_hint_running']() : m['tasks.live_hint_next']()}
			</p>
		{/if}
		{#if error}<p role="alert" class="text-sm break-words text-destructive">{error}</p>{/if}
	</section>{/if}
