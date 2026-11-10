<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { m } from '$lib/features/i18n/messages';
	import type { IssueAgentAssignee } from '$lib/api/agents';
	import AgentAvatar from './AgentAvatar.svelte';
	import { agentIdentities } from './agent-identities.state.svelte';
	import { toggleIssueAgent } from './issue-agents.svelte';

	// The AI agents part of an assignee picker: the agents enabled for the
	// issue's team, toggled like people. `command` renders inside a Command
	// list (ComboboxPopover); `plain` inside a plain popover list. With
	// `selected` and `onselect` it only picks (a new issue has nothing to
	// assign to yet); otherwise it assigns on the issue.
	let {
		slug,
		teamId,
		issue,
		selected,
		onselect,
		onchanged,
		variant = 'command'
	}: {
		slug: string;
		teamId: string | null | undefined;
		issue?: { id: string; identifier: string; agent_assignees?: IssueAgentAssignee[] };
		selected?: string[];
		onselect?: (agentId: string) => void;
		onchanged?: (agents: IssueAgentAssignee[]) => void;
		variant?: 'command' | 'plain';
	} = $props();

	$effect(() => {
		void agentIdentities.ensure(slug);
	});

	const agents = $derived(agentIdentities.forTeam(teamId));
	const isAssigned = (id: string) => (selected ? selected.includes(id) : (issue?.agent_assignees ?? []).some((agent) => agent.id === id));

	async function toggle(id: string) {
		if (onselect) return onselect(id);
		if (!issue) return;
		const result = await toggleIssueAgent(slug, issue, id);
		if (result) onchanged?.(result);
	}
</script>

{#if agents.length > 0}
	{#if variant === 'command'}
		<Command.Separator class="my-1" />
		<Command.Group heading={m['agents.assignee.section']()}>
			{#each agents as agent (agent.id)}
				<Command.Item value={`${agent.name} agent`} keywords={[agent.harness, 'agent']} onSelect={() => toggle(agent.id)} class="flex items-center gap-2" data-agent-option={agent.name}>
					<Checkbox checked={isAssigned(agent.id)} />
					<AgentAvatar name={agent.name} color={agent.avatar_color} size={16} />
					<span class="min-w-0 truncate">{agent.name}</span>
				</Command.Item>
			{/each}
		</Command.Group>
	{:else}
		<div class="mx-1 my-1 border-t border-[var(--app-border)]"></div>
		<div class="px-2 py-1 text-[10px] font-medium tracking-wide text-[var(--color-text-tertiary)] uppercase">{m['agents.assignee.section']()}</div>
		{#each agents as agent (agent.id)}
			{@const assigned = isAssigned(agent.id)}
			<Button
				variant="plain"
				onclick={() => toggle(agent.id)}
				aria-pressed={assigned}
				data-agent-option={agent.name}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {assigned ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				<AgentAvatar name={agent.name} color={agent.avatar_color} size={16} />
				<span class="min-w-0 flex-1 truncate text-left">{agent.name}</span>
				{#if assigned}<span aria-hidden="true">✓</span>{/if}
			</Button>
		{/each}
	{/if}
{/if}
