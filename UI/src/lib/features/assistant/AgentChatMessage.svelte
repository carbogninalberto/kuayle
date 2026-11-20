<script lang="ts">
	import { Bot, CircleHelp, CornerDownRight } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import type { AgentLiveChatMessage } from '$lib/api/agent-tasks';
	import { m } from '$lib/features/i18n/messages';
	import { cn } from '$lib/utils.js';
	import MessageAttachments from './MessageAttachments.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';

	// An echo of a live message in the owning assistant conversation.
	let { message, slug }: { message: AgentLiveChatMessage; slug: string } = $props();
	const outgoing = $derived(message.direction === 'to_agent');
	const question = $derived(!outgoing && message.kind === 'question');
</script>

<Message.Root
	align={outgoing ? 'end' : 'start'}
	role="article"
	aria-label={outgoing ? m['tasks.live_from_you']() : question
		? m['assistant.agent_question_from']({ identifier: message.identifier })
		: m['assistant.agent_message_label']({ identifier: message.identifier })}
>
	<Message.Content class={outgoing ? 'items-end' : undefined}>
		<Message.Header class="gap-1.5">
			{#if outgoing}
				<span>{m['tasks.live_from_you']()}</span>
				<CornerDownRight size={13} aria-hidden="true" />
			{:else}
				<Bot size={13} aria-hidden="true" />
				<span>{m['assistant.agent_message_from']()}</span>
			{/if}
			<a class="underline-offset-2 hover:underline" href="/{slug}/issue/{encodeURIComponent(message.identifier)}"
				>{message.identifier}</a
			>
			{#if question}<Badge
					variant="outline"
					class="h-5 gap-1 border-[var(--status-warning)]/40 px-1.5 text-[11px] text-[var(--status-warning)]"
					><CircleHelp size={11} aria-hidden="true" />{m['assistant.agent_question']()}</Badge
				>{/if}
		</Message.Header>
		{#if message.attachments?.length}
			<MessageAttachments slug={slug} conversationId={message.attachments[0].conversation_id} attachments={message.attachments} />
		{/if}
		{#if message.body}<Bubble.Root variant={outgoing ? 'muted' : 'outline'}>
			<Bubble.Content data-selectable={outgoing ? undefined : 'text'} class={cn('pe-9', question ? 'border-[var(--status-warning)]/40' : '')}>
				<p class="break-words whitespace-pre-wrap">{message.body}</p>
			</Bubble.Content>
			<CopyMessageButton text={message.body} />
		</Bubble.Root>{/if}
		<Message.Footer class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
			<time datetime={message.created_at}
				>{new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time
			>
			{#if !outgoing}<span>{m['assistant.agent_message_untrusted']()}</span>{/if}
		</Message.Footer>
	</Message.Content>
</Message.Root>
