<script lang="ts">
	import { ChartPie } from '@lucide/svelte';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { m } from '$lib/features/i18n/messages';
	import type { ConversationState } from './conversation-state.svelte';
	import { contextUsage } from './shell-rules';

	let { chat }: { chat: ConversationState } = $props();
	const usage = $derived(contextUsage(chat.messages, chat.hasMore));
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}
			<InputGroup.Button
				{...props}
				size="icon-sm"
				aria-label={m['assistant.context_usage']()}
				title={m['assistant.context_usage']()}
			><ChartPie size={16} /></InputGroup.Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content side="top" align="end" class="w-56 gap-3 p-4" role="dialog" aria-label={m['assistant.context']()}>
		<Popover.Title>{m['assistant.context']()}</Popover.Title>
		<div
			role="img"
			aria-label={`${m['assistant.context_usage']()} ${usage.percent}%`}
			class="grid size-20 place-items-center self-center rounded-full"
			style:background={`conic-gradient(var(--primary) ${usage.percent}%, var(--muted) 0)`}
		>
			<span class="grid size-16 place-items-center rounded-full bg-popover font-medium tabular-nums">{usage.percent}%</span>
		</div>
		<Popover.Description class="text-xs">
			{m['assistant.context_detail']({ used: String(usage.used), limit: String(usage.limit) })}
		</Popover.Description>
	</Popover.Content>
</Popover.Root>
