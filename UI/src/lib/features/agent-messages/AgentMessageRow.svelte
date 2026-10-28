<script lang="ts">
	import {
		ArrowRight,
		CirclePause,
		CirclePlay,
		Lock,
		MessageSquare,
		MessagesSquare,
		Trash2,
		LoaderCircle
	} from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { toneBadge } from '$lib/features/assistant/task-tone';
	import type { AgentMessage } from '$lib/api/agent-messages';
	import { messageActions, messageTone, type MessageAction } from './agent-messages';
	import { stateLabel, actionLabel } from './labels';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { m } from '$lib/paraglide/messages.js';

	// One registry record: who wrote to whom, through which channel, its state
	// and the moderator's controls. The body is agent text, shown escaped.
	let {
		message,
		slug,
		busy = false,
		onaction
	}: {
		message: AgentMessage;
		slug: string;
		busy?: boolean;
		onaction?: (message: AgentMessage, action: MessageAction) => void;
	} = $props();
	let expanded = $state(false);
	const actions = $derived(onaction ? messageActions(message) : []);
	const icon = {
		hold: CirclePause,
		release: CirclePlay,
		drop: Trash2,
		hold_conversation: MessagesSquare,
		release_conversation: MessagesSquare
	} as const;
	const recipient = $derived(message.to_identifier ?? (message.to_ref || m['agent_messages.hidden_recipient']()));
	const when = $derived(
		new Date(message.created_at).toLocaleString(getLocale(), { dateStyle: 'short', timeStyle: 'short' })
	);
	const note = $derived.by(() => {
		if (message.state === 'rejected' && message.reject_reason)
			return m['agent_messages.refused']({ reason: message.reject_reason });
		if (message.state === 'held')
			return message.held_by_name ? m['agent_messages.held_by']({ name: message.held_by_name }) : '';
		if (message.state === 'dropped' && message.dropped_by_name)
			return m['agent_messages.dropped_by']({ name: message.dropped_by_name });
		if (message.state === 'delivered') return m['agent_messages.delivered_note']();
		if (message.state === 'queued') return m['agent_messages.queued_note']();
		return '';
	});
	const long = $derived(message.body.length > 160 || message.body.includes('\n'));
</script>

<li
	class="min-w-0 rounded-md border px-3 py-2"
	aria-label={m['agent_messages.row']({ from: message.from_identifier, to: recipient })}
>
	<div class="flex min-w-0 items-center gap-2">
		<span
			class="shrink-0 text-muted-foreground"
			title={message.channel === 'comment'
				? m['agent_messages.channel_comment']()
				: m['agent_messages.channel_internal']()}
		>
			{#if message.channel === 'comment'}<MessageSquare size={14} aria-hidden="true" />{:else}<Lock
					size={14}
					aria-hidden="true"
				/>{/if}
			<span class="sr-only"
				>{message.channel === 'comment'
					? m['agent_messages.channel_comment']()
					: m['agent_messages.channel_internal']()}</span
			>
		</span>
		<span class="flex min-w-0 flex-1 items-center gap-1 text-sm font-medium">
			<a class="truncate hover:underline" href="/{slug}/issue/{message.from_identifier}" title={message.from_title}
				>{message.from_identifier}</a
			>
			<ArrowRight size={12} class="shrink-0 text-muted-foreground" aria-label={m['agent_messages.to']()} />
			{#if message.to_identifier}<a
					class="truncate hover:underline"
					href="/{slug}/issue/{message.to_identifier}"
					title={message.to_title ?? ''}>{message.to_identifier}</a
				>{:else}<span class="truncate text-muted-foreground">{recipient}</span>{/if}
		</span>
		<Badge variant="outline" class="shrink-0 {toneBadge[messageTone(message.state)]}" role="status"
			>{stateLabel(message.state)}</Badge
		>
	</div>
	<p class="mt-1 text-sm break-words whitespace-pre-wrap {expanded ? '' : 'line-clamp-2'}">{message.body}</p>
	<div class="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
		<time datetime={message.created_at}>{when}</time>
		{#if note}<span class="min-w-0 break-words">{note}</span>{/if}
		{#if message.comment_state === 'posted'}<span>{m['agent_messages.comment_posted']()}</span>{/if}
		{#if long}<Button
				variant="link"
				size="sm"
				class="h-auto p-0 text-xs"
				aria-expanded={expanded}
				onclick={() => (expanded = !expanded)}
				>{expanded ? m['agent_messages.less']() : m['agent_messages.more']()}</Button
			>{/if}
		{#if actions.length}
			<Tooltip.Provider>
				<div class="ml-auto flex items-center gap-0.5" role="group" aria-label={m['agent_messages.actions']()}>
					{#each actions as action (action)}
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									{@const Icon = icon[action]}
									<Button
										{...props}
										variant="ghost"
										size="icon-sm"
										class={action === 'drop' ? 'text-destructive hover:text-destructive' : ''}
										aria-label={actionLabel(action)}
										disabled={busy}
										onclick={() => onaction?.(message, action)}
										>{#if busy}<LoaderCircle class="animate-spin" />{:else}<Icon />{/if}</Button
									>
								{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Content>{actionLabel(action)}</Tooltip.Content>
						</Tooltip.Root>
					{/each}
				</div>
			</Tooltip.Provider>
		{/if}
	</div>
</li>
