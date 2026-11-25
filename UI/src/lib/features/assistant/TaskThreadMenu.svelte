<script lang="ts">
	import { untrack } from 'svelte';
	import { Ellipsis, FileDown, ScanSearch } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { agentTasks, type AgentTask } from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import { reviewHarnesses } from './agent-review';
	import { m } from '$lib/features/i18n/messages';

	// The thread header's menu (KUA-286): what is done with a task now and
	// then, kept off the header. Download its trace, and ask for an agent
	// review of its published pull request. Nothing is started by the
	// review pick: it writes the request for the assistant, which asks for
	// approval before a reviewer runs. The review is offered while the task
	// awaits review with a pull request open.
	let {
		chat,
		task,
		allowedAgents,
		onrequest,
		ondownload,
		downloading = false
	}: {
		chat: ConversationState;
		task: AgentTask;
		// Coding agents the workspace allows.
		allowedAgents: string[];
		// The request, as the assistant's composer should show it.
		onrequest: (text: string) => void;
		ondownload: () => void;
		downloading?: boolean;
	} = $props();

	const awaiting = $derived(task.kind !== 'review' && task.state === 'awaiting_review' && !!chat.issue);
	const harnesses = $derived(reviewHarnesses(allowedAgents, task.provider));
	// A review needs the pull request the task published, as recorded.
	let published = $state('');
	let disposed = false;
	$effect(() => () => {
		disposed = true;
	});
	$effect(() => {
		const run = task.run_id;
		void chat.taskPublicationCursor[task.id];
		if (!awaiting || !harnesses.length) return;
		untrack(() => {
			agentTasks
				.publication(chat.slug, task.id, run)
				.then((view) => {
					if (!disposed && task.run_id === run)
						published = view.step?.status === 'succeeded' && view.pull_request_url ? run : '';
				})
				.catch(() => {
					/* Without the publication no review is offered. */
				});
		});
	});
	const reviewable = $derived(awaiting && harnesses.length > 0 && published === task.run_id);
	// Runs once the menu has closed, so its focus handling cannot take the
	// composer's focus back.
	let chosen: string | undefined;
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="ghost" size="icon-sm" aria-label={m['tasks.more_actions']()} title={m['tasks.more_actions']()}
				><Ellipsis /></Button
			>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content
		align="end"
		class="max-w-72 min-w-48"
		onCloseAutoFocus={(event) => {
			const harness = chosen;
			chosen = undefined;
			if (!harness) return;
			event.preventDefault();
			// After the menus' own focus handling (the submenu's too) has finished.
			setTimeout(() =>
				onrequest(
					m['tasks.review_request_prompt']({
						identifier: chat.issue?.identifier ?? '',
						agent: agentInfo(harness).name
					})
				)
			);
		}}
	>
		<DropdownMenu.Item disabled={downloading} onclick={ondownload}><FileDown />{m['tasks.download_trace']()}</DropdownMenu.Item>
		{#if reviewable}
			<DropdownMenu.Sub>
				<DropdownMenu.SubTrigger><ScanSearch /><span class="min-w-0 truncate">{m['tasks.request_review']()}</span></DropdownMenu.SubTrigger>
				<DropdownMenu.SubContent
					class="max-w-72 min-w-44"
					onCloseAutoFocus={(event) => {
						if (chosen) event.preventDefault();
					}}
				>
					<DropdownMenu.Label class="text-xs font-normal text-muted-foreground">{m['tasks.review_with']()}</DropdownMenu.Label>
					{#each harnesses as harness (harness)}
						<DropdownMenu.Item onclick={() => (chosen = harness)}
							><AgentLogo agent={harness} size="sm" class="size-4" /><span class="min-w-0 truncate"
								>{agentInfo(harness).name}</span
							>{#if harness === task.provider}{' '}<span class="ml-auto shrink-0 pl-2 text-xs text-muted-foreground"
									>{m['tasks.review_same_agent']()}</span
								>{/if}</DropdownMenu.Item
						>
					{/each}
				</DropdownMenu.SubContent>
			</DropdownMenu.Sub>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
