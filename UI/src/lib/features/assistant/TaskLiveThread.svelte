<script lang="ts">
	import { untrack } from 'svelte';
	import { ArrowUp, Bot, CircleHelp, LoaderCircle, Reply, User } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { getErrorMessage } from '$lib/features/toast/toast';
	import { agentTasks, type AgentLiveMessage, type AgentTask } from '$lib/api/agent-tasks';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import type { ConversationState } from './conversation-state.svelte';
	import { isMessageableTask } from './shell-rules';
	import { m } from '$lib/paraglide/messages.js';

	// The task's live channel as a conversation with its coding agent: what its
	// creator (or the assistant for them) told the agent and what the agent
	// wrote back. Replies are written in the chat composer aimed at this agent
	// or, inline (the task card), in the thread's own one-line composer. The
	// backend decides who may read or send; the agent's text is untrusted and
	// only shown as plain text.
	let {
		chat,
		task,
		inline = false
	}: { chat: ConversationState; task: Pick<AgentTask, 'id' | 'state'>; inline?: boolean } = $props();
	let draft = $state(''),
		sending = $state(false),
		sendError = $state(''),
		input = $state<HTMLInputElement | null>(null);
	let messages = $state<AgentLiveMessage[]>([]),
		showAll = $state(false),
		unavailable = $state(false);
	let disposed = false;
	$effect(() => () => (disposed = true));
	const open = $derived(isMessageableTask(task.state));
	// Long threads fold their start away: the latest exchange stays in view.
	const visible = $derived(showAll ? messages : messages.slice(-6));
	const targeted = $derived(chat.composerTarget === task.id);
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
	function reply() {
		if (inline) return input?.focus();
		chat.composerTarget = task.id;
		chat.composerFocus++;
	}
	const hint = $derived(task.state === 'running' ? m['tasks.live_hint_running']() : m['tasks.live_hint_next']());
	async function send(event: SubmitEvent) {
		event.preventDefault();
		const body = draft.trim();
		if (!body || sending) return;
		sending = true;
		sendError = '';
		try {
			await agentTasks.sendLiveMessage(chat.slug, task.id, body);
			if (disposed) return;
			if (draft.trim() === body) draft = '';
			// The thread reloads and shows the message with its delivery.
			chat.taskLiveCursor = { ...chat.taskLiveCursor, [task.id]: -Date.now() };
		} catch (e) {
			if (!disposed) sendError = getErrorMessage(e, m['tasks.live_send_error']());
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
		{#if !inline}<div class="flex min-w-0 items-center gap-2">
				<h3 class="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
					{m['tasks.live_messages']()}
				</h3>
			</div>{/if}
		{#if messages.length > visible.length}<Button
				variant="ghost"
				size="sm"
				class="h-auto px-0 text-xs"
				onclick={() => (showAll = true)}>{m['tasks.live_show_all']({ count: messages.length })}</Button
			>{/if}
		{#if messages.length}
			<ol class="flex flex-col gap-2">
				{#each visible as message (message.id)}
					{@const fromAgent = message.direction === 'from_agent'}
					<li
						class="flex min-w-0 flex-col gap-1 {fromAgent ? 'items-start' : 'items-end'}"
						aria-label={sender(message)}
					>
						<div class="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
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
						<p
							class="max-w-[92%] rounded-2xl px-3 py-2 text-sm break-words whitespace-pre-wrap {fromAgent
								? 'rounded-tl-sm border bg-background'
								: 'rounded-tr-sm bg-muted'} {message.kind === 'question' ? 'border-[var(--status-warning)]/50' : ''}"
						>
							{message.body}
						</p>
						<!-- A question is answered right where it was asked. -->
						{#if fromAgent && message.kind === 'question' && open}<Button
								size="sm"
								variant={targeted ? 'secondary' : 'outline'}
								class="h-7 text-xs"
								onclick={reply}><Reply />{m['tasks.live_answer']()}</Button
							>{/if}
						{#if message.state === 'rejected' && message.reject_reason}<p class="text-xs text-muted-foreground">
								{m['tasks.live_rejected_reason']({ reason: message.reject_reason })}
							</p>{/if}
					</li>
				{/each}
			</ol>
		{/if}
		{#if open && inline}
			<!-- A one-line composer for this agent; the hint says when it reads it. -->
			<form class="min-w-0" onsubmit={send}>
				<InputGroup.Root class="h-8 rounded-lg">
					<InputGroup.Input
						bind:ref={input}
						bind:value={draft}
						class="h-8 text-xs placeholder:text-xs md:text-xs"
						placeholder={m['tasks.live_placeholder']()}
						aria-label={m['tasks.live_reply']()}
						title={hint}
						autocomplete="off"
					/>
					<InputGroup.Addon align="inline-end" class="pr-1">
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									<InputGroup.Button
										{...props}
										type="submit"
										variant="default"
										size="icon-xs"
										class="rounded-md"
										disabled={!draft.trim() || sending}
										aria-label={m['tasks.live_send']()}
										>{#if sending}<LoaderCircle class="animate-spin motion-reduce:animate-none" />{:else}<ArrowUp
											/>{/if}</InputGroup.Button
									>
								{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Content class="max-w-60">{hint}</Tooltip.Content>
						</Tooltip.Root>
					</InputGroup.Addon>
				</InputGroup.Root>
				{#if sendError}<p role="alert" class="mt-1 text-xs text-destructive">{sendError}</p>{/if}
			</form>
		{:else if open}
			<div class="flex min-w-0 flex-wrap items-center gap-2">
				<Button size="sm" variant={targeted ? 'secondary' : 'outline'} aria-pressed={targeted} onclick={reply}
					><Reply />{m['tasks.live_reply']()}</Button
				>
				<p class="min-w-0 text-xs text-muted-foreground">{hint}</p>
			</div>
		{/if}
	</section>{/if}
