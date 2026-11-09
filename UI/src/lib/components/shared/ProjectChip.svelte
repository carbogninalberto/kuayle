<script lang="ts">
	import type { Project } from '$lib/types/project';
	import ProjectIcon from './ProjectIcon.svelte';

	// A project's icon and name as a small chip. Below the sm breakpoint only the
	// icon shows, so list rows and cards stay compact; the name stays in the title.
	let {
		project,
		class: className = ''
	}: {
		project: Pick<Project, 'name' | 'icon' | 'color'>;
		class?: string;
	} = $props();
</script>

<span
	data-testid="project-chip"
	title={project.name}
	class="inline-flex min-w-0 max-w-40 shrink-0 items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 text-[11px] leading-5 text-[var(--color-text-tertiary)] {className}"
>
	<ProjectIcon {project} size={11} />
	<span class="hidden truncate sm:inline">{project.name}</span>
	<span class="sr-only sm:hidden">{project.name}</span>
</span>
