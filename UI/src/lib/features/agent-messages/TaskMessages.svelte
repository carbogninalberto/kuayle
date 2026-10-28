<script lang="ts">
	import { MessagesSquare, ExternalLink } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentMessages, type AgentMessage } from '$lib/api/agent-messages';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { appToast } from '$lib/features/toast/toast';
	import { toneBadge } from '$lib/features/assistant/task-tone';
	import AgentMessageRow from './AgentMessageRow.svelte';
	import { messageTone, summarize, type MessageAction } from './agent-messages';
	import { moderate, stateLabel } from './labels';
	import { m } from '$lib/paraglide/messages.js';

	// The messages this task's issue sent and received, compact on the task
	// card. Hidden while there are none; the navigator lists everything.
	let { slug, task }: { slug: string; task: Pick<AgentTask, 'issue_id' | 'updated_at' | 'run_id'> } = $props();
	let messages = $state<AgentMessage[]>([]),
		showAll = $state(false),
		busy = $state<string | null>(null);
	const counts = $derived(summarize(messages));
	const visible = $derived(showAll ? messages : messages.slice(0, 3));

	async function load() {
		try {
			messages = (await agentMessages.list(slug, { issue_id: task.issue_id }))?.messages ?? [];
		} catch {
			// The registry is supplementary on the card; the navigator reports errors.
			messages = [];
		}
	}
	$effect(() => {
		void [slug, task.issue_id, task.updated_at, task.run_id];
		void load();
	});

	async function act(message: AgentMessage, action: MessageAction) {
		busy = message.id;
		try {
			await moderate(slug, message, action);
			await load();
		} catch (error) {
			appToast.apiError(error, m['agent_messages.action_failed']());
		} finally {
			busy = null;
		}
	}
</script>

{#if messages.length}<section aria-label={m['agent_messages.task_panel']()} class="min-w-0 space-y-2">
		<div class="flex min-w-0 flex-wrap items-center gap-2">
			<MessagesSquare size={14} class="text-muted-foreground" aria-hidden="true" />
			<h3 class="text-xs font-medium text-muted-foreground">{m['agent_messages.task_panel']()}</h3>
			{#each Object.entries(counts) as [state, count] (state)}
				<Badge variant="outline" class="h-5 px-1.5 text-[11px] {toneBadge[messageTone(state as AgentMessage['state'])]}"
					>{count} · {stateLabel(state as AgentMessage['state'])}</Badge
				>
			{/each}
			<a
				class="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:underline"
				href="/{slug}/settings/agent-messages?issue_id={encodeURIComponent(task.issue_id)}"
				><ExternalLink size={12} aria-hidden="true" />{m['agent_messages.open_navigator']()}</a
			>
		</div>
		<ul class="space-y-1.5">
			{#each visible as message (message.id)}<AgentMessageRow
					{message}
					{slug}
					busy={busy === message.id}
					onaction={act}
				/>{/each}
		</ul>
		{#if messages.length > 3 && !showAll}<Button variant="ghost" size="sm" onclick={() => (showAll = true)}
				>{m['agent_messages.show_all']({ count: messages.length })}</Button
			>{/if}
	</section>{/if}
