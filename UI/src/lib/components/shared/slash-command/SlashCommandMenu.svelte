<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { SlashGroup, SlashMenuItem } from './slash-items';

	let {
		groups,
		selectedIndex,
		position,
		onselect,
		onclose
	}: {
		groups: SlashGroup[];
		selectedIndex: number;
		position: { x: number; y: number };
		onselect: (item: SlashMenuItem) => void;
		onclose: () => void;
	} = $props();

	let menuRef: HTMLElement | undefined = $state();
	let flipAbove = $state(false);

	// Build flat index for keyboard nav highlighting
	const flatItems = $derived(groups.flatMap((g) => g.items));

	// Scroll selected item into view
	$effect(() => {
		if (!menuRef || flatItems.length === 0) return;
		const item = menuRef.querySelector(`[data-index="${selectedIndex}"]`);
		item?.scrollIntoView({ block: 'nearest' });
	});

	// Flip above cursor if near viewport bottom
	$effect(() => {
		if (!menuRef) return;
		const menuHeight = menuRef.offsetHeight;
		flipAbove = position.y + menuHeight > window.innerHeight - 16;
	});

	// Close on click outside
	function handlePointerDown(e: PointerEvent) {
		if (menuRef && !menuRef.contains(e.target as Node)) {
			onclose();
		}
	}

	onMount(() => {
		window.addEventListener('pointerdown', handlePointerDown, true);
	});

	onDestroy(() => {
		window.removeEventListener('pointerdown', handlePointerDown, true);
	});

	function computedTop(): number {
		if (!flipAbove || !menuRef) return position.y;
		return position.y - menuRef.offsetHeight - 28;
	}

	let globalIdx = 0;
</script>

<div
	bind:this={menuRef}
	class="slash-command-menu"
	style="position: fixed; left: {position.x}px; top: {flipAbove ? computedTop() : position.y}px;"
>
	{#each groups as group}
		<div class="slash-group">
			{#each group.items as item}
				{@const idx = flatItems.indexOf(item)}
				<button
					type="button"
					data-index={idx}
					class="slash-item"
					class:selected={idx === selectedIndex}
					onpointerdown={(e) => { e.preventDefault(); onselect(item); }}
				>
					<span class="slash-item-icon">
						<svelte:component this={item.icon} size={16} />
					</span>
					<span class="slash-item-label">{item.label}</span>
					{#if item.shortcut}
						<span class="slash-item-shortcut">{item.shortcut}</span>
					{/if}
				</button>
			{/each}
		</div>
	{/each}
	{#if flatItems.length === 0}
		<div class="slash-empty">No results</div>
	{/if}
</div>

<style>
	.slash-command-menu {
		z-index: 100;
		width: 240px;
		max-height: 340px;
		overflow-y: auto;
		background: var(--color-bg-secondary);
		border: 1px solid var(--app-border);
		border-radius: 10px;
		padding: 4px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
	}

	.slash-group {
		padding: 2px 0;
	}

	.slash-group + .slash-group {
		border-top: 1px solid var(--app-border);
		margin-top: 2px;
		padding-top: 4px;
	}

	.slash-item {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 6px 8px;
		border: none;
		border-radius: 6px;
		background: transparent;
		color: var(--color-text-primary);
		font-size: 13px;
		cursor: pointer;
		text-align: left;
	}

	.slash-item:hover,
	.slash-item.selected {
		background: var(--color-bg-hover);
	}

	.slash-item-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		color: var(--color-text-tertiary);
		flex-shrink: 0;
	}

	.slash-item-label {
		flex: 1;
		min-width: 0;
	}

	.slash-item-shortcut {
		font-size: 11px;
		color: var(--color-text-tertiary);
		white-space: nowrap;
		margin-left: auto;
	}

	.slash-empty {
		padding: 12px;
		text-align: center;
		font-size: 12px;
		color: var(--color-text-tertiary);
	}
</style>
