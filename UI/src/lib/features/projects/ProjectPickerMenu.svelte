<script lang="ts">
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import { FolderKanban, Users, X } from '@lucide/svelte';
	import { m } from '$lib/features/i18n/messages';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import type { Project } from '$lib/types/project';
	import { commonTeamId, groupProjectsByTeam, projectsForIssues } from './project-scope';

	/**
	 * The Project submenu of an issue (or of several issues): "No project", the
	 * projects of the issues' own team, a separator, then Teams > team > projects.
	 * Workspace-wide projects (no team) sit under Teams > Workspace. Only projects
	 * every issue can join are offered.
	 */
	let {
		projects,
		issues,
		onselect
	}: {
		projects: Project[];
		issues: { team_id: string }[];
		onselect: (projectId: string | null) => void;
	} = $props();

	const iconClass = 'h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]';
	const rowClass = 'flex items-center gap-2';

	const teams = $derived(sidebarState.teams);
	const groups = $derived(
		groupProjectsByTeam(projectsForIssues(projects, issues, teams), teams, commonTeamId(issues))
	);
	const teamEntries = $derived([
		...groups.teams.map((group) => ({ id: group.team.id, name: group.team.name, projects: group.projects })),
		...(groups.workspace.length > 0
			? [{ id: 'workspace', name: m['projects.picker.workspace'](), projects: groups.workspace }]
			: [])
	]);
</script>

<ContextMenu.Item onclick={() => onselect(null)}
	><span class={rowClass}><X class={iconClass} />{m['common.context_menu.no_project']()}</span></ContextMenu.Item
>
{#each groups.own as project (project.id)}
	<ContextMenu.Item onclick={() => onselect(project.id)}
		><span class={rowClass}><FolderKanban class={iconClass} /><span class="truncate">{project.name}</span></span
		></ContextMenu.Item
	>
{/each}
{#if teamEntries.length > 0}
	<ContextMenu.Separator />
	<ContextMenu.Sub>
		<ContextMenu.SubTrigger
			><span class={rowClass}><Users class={iconClass} />{m['projects.picker.teams']()}</span></ContextMenu.SubTrigger
		>
		<ContextMenu.SubContent class="w-48">
			{#each teamEntries as entry (entry.id)}
				<ContextMenu.Sub>
					<ContextMenu.SubTrigger><span class="truncate">{entry.name}</span></ContextMenu.SubTrigger>
					<ContextMenu.SubContent class="w-48">
						{#each entry.projects as project (project.id)}
							<ContextMenu.Item onclick={() => onselect(project.id)}
								><span class={rowClass}><FolderKanban class={iconClass} /><span class="truncate">{project.name}</span></span
								></ContextMenu.Item
							>
						{/each}
					</ContextMenu.SubContent>
				</ContextMenu.Sub>
			{/each}
		</ContextMenu.SubContent>
	</ContextMenu.Sub>
{/if}
