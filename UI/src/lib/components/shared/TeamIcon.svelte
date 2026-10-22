<script lang="ts">
	import type { Team } from '$lib/types/team';
	import { getStableTeamColor } from '$lib/features/layout/sidebar.state.svelte';
	import TeamIconGlyph from './TeamIconGlyph.svelte';

	const LEGACY_ICON_NAMES: Record<string, string> = {
		box: 'Box',
		'circle-dot': 'CircleDot',
		layers: 'Layers',
		settings: 'Settings',
		shield: 'ShieldCheck',
		'square-user': 'SquareUser',
		users: 'Users'
	};

	function resolveIconName(icon?: string | null): string {
		if (!icon || icon.startsWith('emoji:')) return 'SquareUser';
		return LEGACY_ICON_NAMES[icon] ?? icon;
	}

	let {
		team,
		size = 16,
		class: className = 'shrink-0'
	}: {
		team: Team;
		size?: number;
		class?: string;
	} = $props();

	const iconName = $derived(resolveIconName(team.icon));
	const emoji = $derived(team.icon?.startsWith('emoji:') ? team.icon.slice(6) : null);
	const color = $derived(getStableTeamColor(team));
</script>

{#if emoji}
	<span class={className} style="font-size: {size}px; line-height: 1" aria-hidden="true">{emoji}</span>
{:else}
	<TeamIconGlyph name={iconName} {size} class={className} style="color: {color}" />
{/if}
