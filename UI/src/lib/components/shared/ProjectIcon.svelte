<script lang="ts">
	import type { Project } from '$lib/types/project';
	import TeamIconGlyph from './TeamIconGlyph.svelte';

	let {
		project,
		size = 14,
		class: className = 'shrink-0'
	}: {
		project: Pick<Project, 'icon' | 'color'>;
		size?: number;
		class?: string;
	} = $props();

	const emoji = $derived(project.icon?.startsWith('emoji:') ? project.icon.slice(6) : null);
	const iconName = $derived(project.icon && !emoji ? project.icon : 'FolderKanban');
	const color = $derived(project.color ?? 'var(--color-text-tertiary)');
</script>

{#if emoji}
	<span class={className} style="font-size: {size}px; line-height: 1" aria-hidden="true">{emoji}</span>
{:else}
	<TeamIconGlyph name={iconName} {size} class={className} style="color: {color}" aria-hidden="true" />
{/if}
