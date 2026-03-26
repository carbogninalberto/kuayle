<script lang="ts">
	import ComboboxPopover from '$lib/components/shared/ComboboxPopover.svelte';
	import * as Command from '$lib/components/ui/command/index.js';
	import { FolderKanban } from 'lucide-svelte';
	import type { Project } from '$lib/types/project';
	import type { Snippet } from 'svelte';

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
	}: {
		open?: boolean;
		projects: Project[];
		value: string | null | undefined;
		onchange: (projectId: string | null) => void;
		trigger: Snippet;
		showNone?: boolean;
		width?: string;
		align?: 'start' | 'center' | 'end';
		shortcutKey?: string;
	} = $props();
</script>

<ComboboxPopover bind:open placeholder="Search projects..." emptyMessage="No projects." {width} {align} {shortcutKey} {trigger}>
	{#if showNone}
		<Command.Item
			value="No project"
			onSelect={() => { onchange(null); open = false; }}
			class="text-[var(--color-text-tertiary)]"
		>
			No project
		</Command.Item>
	{/if}
	{#each projects as project (project.id)}
		<Command.Item
			value={project.name}
			onSelect={() => { onchange(project.id); open = false; }}
			data-checked={value === project.id}
			class="flex items-center gap-2"
		>
			<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
			{project.name}
		</Command.Item>
	{/each}
</ComboboxPopover>
