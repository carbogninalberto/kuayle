<script lang="ts">
	import { X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { PanelContext, PanelDefinition } from './panel-host.svelte';
	import { m } from '$lib/features/i18n/messages';

	// The side panel column: a switcher for the registered panels and the
	// active panel's body, scrolling on its own. The same host is used inline
	// (a resizable column) and inside the narrow-screen drawer.
	let {
		panels,
		active,
		context
	}: { panels: PanelDefinition[]; active: PanelDefinition; context: PanelContext } = $props();
</script>

<div class="flex h-full min-h-0 min-w-0 flex-col bg-background" data-testid="side-panel">
	<div class="flex h-12 shrink-0 items-center gap-1 border-b px-2">
		<!-- A narrow column shows the switcher's icons only; the names stay for screen readers and hover. -->
		<div class="@container flex min-w-0 flex-1 items-center gap-0.5" role="toolbar" aria-label={m['assistant.panels']()}>
			{#each panels as panel (panel.id)}
				<!-- Labelled buttons, no tooltip: a drawer focuses the first one on open. The
				     open panel shows its name; the others their icon, so many panels fit. -->
				<Button
					variant={panel.id === active.id ? 'secondary' : 'ghost'}
					size="sm"
					class="h-8 shrink-0 gap-1.5 px-2 text-xs"
					aria-pressed={panel.id === active.id}
					title={panel.label()}
					onclick={() => context.open(panel.id)}
					><panel.icon class="size-3.5" /><span class={panel.id === active.id ? 'max-[420px]:sr-only' : 'sr-only'}
						>{panel.label()}</span
					></Button
				>
			{/each}
		</div>
		<Button variant="ghost" size="icon-sm" aria-label={m['assistant.panel_close']()} onclick={context.close}
			><X /></Button
		>
	</div>
	<!-- The diff and panels that say so manage their own scrolling; other panels scroll here. -->
	<div
		class="min-h-0 flex-1 {active.id === 'diff' || active.scrolls ? 'flex flex-col' : 'overflow-y-auto'}"
		role="region"
		aria-label={active.label()}
	>
		<active.component {context} />
	</div>
</div>
