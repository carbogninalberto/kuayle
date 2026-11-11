<script lang="ts">
	import { getContext } from 'svelte';
	import {
		Bot,
		Check,
		CheckCheck,
		CircleHelp,
		CircleSlash,
		Clock,
		CornerDownRight,
		Reply,
		TriangleAlert
	} from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import type { AgentLiveMessage } from '$lib/api/agent-tasks';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import { answerContext, type AnswerContext, type MessageState, type PlacedMessage } from './agent-conversation';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// One message of the conversation with a task's agent, in the timeline:
	// what its creator (or the assistant for them) said, with the time and a
	// quiet icon for where it stands, and the agent's reply right under it.
	// Nothing is spelled out: the state is the icon, named for assistive
	// technology and on hover. The agent's text is untrusted and
	// shown as plain text only.
	let { placed }: { placed: PlacedMessage } = $props();
	const message = $derived(placed.message);
	// A question is answered from where it was asked: the view's composer
	// takes it as what the next message answers.
	const answer = getContext<AnswerContext | undefined>(answerContext);

	const stateIcon: Record<MessageState, typeof Check> = {
		queued: Clock,
		delivered: Check,
		replied: CheckCheck,
		unanswered: TriangleAlert,
		received: CornerDownRight,
		rejected: CircleSlash
	};
	const stateLabel = (state: MessageState, item: AgentLiveMessage) =>
		state === 'queued'
			? m['tasks.live_state_queued']()
			: state === 'replied'
				? m['tasks.live_state_replied']()
				: state === 'unanswered'
					? m['tasks.live_state_unanswered']()
					: state === 'rejected'
						? m['tasks.live_rejected_reason']({ reason: item.reject_reason ?? '' })
						: item.delivered_via === 'prompt'
							? m['tasks.live_state_prompt']()
							: m['tasks.live_state_inbox']();
	const stateTone: Partial<Record<MessageState, string>> = {
		replied: 'text-[var(--status-success)]',
		unanswered: 'text-[var(--status-warning)]'
	};
	const clock = (value: string) =>
		new Date(value).toLocaleTimeString(getLocale(), { hour: '2-digit', minute: '2-digit' });
	const full = (value: string) =>
		new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'medium' });
</script>

{#snippet mark(state: MessageState, item: AgentLiveMessage)}
	{@const Icon = stateIcon[state]}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<span
					{...props}
					role="img"
					aria-label={stateLabel(state, item)}
					class="inline-flex shrink-0 {stateTone[state] ?? ''}"><Icon size={13} aria-hidden="true" /></span
				>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content side="top">{stateLabel(state, item)}</Tooltip.Content>
	</Tooltip.Root>
{/snippet}

{#snippet when(value: string)}
	<time datetime={value} title={full(value)} class="tabular-nums">{clock(value)}</time>
{/snippet}

{#snippet fromAgent(item: AgentLiveMessage, state: MessageState)}
	<Message.Root align="start" role="article" aria-label={m['tasks.live_from_agent']()} data-state={state}>
		<Message.Content class="gap-1">
			<Bubble.Root variant="outline" class="max-w-[92%]"
				><Bubble.Content
					class="rounded-2xl rounded-bl-md border px-3.5 py-2 break-words whitespace-pre-wrap {item.kind ===
					'question'
						? 'border-[var(--status-warning)]/50'
						: ''} {state === 'rejected' ? 'text-muted-foreground' : ''}">{item.body}</Bubble.Content
				></Bubble.Root
			>
			<Message.Footer class="gap-1.5 px-1 text-[11px] font-normal">
				{#if state === 'rejected'}{@render mark(state, item)}{:else if item.kind === 'question'}<span
						role="img"
						aria-label={m['assistant.agent_question']()}
						title={m['assistant.agent_question']()}
						class="inline-flex shrink-0 text-[var(--status-warning)]"><CircleHelp size={13} aria-hidden="true" /></span
					>{:else}<CornerDownRight size={13} aria-hidden="true" class="shrink-0" />{/if}
				{@render when(item.created_at)}
				<CopyMessageButton text={item.body} />
			</Message.Footer>
			{#if item.kind === 'question' && state === 'received' && answer?.()}<Button
					variant="outline"
					size="sm"
					class="h-7 self-start text-xs"
					onclick={() => answer?.()?.(item)}><Reply />{m['tasks.live_answer']()}</Button
				>{/if}
		</Message.Content>
	</Message.Root>
{/snippet}

<Tooltip.Provider delayDuration={300}>
	{#if message.direction === 'from_agent'}
		{@render fromAgent(message, placed.state)}
	{:else}
		<Message.Root
			align="end"
			role="article"
			aria-label={message.sender === 'orchestrator' ? m['tasks.live_from_orchestrator']() : m['tasks.live_from_you']()}
			data-state={placed.state}
		>
			<Message.Content class="items-end gap-1">
				<Bubble.Root variant="muted" class="max-w-[92%]"
					><Bubble.Content class="rounded-2xl rounded-br-md px-3.5 py-2 break-words whitespace-pre-wrap"
						>{message.body}</Bubble.Content
					></Bubble.Root
				>
				<Message.Footer class="gap-1.5 px-1 text-[11px] font-normal">
					<CopyMessageButton text={message.body} />
					{#if message.sender === 'orchestrator'}<span
							role="img"
							aria-label={m['tasks.live_from_orchestrator']()}
							title={m['tasks.live_from_orchestrator']()}
							class="inline-flex shrink-0"><Bot size={13} aria-hidden="true" /></span
						>{/if}
					{@render when(placed.at)}
					{@render mark(placed.state, message)}
				</Message.Footer>
			</Message.Content>
		</Message.Root>
		{#each placed.replies as reply (reply.id)}{@render fromAgent(reply, 'received')}{/each}
	{/if}
</Tooltip.Provider>
