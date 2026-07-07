<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getWorkspace, updateWorkspace, deleteWorkspace } from '$lib/api/workspaces';
	import type { Workspace } from '$lib/types/workspace';
	import { onMount } from 'svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Trash2 } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let workspace = $state<Workspace | null>(null);
	let wsName = $state('');
	let logoUrl = $state('');
	let shareLinkMinRole = $state('admin');
	let savingName = $state(false);
	let savingLogo = $state(false);
	let savingRole = $state(false);
	let showDelete = $state(false);
	let deleteConfirm = $state('');
	let deleting = $state(false);

	const minRoleOptions = [
		{ value: 'owner', label: 'Owner' },
		{ value: 'admin', label: 'Admin' },
		{ value: 'member', label: 'Member' }
	];

	onMount(async () => {
		workspace = await getWorkspace(slug);
		wsName = workspace.name;
		logoUrl = workspace.logo_url ?? '';
		shareLinkMinRole = workspace.share_link_min_role ?? 'admin';
	});

	const isOwner = $derived(
		workspace?.current_user_role === 'owner' ||
			(workspace?.owner_id && authState.user?.id === workspace.owner_id) ||
			false
	);

	async function handleNameBlur() {
		if (!workspace || wsName.trim() === workspace.name) return;
		if (!wsName.trim()) {
			wsName = workspace.name;
			return;
		}
		savingName = true;
		try {
			workspace = await updateWorkspace(slug, { name: wsName.trim() });
			wsName = workspace.name;
			appToast.success('Workspace name updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update workspace name');
			wsName = workspace.name;
		} finally {
			savingName = false;
		}
	}

	async function handleLogoBlur() {
		if (!workspace) return;
		const next = logoUrl.trim() === '' ? null : logoUrl.trim();
		if ((workspace.logo_url ?? null) === next) return;
		savingLogo = true;
		try {
			workspace = await updateWorkspace(slug, { logo_url: next });
			logoUrl = workspace.logo_url ?? '';
			appToast.success('Workspace logo updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update workspace logo');
			logoUrl = workspace.logo_url ?? '';
		} finally {
			savingLogo = false;
		}
	}

	async function handleRoleChange(value: string) {
		if (!workspace || value === workspace.share_link_min_role) return;
		savingRole = true;
		try {
			workspace = await updateWorkspace(slug, { share_link_min_role: value });
			shareLinkMinRole = workspace.share_link_min_role;
			appToast.success('Shared link minimum role updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update minimum role');
			shareLinkMinRole = workspace.share_link_min_role ?? 'admin';
		} finally {
			savingRole = false;
		}
	}

	async function handleDelete() {
		if (deleteConfirm !== workspace?.slug) {
			appToast.error('Type the workspace slug to confirm deletion');
			return;
		}
		deleting = true;
		try {
			await deleteWorkspace(slug);
			appToast.success('Workspace deleted');
			showDelete = false;
			goto('/');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete workspace');
		} finally {
			deleting = false;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">General</h1>

	{#if workspace}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<!-- Workspace name -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Workspace name</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The name of your workspace visible to all members.</p>
				</div>
				<input
					type="text"
					bind:value={wsName}
					onblur={handleNameBlur}
					disabled={!isOwner || savingName}
					class="w-[200px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] disabled:cursor-not-allowed disabled:opacity-60"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Workspace URL -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Workspace URL</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The unique identifier for your workspace.</p>
				</div>
				<span class="text-sm text-[var(--color-text-tertiary)]">{workspace.slug}</span>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Logo URL -->
			<div class="flex items-center justify-between px-5 py-4">
				<div class="min-w-0">
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Logo URL</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Public URL to an image used as the workspace logo.</p>
				</div>
				<input
					type="url"
					bind:value={logoUrl}
					onblur={handleLogoBlur}
					placeholder="https://"
					disabled={!isOwner || savingLogo}
					class="w-[240px] rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)] disabled:cursor-not-allowed disabled:opacity-60"
				/>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Owner -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Owner</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">The user who owns this workspace.</p>
				</div>
				<div class="flex items-center gap-2">
					<div class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--app-accent)] text-[10px] font-medium text-[var(--app-accent-foreground)]">
						{(workspace.owner?.name ?? workspace.owner?.email ?? 'U').charAt(0).toUpperCase()}
					</div>
					<span class="text-sm text-[var(--color-text-secondary)]">
						{workspace.owner?.name ?? workspace.owner?.email ?? 'Unknown'}
					</span>
				</div>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Shared link min role -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">Shared link minimum role</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Minimum role required to create shared links.</p>
				</div>
				<Select.Root
					type="single"
					value={shareLinkMinRole}
					disabled={!isOwner || savingRole}
					onValueChange={(v) => v && handleRoleChange(v)}
				>
					<Select.Trigger size="sm" class="w-[140px]">
						{minRoleOptions.find((o) => o.value === shareLinkMinRole)?.label ?? shareLinkMinRole}
					</Select.Trigger>
					<Select.Content>
						{#each minRoleOptions as opt}
							<Select.Item value={opt.value}>{opt.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		</div>

		{#if !isOwner}
			<p class="mt-3 text-xs text-[var(--color-text-tertiary)]">
				Only the workspace owner can edit these settings.
			</p>
		{/if}

		<!-- Danger zone -->
		{#if isOwner}
			<div class="mt-10">
				<h2 class="text-base font-medium text-[var(--color-text-primary)]">Danger zone</h2>
				<div class="mt-3 rounded-lg border border-red-500/30 bg-[var(--color-bg-secondary)]">
					<div class="flex items-center justify-between px-5 py-4">
						<div>
							<p class="text-sm font-medium text-[var(--color-text-primary)]">Delete workspace</p>
							<p class="text-xs text-[var(--color-text-tertiary)]">
								Permanently delete this workspace, its teams, projects, and issues. This action cannot be undone.
							</p>
						</div>
						<Button variant="destructive" onclick={() => (showDelete = true)}>
							<Trash2 size={14} />
							Delete workspace
						</Button>
					</div>
				</div>
			</div>
		{/if}
	{:else}
		<div class="mt-8 flex justify-center py-8">
			<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
		</div>
	{/if}
</div>

<Dialog.Root bind:open={showDelete}>
	<Dialog.Content class="sm:max-w-md border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<Dialog.Header>
			<Dialog.Title>Delete workspace</Dialog.Title>
			<Dialog.Description>
				This will permanently delete <span class="font-medium text-[var(--color-text-primary)]">{workspace?.name}</span> and all of its teams, projects, issues, and members.
			</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-2 py-4">
			<label for="delete-confirm" class="block text-sm text-[var(--color-text-secondary)]">
				Type <span class="font-mono text-[var(--color-text-primary)]">{workspace?.slug}</span> to confirm
			</label>
			<input
				id="delete-confirm"
				type="text"
				bind:value={deleteConfirm}
				placeholder={workspace?.slug}
				class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
			/>
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (showDelete = false)} disabled={deleting}>Cancel</Button>
			<Button
				variant="destructive"
				onclick={handleDelete}
				disabled={deleting || deleteConfirm !== workspace?.slug}
			>
				{deleting ? 'Deleting...' : 'Delete workspace'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
