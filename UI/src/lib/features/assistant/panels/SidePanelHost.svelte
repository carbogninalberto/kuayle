<script lang="ts">
	import { EllipsisVertical, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { cn } from '$lib/utils.js';
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

<div id="assistant-panel-region" class="flex h-full min-h-0 min-w-0 flex-col bg-background" data-testid="side-panel">
	<div class="mobile-touch-targets flex h-12 shrink-0 items-center gap-1 border-b px-2">
		<!-- One rule for the switcher (KUA-296), by its own width: from 18rem every tab shows
		     its icon and the open one its name too (shortened if it must); narrower, icons
		     only; below 13rem, where even seven icons do not fit, the open tab and a "More
		     panels" menu. Names stay for screen readers and hover. Labelled buttons, no
		     tooltip: a drawer focuses the first one on open. -->
		<div class="@container flex min-w-0 flex-1 items-center gap-0.5" role="toolbar" aria-label={m['assistant.panels']()}>
			{#each panels as panel (panel.id)}
				{@const open = panel.id === active.id}
				<Button
					variant={open ? 'secondary' : 'ghost'}
					size="sm"
					class={cn('h-8 gap-1.5 px-2 text-xs @max-[18rem]:px-1.5', open ? 'min-w-0 shrink' : 'shrink-0 @max-[13rem]:hidden max-md:@max-[320px]:hidden')}
					aria-pressed={open}
					title={panel.label()}
					onclick={() => context.open(panel.id)}
					><panel.icon class="size-3.5" /><span class={open ? 'min-w-0 truncate @max-[18rem]:sr-only max-md:@max-[24rem]:sr-only' : 'sr-only'}
						>{panel.label()}</span
					></Button
				>
			{/each}
			{#if panels.length > 1}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="ghost"
								size="sm"
								class="hidden h-8 shrink-0 px-1.5 @max-[13rem]:inline-flex max-md:@max-[320px]:inline-flex"
								aria-label={m['assistant.more_panels']()}
								title={m['assistant.more_panels']()}><EllipsisVertical class="size-3.5" /></Button
							>
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="start" class="mobile-touch-targets w-48">
						<DropdownMenu.RadioGroup value={active.id} onValueChange={(id) => context.open(id)}>
							{#each panels as panel (panel.id)}
								<DropdownMenu.RadioItem value={panel.id}><panel.icon />{panel.label()}</DropdownMenu.RadioItem>
							{/each}
						</DropdownMenu.RadioGroup>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}
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
