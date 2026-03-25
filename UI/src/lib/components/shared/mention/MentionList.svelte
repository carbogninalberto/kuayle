<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { MentionUser } from './mention.extension';

	let {
		users,
		selectedIndex,
		position,
		onselect,
		onclose
	}: {
		users: MentionUser[];
		selectedIndex: number;
		position: { x: number; y: number };
		onselect: (user: MentionUser) => void;
		onclose: () => void;
	} = $props();

	let menuRef: HTMLElement | undefined = $state();
	let flipAbove = $state(false);

	$effect(() => {
		if (!menuRef || users.length === 0) return;
		const item = menuRef.querySelector(`[data-index="${selectedIndex}"]`);
		item?.scrollIntoView({ block: 'nearest' });
	});

	// Flip above cursor if near viewport bottom
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
</script>

<div
	bind:this={menuRef}
	class="mention-menu"
	style="position: fixed; left: {position.x}px; top: {flipAbove ? computedTop() : position.y}px;"
>
	{#each users as user, i (user.id)}
		<button
			type="button"
			data-index={i}
			class="mention-item"
			class:selected={i === selectedIndex}
			onpointerdown={(e) => { e.preventDefault(); onselect(user); }}
		>
			<div class="mention-avatar">
				{(user.name || user.email).charAt(0).toUpperCase()}
			</div>
			<div class="mention-info">
				<span class="mention-name">{user.name || user.email}</span>
				{#if user.name}
					<span class="mention-email">{user.email}</span>
				{/if}
			</div>
		</button>
	{/each}
	{#if users.length === 0}
		<div class="mention-empty">No users found</div>
	{/if}
</div>

<style>
	.mention-menu {
		z-index: 100;
		width: 240px;
		max-height: 240px;
		overflow-y: auto;
		background: var(--color-bg-secondary);
		border: 1px solid var(--app-border);
		border-radius: 10px;
		padding: 4px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
	}

	.mention-item {
		display: flex;
		align-items: center;
		gap: 8px;
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

	.mention-item:hover,
	.mention-item.selected {
		background: var(--color-bg-hover);
	}

	.mention-avatar {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		background: var(--app-accent);
		color: var(--app-accent-foreground);
		font-size: 11px;
		font-weight: 600;
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
