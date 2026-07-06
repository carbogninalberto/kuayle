<script lang="ts">
	import { goto } from '$app/navigation';
	import type { Workspace } from '$lib/types/workspace';
	import { createWorkspace, listWorkspaces } from '$lib/api/workspaces';
	import * as Popover from '$lib/components/ui/popover';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Plus, ChevronsUpDown, Check, Loader2 } from 'lucide-svelte';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	let {
		currentWorkspace,
		slug
	}: {
		currentWorkspace: Workspace;
		slug: string;
	} = $props();

	let open = $state(false);
	let showCreateWorkspace = $state(false);
	let workspaces = $state<Workspace[]>([]);
	let newWorkspaceName = $state('');
	let newWorkspaceSlug = $state('');
	let slugEdited = $state(false);
	let creating = $state(false);

	onMount(async () => {
		workspaces = await listWorkspaces();
	});

	function switchWorkspace(ws: Workspace) {
		open = false;
		if (ws.slug !== slug) {
			localStorage.setItem('kuayle_last_workspace', ws.slug);
			goto(`/${ws.slug}/my-issues`);
		}
	}

	function toSlug(value: string) {
		return value
			.toLowerCase()
			.replace(/[^a-z0-9]/g, '-')
			.replace(/-+/g, '-')
			.replace(/^-|-$/g, '')
			.slice(0, 50);
	}

	function handleNameInput() {
		if (!slugEdited) {
			newWorkspaceSlug = toSlug(newWorkspaceName);
		}
	}

	function openCreateWorkspace() {
		open = false;
		showCreateWorkspace = true;
	}

	async function handleCreateWorkspace(e: Event) {
		e.preventDefault();
		const workspaceName = newWorkspaceName.trim();
		const workspaceSlug = toSlug(newWorkspaceSlug || newWorkspaceName);
		if (!workspaceName || !workspaceSlug) return;

		creating = true;
		try {
			const workspace = await createWorkspace(workspaceName, workspaceSlug);
			workspaces = [...workspaces, workspace].sort((a, b) => a.name.localeCompare(b.name));
			localStorage.setItem('kuayle_last_workspace', workspace.slug);
			toast.success('Workspace created');
			showCreateWorkspace = false;
			newWorkspaceName = '';
			newWorkspaceSlug = '';
			slugEdited = false;
			goto(`/${workspace.slug}/inbox`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create workspace');
		} finally {
			creating = false;
		}
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger class="min-w-0 w-full">
		<button class="flex w-full min-w-0 items-center gap-2 rounded-md px-1 py-0.5 hover:bg-[var(--color-bg-hover)]">
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
				onclick={openCreateWorkspace}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
			>
				<Plus size={14} />
				Create workspace
			</button>
		</div>
	</Popover.Content>
</Popover.Root>

<Dialog.Root bind:open={showCreateWorkspace}>
	<Dialog.Content class="sm:max-w-md border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<Dialog.Header>
			<Dialog.Title>Create workspace</Dialog.Title>
			<Dialog.Description>Set up a new workspace for your team, projects, and issues.</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={handleCreateWorkspace} class="space-y-4 py-2">
			<div>
				<label for="workspace-name" class="mb-1 block text-sm text-[var(--color-text-secondary)]">Workspace name</label>
				<input
					id="workspace-name"
					type="text"
					bind:value={newWorkspaceName}
					oninput={handleNameInput}
					required
					maxlength="100"
					placeholder="Acme Engineering"
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
			</div>

			<div>
				<label for="workspace-slug" class="mb-1 block text-sm text-[var(--color-text-secondary)]">Workspace URL</label>
				<input
					id="workspace-slug"
					type="text"
					bind:value={newWorkspaceSlug}
					oninput={() => {
						slugEdited = true;
						newWorkspaceSlug = toSlug(newWorkspaceSlug);
					}}
					required
					maxlength="50"
					placeholder="acme-engineering"
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
				/>
				<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">This becomes the workspace URL path.</p>
			</div>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (showCreateWorkspace = false)} disabled={creating}>Cancel</Button>
				<Button type="submit" disabled={creating || !newWorkspaceName.trim() || !newWorkspaceSlug.trim()}>
					{#if creating}
						<Loader2 size={14} class="animate-spin" />
						Creating...
					{:else}
						Create workspace
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
