<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Workspace } from '$lib/types/workspace';
	import { listWorkspaces } from '$lib/api/workspaces';
	import * as Popover from '$lib/components/ui/popover';
	import { Plus, ChevronsUpDown, Check } from 'lucide-svelte';
	import { onMount } from 'svelte';

	let {
		currentWorkspace,
		slug
	}: {
		currentWorkspace: Workspace;
		slug: string;
	} = $props();

	let open = $state(false);
	let workspaces = $state<Workspace[]>([]);

	onMount(async () => {
		workspaces = await listWorkspaces();
	});

	function switchWorkspace(ws: Workspace) {
		open = false;
		if (ws.slug !== slug) {
			localStorage.setItem('kuayle_last_workspace', ws.slug);
			goto(`/${ws.slug}/dashboard`);
		}
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		<button class="flex w-full items-center gap-2 rounded-md px-1 py-0.5 hover:bg-[var(--color-bg-hover)]">
			<div
				class="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-[var(--app-accent)] text-xs font-bold text-[var(--app-accent-foreground)]"
			>
				{currentWorkspace.name.charAt(0).toUpperCase()}
			</div>
			<span class="flex-1 truncate text-left text-sm font-medium text-[var(--color-text-primary)]">
				{currentWorkspace.name}
			</span>
			<ChevronsUpDown size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
		</button>
	</Popover.Trigger>
	<Popover.Content class="w-56 p-1" align="start">
		<div class="px-2 py-1">
			<span class="text-[10px] font-medium uppercase text-[var(--color-text-tertiary)]">Workspaces</span>
		</div>
		{#each workspaces as ws}
			<button
				onclick={() => switchWorkspace(ws)}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
			>
				<div
					class="flex h-5 w-5 items-center justify-center rounded bg-[var(--app-accent)] text-[9px] font-bold text-[var(--app-accent-foreground)]"
				>
					{ws.name.charAt(0).toUpperCase()}
				</div>
				<span class="flex-1 truncate text-left">{ws.name}</span>
				{#if ws.slug === slug}
					<Check size={14} class="text-[var(--app-accent)]" />
				{/if}
			</button>
		{/each}
		<div class="mt-1 border-t border-[var(--app-border)] pt-1">
			<button
				onclick={() => { open = false; goto('/create-workspace'); }}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				<Plus size={14} />
				Create workspace
			</button>
		</div>
	</Popover.Content>
</Popover.Root>
