<script lang="ts">
	import { PanelLeft, Settings, SquarePen } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { assistantPlaces } from './assistant-places';
	import { m } from '$lib/features/i18n/messages';

	// The assistant's sidebar folded to a rail while a task is open (KUA-281):
	// the places as icons, and the button that opens the conversations again.
	let { slug, onexpand }: { slug: string; onexpand: () => void } = $props();
	const places = $derived(assistantPlaces(slug));
</script>

<nav
	class="flex h-full min-h-0 w-12 shrink-0 flex-col items-center gap-1 border-r bg-muted/20 py-2.5"
	aria-label={m['assistant.sidebar']()}
	data-testid="assistant-sidebar-rail"
>
	<Button
		variant="ghost"
		size="icon-sm"
		aria-label={m['assistant.show_sidebar']()}
		title={m['assistant.show_sidebar']()}
		onclick={onexpand}><PanelLeft /></Button
	>
	<Button variant="ghost" size="icon-sm" href="/{slug}/assistant" aria-label={m['assistant.new']()} title={m['assistant.new']()}
		><SquarePen /></Button
	>
	{#each places as place (place.href)}
		<Button variant="ghost" size="icon-sm" href={place.href} aria-label={place.label} title={place.label}
			><place.icon /></Button
		>
	{/each}
	<Button
		variant="ghost"
		size="icon-sm"
		class="mt-auto"
		href="/{slug}/settings/assistant"
		aria-label={m['settings.nav.assistant']()}
		title={m['settings.nav.assistant']()}><Settings /></Button
	>
</nav>
