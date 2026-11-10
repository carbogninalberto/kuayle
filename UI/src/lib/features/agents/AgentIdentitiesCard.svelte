<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as Command from '$lib/components/ui/command/index.js';
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import { agentsApi, type AgentIdentity } from '$lib/api/agents';
	import { listTeams } from '$lib/api/teams';
	import type { Team } from '$lib/types/team';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import AgentAvatar from './AgentAvatar.svelte';
	import { agentIdentities } from './agent-identities.state.svelte';
	import { agentName } from '$lib/features/issues/history-agent';

	// Agents as members (KUA-107): administrators choose, per agent profile,
	// the teams where it can be assigned issues and mentioned, and its avatar
	// colour. The server refuses these writes from anyone else.
	let { slug }: { slug: string } = $props();

	let teams = $state<Team[]>([]);
	let saving = $state<string | null>(null);

	onMount(() => {
		void agentIdentities.reload(slug);
		listTeams(slug)
			.then((loaded) => (teams = loaded))
			.catch(() => {});
	});

	const canManage = $derived(agentIdentities.canManage);

	async function save(agent: AgentIdentity, input: { avatar_color?: string; team_ids?: string[] }) {
		if (!canManage || saving) return;
		saving = agent.id;
		try {
			await agentsApi.update(slug, agent.id, input);
			await agentIdentities.reload(slug);
			appToast.success(m['agents.identity.saved']());
		} catch (error) {
			appToast.apiError(error, m['agents.identity.save_failed']());
		} finally {
			saving = null;
		}
	}

	function toggleTeam(agent: AgentIdentity, teamId: string) {
		const next = agent.team_ids.includes(teamId) ? agent.team_ids.filter((id) => id !== teamId) : [...agent.team_ids, teamId];
		void save(agent, { team_ids: next });
	}

	function teamsLabel(agent: AgentIdentity): string {
		if (agent.team_ids.length === 0) return m['agents.identity.no_teams']();
		if (agent.team_ids.length === 1) return teams.find((team) => team.id === agent.team_ids[0])?.name ?? m['agents.identity.teams_count']({ n: 1 });
		return m['agents.identity.teams_count']({ n: agent.team_ids.length });
	}
</script>

<section aria-labelledby="agent-identities-title" class="space-y-4 rounded-xl border bg-card p-5" data-testid="agent-identities">
	<div>
		<h2 id="agent-identities-title" class="text-sm font-semibold">{m['agents.identity.title']()}</h2>
		<p class="text-xs text-muted-foreground">{m['agents.identity.description']()}</p>
	</div>
	{#if !canManage}
		<p class="rounded-lg border bg-muted/40 p-3 text-xs text-muted-foreground">{m['agents.identity.read_only']()}</p>
	{/if}
	{#if agentIdentities.agents.length > 0}
		<ul class="divide-y rounded-lg border">
			{#each agentIdentities.agents as agent (agent.id)}
				<li class="flex flex-wrap items-center gap-3 p-3" data-agent-identity={agent.name}>
					<AgentAvatar name={agent.name} color={agent.avatar_color} size={28} />
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-medium">{agent.name}</p>
						<p class="truncate text-xs text-muted-foreground">
							{agentName(agent.harness)}{agent.model ? ` · ${agent.model}` : ''}
							{#if !agent.available}
								<span class="text-[var(--status-warning)]"> · {agent.unavailable_reason || m['agents.assignee.unavailable']()}</span>
							{/if}
						</p>
					</div>
					{#if canManage}
						<Input
							type="color"
							aria-label={m['agents.identity.color']({ name: agent.name })}
							value={agent.avatar_color || '#6b7280'}
							disabled={saving === agent.id}
							onchange={(event) => save(agent, { avatar_color: (event.currentTarget as HTMLInputElement).value })}
							class="h-8 w-10 cursor-pointer p-1"
						/>
					{/if}
					<div class="flex items-center gap-2">
						<span class="text-xs text-muted-foreground">{m['agents.identity.teams']()}</span>
						{#if canManage}
							<ComboboxPopover width="w-56" align="end">
								{#snippet trigger(props)}
									<Button {...props} variant="outline" size="sm" disabled={saving === agent.id} data-agent-teams={agent.name}>{teamsLabel(agent)}</Button>
								{/snippet}
								{#each teams as team (team.id)}
									<Command.Item value={team.name} onSelect={() => toggleTeam(agent, team.id)} class="flex items-center gap-2">
										<Checkbox checked={agent.team_ids.includes(team.id)} />
										<span class="min-w-0 truncate">{team.name}</span>
									</Command.Item>
								{/each}
							</ComboboxPopover>
						{:else}
							<span class="text-xs">{teamsLabel(agent)}</span>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</section>
