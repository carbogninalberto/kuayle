<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { FolderKanban } from '@lucide/svelte';
	import type { Project } from '$lib/types/project';
	import type { Snippet } from 'svelte';
	import { m, getLocale, setLocale } from '$lib/features/i18n/messages';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { groupProjectsByTeam } from '$lib/features/projects/project-scope';

	let {
		open = $bindable(false),
		projects,
		value,
		onchange,
		trigger,
		showNone = true,
		width = 'w-48',
		align = 'start' as 'start' | 'center' | 'end',
		shortcutKey,
		ownTeamId = null,
	}: {
		open?: boolean;
		projects: Project[];
		value: string | null | undefined;
		onchange: (projectId: string | null) => void;
		trigger: Snippet<[Record<string, unknown>]>;
		showNone?: boolean;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
		/** The issue's team: its projects come first, other teams follow as named groups. */
		ownTeamId?: string | null;
	} = $props();

	const groups = $derived(groupProjectsByTeam(projects, sidebarState.teams, ownTeamId));
</script>

{#snippet projectItem(project: Project)}
	<Command.Item
		value={project.name}
		onSelect={() => { onchange(project.id); open = false; }}
		data-checked={value === project.id}
		class="flex items-center gap-2"
	>
		<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
		{project.name}
	</Command.Item>
{/snippet}

<ComboboxPopover bind:open placeholder={m['sharedComponents.selectors.search_projects']()} emptyMessage={m['sharedComponents.selectors.no_projects']()} {width} {align} {shortcutKey} {trigger}>
	{#if showNone}
		<Command.Item
			value={m['sharedComponents.filter_builder.no_project']()}
			onSelect={() => { onchange(null); open = false; }}
			class="text-[var(--color-text-tertiary)]"
		>
			{m['sharedComponents.filter_builder.no_project']()}
		</Command.Item>
	{/if}
	{#each groups.own as project (project.id)}
		{@render projectItem(project)}
	{/each}
	{#each groups.teams as group (group.team.id)}
		<Command.Group heading={group.team.name}>
			{#each group.projects as project (project.id)}
				{@render projectItem(project)}
			{/each}
		</Command.Group>
	{/each}
	{#if groups.workspace.length > 0}
		<Command.Group heading={m['projects.picker.workspace']()}>
			{#each groups.workspace as project (project.id)}
				{@render projectItem(project)}
			{/each}
		</Command.Group>
	{/if}
</ComboboxPopover>
