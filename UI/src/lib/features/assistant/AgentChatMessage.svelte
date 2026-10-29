<script lang="ts">
	import { Bot, CircleHelp } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import type { AgentLiveChatMessage } from '$lib/api/agent-tasks';
	import { m } from '$lib/paraglide/messages.js';

	// A message a coding agent sent this conversation through its live channel.
	// It is the agent's own untrusted text: shown escaped, never as Markdown, so
	// it cannot bring links, images or formatting, and the assistant never acts
	// on it by itself.
	let { message, slug }: { message: AgentLiveChatMessage; slug: string } = $props();
	const question = $derived(message.kind === 'question');
</script>

<Message.Root
	align="start"
	role="article"
	aria-label={question
		? m['assistant.agent_question_from']({ identifier: message.identifier })
		: m['assistant.agent_message_label']({ identifier: message.identifier })}
>
	<Message.Content>
		<Message.Header class="gap-1.5">
			<Bot size={13} aria-hidden="true" />
			<span>{m['assistant.agent_message_from']()}</span>
			<a class="underline-offset-2 hover:underline" href="/{slug}/issue/{encodeURIComponent(message.identifier)}"
				>{message.identifier}</a
			>
			{#if question}<Badge
					variant="outline"
					class="h-5 gap-1 border-[var(--status-warning)]/40 px-1.5 text-[11px] text-[var(--status-warning)]"
					><CircleHelp size={11} aria-hidden="true" />{m['assistant.agent_question']()}</Badge
				>{/if}
		</Message.Header>
		<Bubble.Root variant="outline">
			<Bubble.Content class={question ? 'border-[var(--status-warning)]/40' : ''}>
				<p class="break-words whitespace-pre-wrap">{message.body}</p>
			</Bubble.Content>
		</Bubble.Root>
		<Message.Footer class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
			<time datetime={message.created_at}
				>{new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time
			>
			<span>{m['assistant.agent_message_untrusted']()}</span>
		</Message.Footer>
	</Message.Content>
</Message.Root>
