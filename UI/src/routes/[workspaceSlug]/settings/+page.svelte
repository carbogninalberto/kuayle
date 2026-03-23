<script lang="ts">
	import { page } from '$app/state';
	import { getWorkspace, updateWorkspace } from '$lib/api/workspaces';
	import type { Workspace } from '$lib/types/workspace';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let workspace = $state<Workspace | null>(null);
	let wsName = $state('');

	onMount(async () => {
		workspace = await getWorkspace(slug);
		wsName = workspace.name;
	});

	async function handleNameBlur() {
		if (!workspace || wsName.trim() === workspace.name) return;
		if (!wsName.trim()) {
			wsName = workspace.name;
			return;
		}
		try {
			workspace = await updateWorkspace(slug, { name: wsName.trim() });
			wsName = workspace.name;
			toast.success('Workspace name updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update workspace name');
			wsName = workspace.name;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">General</h1>

	{#if workspace}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Workspace name</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The name of your workspace visible to all members.</p>
				</div>
				<input
					type="text"
					bind:value={wsName}
					onblur={handleNameBlur}
					class="w-[200px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>
			<div class="border-t border-[var(--app-border)]"></div>
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Workspace URL</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The unique identifier for your workspace.</p>
				</div>
				<span class="text-sm text-[var(--color-text-tertiary)]">{workspace.slug}</span>
			</div>
		</div>
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>
