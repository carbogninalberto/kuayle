<script lang="ts">
	import { page } from '$app/state';
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

	const IMAGE_ICON = /^image:([0-9a-f-]{36}\.(?:png|jpg|webp|gif|ico))$/;

	function resolveIconName(icon?: string | null): string {
		if (!icon || icon.startsWith('emoji:') || icon.startsWith('image:')) return 'SquareUser';
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
	// An uploaded image icon is served by the API under the team.
	const imageFile = $derived(IMAGE_ICON.exec(team.icon ?? '')?.[1] ?? null);
	const imageUrl = $derived(
		imageFile && page.params.workspaceSlug
			? `/api/workspaces/${page.params.workspaceSlug}/teams/${team.id}/icon/${imageFile}`
			: null
	);
</script>

{#if imageUrl}
	<img src={imageUrl} alt="" width={size} height={size} class="{className} rounded-sm object-contain" style="width: {size}px; height: {size}px" />
{:else if emoji}
	<span class={className} style="font-size: {size}px; line-height: 1" aria-hidden="true">{emoji}</span>
{:else}
	<TeamIconGlyph name={iconName} {size} class={className} style="color: {color}" />
{/if}
