<script lang="ts">
	import { page } from '$app/state';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { Workspace } from '$lib/types/workspace';
	import { onMount } from 'svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let workspace = $state<Workspace | null>(null);

	onMount(async () => {
		workspace = await getWorkspace(slug);
	});
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">General</h1>
	</div>
	<div class="max-w-xl p-6 space-y-6">
		{#if workspace}
			<div>
				<label for="ws-name" class="mb-1.5 block text-sm font-medium text-[var(--color-text-primary)]">Workspace name</label>
				<input
					id="ws-name"
					type="text"
					value={workspace.name}
					disabled
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none"
				/>
				<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">The name of your workspace visible to all members.</p>
			</div>
			<div>
				<label for="ws-slug" class="mb-1.5 block text-sm font-medium text-[var(--color-text-primary)]">Workspace URL</label>
				<input
					id="ws-slug"
					type="text"
					value={workspace.slug}
					disabled
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-tertiary)] outline-none"
				/>
			</div>
		{:else}
			<p class="text-sm text-[var(--color-text-tertiary)]">Loading...</p>
		{/if}
	</div>
</div>
