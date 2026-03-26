<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { MentionItem } from './mention.extension';
	import { CircleDot } from 'lucide-svelte';

	let {
		items,
		selectedIndex,
		position,
		onselect,
		onclose
	}: {
		items: MentionItem[];
		selectedIndex: number;
		position: { x: number; y: number };
		onselect: (item: MentionItem) => void;
		onclose: () => void;
	} = $props();

	let menuRef: HTMLElement | undefined = $state();
	let flipAbove = $state(false);

	const users = $derived(items.filter((i) => i.kind === 'user'));
	const issues = $derived(items.filter((i) => i.kind === 'issue'));

	$effect(() => {
		if (!menuRef || items.length === 0) return;
		const item = menuRef.querySelector(`[data-index="${selectedIndex}"]`);
		item?.scrollIntoView({ block: 'nearest' });
	});

	$effect(() => {
		if (!menuRef) return;
		const menuHeight = menuRef.offsetHeight;
		flipAbove = position.y + menuHeight > window.innerHeight - 16;
	});

	function computedTop(): number {
		if (!flipAbove || !menuRef) return position.y;
		return position.y - menuRef.offsetHeight - 28;
	}

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

	// Global index for keyboard nav across both sections
	function globalIndex(item: MentionItem): number {
		return items.indexOf(item);
	}
</script>

<div
	bind:this={menuRef}
	class="mention-menu"
	style="position: fixed; left: {position.x}px; top: {flipAbove ? computedTop() : position.y}px;"
>
	{#if users.length > 0}
		{#if issues.length > 0}
			<div class="mention-section-label">Members</div>
		{/if}
		{#each users as item (item.id)}
			{@const idx = globalIndex(item)}
			<button
				type="button"
				data-index={idx}
				class="mention-item"
				class:selected={idx === selectedIndex}
				onpointerdown={(e) => { e.preventDefault(); onselect(item); }}
			>
				<div class="mention-avatar">
					{(item.kind === 'user' ? item.name || item.email : '').charAt(0).toUpperCase()}
				</div>
				<div class="mention-info">
					<span class="mention-name">{item.kind === 'user' ? (item.name || item.email) : ''}</span>
					{#if item.kind === 'user' && item.name}
						<span class="mention-email">{item.email}</span>
					{/if}
				</div>
			</button>
		{/each}
	{/if}

	{#if issues.length > 0}
		{#if users.length > 0}
			<div class="mention-separator"></div>
		{/if}
		<div class="mention-section-label">Issues</div>
		{#each issues as item (item.id)}
			{@const idx = globalIndex(item)}
			<button
				type="button"
				data-index={idx}
				class="mention-item"
				class:selected={idx === selectedIndex}
				onpointerdown={(e) => { e.preventDefault(); onselect(item); }}
			>
				<div class="mention-issue-icon">
					<CircleDot size={14} />
				</div>
				<div class="mention-info">
					<span class="mention-name">
						{#if item.kind === 'issue'}
							<span class="mention-identifier">{item.identifier}</span>
							{item.title}
						{/if}
					</span>
				</div>
			</button>
		{/each}
	{/if}

	{#if items.length === 0}
		<div class="mention-empty">No results</div>
	{/if}
</div>

<style>
	.mention-menu {
		z-index: 100;
		width: 280px;
		max-height: 280px;
		overflow-y: auto;
		background: var(--color-bg-secondary);
		border: 1px solid var(--app-border);
		border-radius: 10px;
		padding: 4px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
	}

	.mention-section-label {
		padding: 4px 8px 2px;
		font-size: 10px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-tertiary);
	}

	.mention-separator {
		height: 1px;
		background: var(--app-border);
		margin: 4px 0;
	}

	.mention-item {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 5px 8px;
		border: none;
		border-radius: 6px;
		background: transparent;
		color: var(--color-text-primary);
		font-size: 13px;
		cursor: pointer;
		text-align: left;
	}

	.mention-item:hover,
	.mention-item.selected {
		background: var(--color-bg-hover);
	}

	.mention-avatar {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		background: var(--app-accent);
		color: var(--app-accent-foreground);
		font-size: 10px;
		font-weight: 600;
		flex-shrink: 0;
	}

	.mention-issue-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		color: var(--color-text-tertiary);
		flex-shrink: 0;
	}

	.mention-info {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}

	.mention-name {
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.mention-identifier {
		color: var(--app-accent-light, var(--app-accent));
		margin-right: 4px;
		font-weight: 600;
	}

	.mention-email {
		font-size: 11px;
		color: var(--color-text-tertiary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.mention-empty {
		padding: 12px;
		text-align: center;
		font-size: 12px;
		color: var(--color-text-tertiary);
	}
</style>
