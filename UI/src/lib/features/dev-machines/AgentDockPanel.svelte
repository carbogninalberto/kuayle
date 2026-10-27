<script lang="ts">
	import { CircleAlert, CircleCheck, CircleDot, LoaderCircle, MessageSquare, SquareTerminal } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { AgentQueueItem } from '$lib/api/agent-tasks';
	import { taskLabel } from '$lib/features/assistant/task-labels';
	import { useTerminalDock } from './terminal-dock-context.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// The dock's Agents tab: every task the user started, with a way into its
	// conversation and a terminal in its worktree for direct (TUI) work.
	let { items }: { items: AgentQueueItem[] } = $props();
	const dock = useTerminalDock();
	const running = ['preparing', 'running', 'validating', 'publishing', 'cancelling'];

	function terminal(item: AgentQueueItem) {
		if (!item.machine_id) return;
		dock.open({
			slug: dock.slug,
			machineId: item.machine_id,
			machineName: item.machine_name ?? item.issue_identifier,
			checkoutId: item.checkout_id ?? undefined,
			checkoutLabel: `${item.issue_identifier} · ${item.issue_title}`,
			sessionName: item.issue_identifier
		});
	}
</script>

<div class="min-h-0 flex-1 overflow-y-auto" role="tabpanel" aria-label={m['tasks.agents']()}>
	{#if items.length === 0}
		<p class="p-4 text-sm text-muted-foreground">{m['tasks.agents_empty']()}</p>
	{:else}
		<ul class="divide-y divide-border">
			{#each items as item (item.id)}
				<li class="flex items-center gap-3 px-3 py-2">
					<span class="shrink-0 text-muted-foreground" aria-hidden="true"
						>{#if running.includes(item.state)}<LoaderCircle
								class="size-4 animate-spin"
							/>{:else if item.state === 'completed'}<CircleCheck
								class="size-4"
							/>{:else if item.state === 'failed' || item.state === 'blocked'}<CircleAlert
								class="size-4 text-destructive"
							/>{:else}<CircleDot class="size-4" />{/if}</span
					>
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm">
							<span class="font-medium">{item.issue_identifier}</span> · {item.issue_title}
						</p>
						<p class="truncate text-xs text-muted-foreground">{taskLabel(item.state)} · {item.reason}</p>
					</div>
					<Button
						size="sm"
						variant="ghost"
						href="/{dock.slug}/assistant?conversation={encodeURIComponent(item.conversation_id)}"
						><MessageSquare />{m['tasks.agents_open']()}</Button
					>
					{#if item.machine_id}<Button size="sm" variant="ghost" onclick={() => terminal(item)}
							><SquareTerminal />{m['tasks.agents_terminal']()}</Button
						>{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>
