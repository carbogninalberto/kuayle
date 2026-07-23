<script lang="ts">
	import { ChevronDown, ChevronUp, SquareTerminal, X } from 'lucide-svelte';
	import { useTerminalDock } from './terminal-dock-context.svelte';
	import TerminalSession from './TerminalSession.svelte';

	const dock = useTerminalDock();

	const MIN_HEIGHT = 180;
	const MAX_HEIGHT_PCT = 0.7;

	let containerElement = $state<HTMLDivElement | undefined>();
	let dragging = $state(false);
	let dragStartY = 0;
	let dragStartHeight = 0;

	function onPointerDown(e: PointerEvent) {
		if (!containerElement) return;
		dragging = true;
		dragStartY = e.clientY;
		dragStartHeight = dock.height;
		try {
			(e.target as HTMLElement).setPointerCapture(e.pointerId);
		} catch {
			// ignore
		}
	}

	function onPointerMove(e: PointerEvent) {
		if (!dragging) return;
		const delta = dragStartY - e.clientY;
		let newHeight = dragStartHeight + delta;
		const maxHeight = Math.floor(window.innerHeight * MAX_HEIGHT_PCT);
		newHeight = Math.max(MIN_HEIGHT, Math.min(newHeight, maxHeight));
		dock.setHeight(newHeight);
	}

	function onPointerUp(_e: PointerEvent) {
		dragging = false;
	}

	function handleCloseTab(id: string, e: Event) {
		e.stopPropagation();
		dock.closeTab(id);
	}

	function handleCloseAll() {
		dock.closeAll();
	}

	const hasTabs = $derived(dock.tabs.length > 0);
</script>

<svelte:window
	onpointermove={dragging ? onPointerMove : undefined}
	onpointerup={dragging ? onPointerUp : undefined}
/>

{#if hasTabs}
	<div
		bind:this={containerElement}
		class="flex shrink-0 flex-col border-t border-[var(--app-border)] bg-[var(--color-bg)]"
		style="height: {dock.expanded ? dock.height + 'px' : 'auto'}"
		role="region"
		aria-label="Terminal dock"
		data-testid="terminal-dock"
	>
		<div class="flex items-center border-b border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1">
			<button
				type="button"
				class="flex h-7 w-7 items-center justify-center rounded text-zinc-400 hover:bg-[var(--color-bg-hover)] hover:text-zinc-200"
				aria-label={dock.expanded ? 'Collapse terminal dock' : 'Expand terminal dock'}
				onclick={() => dock.toggle()}
			>
				{#if dock.expanded}
					<ChevronDown class="size-3.5" />
				{:else}
					<ChevronUp class="size-3.5" />
				{/if}
			</button>
			<div class="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto px-1" role="tablist" aria-label="Terminal sessions">
				{#each dock.tabs as t (t.id)}
					<div class="flex min-w-0 max-w-[200px] shrink-0 items-center gap-1 rounded-t border border-b-0 px-2.5 py-1 text-xs transition-colors {dock.activeTabId === t.id ? 'border-[var(--app-border)] border-b-transparent bg-zinc-950 text-zinc-200' : 'border-transparent text-zinc-500'}">
						<button
							type="button"
							class="flex min-w-0 items-center gap-1 truncate hover:text-zinc-300"
							role="tab"
							aria-selected={dock.activeTabId === t.id}
							onclick={() => dock.setActiveTab(t.id)}
						>
							<SquareTerminal class="size-3 shrink-0" />
							<span class="truncate">{t.runtimeTitle ?? t.sessionName ?? t.machineName}</span>
						</button>
						<button
							type="button"
							class="ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-zinc-500 hover:bg-red-500/10 hover:text-red-400"
							aria-label="Close tab: {t.runtimeTitle ?? t.sessionName ?? t.machineName}"
							data-testid="close-tab"
							onclick={(e) => handleCloseTab(t.id, e)}
						>
							<X class="size-2.5" />
						</button>
					</div>
				{/each}
			</div>
			<div class="flex shrink-0 items-center gap-0.5 pr-0.5">
				{#if dock.tabs.length > 1 && dock.expanded}
					<button
						type="button"
						class="flex h-6 w-6 items-center justify-center rounded text-zinc-500 hover:bg-red-500/10 hover:text-red-400"
						aria-label="Close all terminal tabs"
						data-testid="close-all-tabs"
						onclick={handleCloseAll}
					>
						<X class="size-3" />
					</button>
				{/if}
			</div>
		</div>

		{#if dock.expanded}
			<div
				class="h-1 cursor-ns-resize bg-zinc-800 hover:bg-zinc-600 active:bg-zinc-500"
				role="separator"
				aria-label="Resize terminal dock"
				aria-orientation="horizontal"
				data-testid="terminal-resize-handle"
				onpointerdown={onPointerDown}
			></div>
		{/if}
		<div class="flex min-h-0 flex-1 overflow-hidden" class:hidden={!dock.expanded}>
			{#each dock.tabs as t (t.id)}
				<TerminalSession tab={t} visible={dock.expanded && dock.activeTabId === t.id} />
			{/each}
		</div>
	</div>
{/if}
