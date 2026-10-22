<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getWorkspace, updateWorkspace, deleteWorkspace, downloadWorkspaceExport } from '$lib/api/workspaces';
	import type { Workspace } from '$lib/types/workspace';
	import { onMount } from 'svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import { Trash2, Download, Loader2 } from '@lucide/svelte';
	import WorkspaceImportDialog from '$lib/features/workspaces/WorkspaceImportDialog.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

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
	let exporting = $state(false);

	const minRoleOptions = $derived([
		{ value: 'owner', label: m['settings.role_owner']() },
		{ value: 'admin', label: m['settings.role_admin']() },
		{ value: 'member', label: m['settings.role_member']() }
	]);

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
	const canTransfer = $derived(workspace?.current_user_role === 'owner' || workspace?.current_user_role === 'admin');

	async function handleExport() {
		exporting = true;
		try {
			await downloadWorkspaceExport(slug);
			appToast.success(m['workspace_transfer.exported']());
		} catch (err: any) {
			appToast.apiError(err, m['workspace_transfer.export_failed']());
		} finally {
			exporting = false;
		}
	}

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
			appToast.success(m['settings.general.workspace_name_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.general.failed_update_name']());
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
			appToast.success(m['settings.general.logo_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.general.failed_update_logo']());
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
			appToast.success(m['settings.general.role_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['settings.general.failed_update_role']());
			shareLinkMinRole = workspace.share_link_min_role ?? 'admin';
		} finally {
			savingRole = false;
		}
	}

	async function handleDelete() {
		if (deleteConfirm !== workspace?.slug) {
			appToast.error(m['settings.general.type_slug_confirm']({ slug: workspace?.slug ?? '' }));
			return;
		}
		deleting = true;
		try {
			await deleteWorkspace(slug);
			appToast.success(m['settings.general.workspace_deleted']());
			showDelete = false;
			goto('/');
		} catch (err: any) {
			appToast.apiError(err, m['settings.general.failed_delete']());
		} finally {
			deleting = false;
		}
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['settings.general.title']()}</h1>

	{#if workspace}
		<div class="mt-8 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<!-- Workspace name -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.workspace_name']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.general.workspace_name_desc']()}</p>
				</div>
				<Input variant="plain"
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
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.workspace_url']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.general.workspace_url_desc']()}</p>
				</div>
				<span class="text-sm text-[var(--color-text-tertiary)]">{workspace.slug}</span>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Logo URL -->
			<div class="flex items-center justify-between px-5 py-4">
				<div class="min-w-0">
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.logo_url']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.general.logo_url_desc']()}</p>
				</div>
				<Input variant="plain"
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
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.owner']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.general.owner_desc']()}</p>
				</div>
				<div class="flex items-center gap-2">
					<div class="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--app-accent)] text-[10px] font-medium text-[var(--app-accent-foreground)]">
						{(workspace.owner?.name ?? workspace.owner?.email ?? 'U').charAt(0).toUpperCase()}
					</div>
					<span class="text-sm text-[var(--color-text-secondary)]">
						{workspace.owner?.name ?? workspace.owner?.email ?? m['settings.unknown']()}
					</span>
				</div>
			</div>

			<div class="border-t border-[var(--app-border)]"></div>

			<!-- Shared link min role -->
			<div class="flex items-center justify-between px-5 py-4">
				<div>
					<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.shared_link_role']()}</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">{m['settings.general.shared_link_role_desc']()}</p>
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
				{m['settings.general.only_owner_edit']()}
			</p>
		{/if}

		{#if canTransfer}
			<div class="mt-10">
				<h2 class="text-base font-medium text-[var(--color-text-primary)]">{m['workspace_transfer.title']()}</h2>
				<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">{m['workspace_transfer.description']()}</p>
				<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
					<div class="flex items-center justify-between gap-4 px-5 py-4">
						<div>
							<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['workspace_transfer.export_title']()}</p>
							<p class="text-xs text-[var(--color-text-tertiary)]">{m['workspace_transfer.export_description']()}</p>
						</div>
						<Button variant="outline" onclick={handleExport} disabled={exporting}>
							{#if exporting}<Loader2 size={14} class="animate-spin" />{:else}<Download size={14} />{/if}
							{exporting ? m['workspace_transfer.exporting']() : m['workspace_transfer.export_button']()}
						</Button>
					</div>
					<div class="border-t border-[var(--app-border)]"></div>
					<div class="flex items-center justify-between gap-4 px-5 py-4">
						<div>
							<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['workspace_transfer.import_title']()}</p>
							<p class="text-xs text-[var(--color-text-tertiary)]">{m['workspace_transfer.import_new_description']()}</p>
						</div>
						<WorkspaceImportDialog compact />
					</div>
				</div>
			</div>
		{/if}

		<!-- Danger zone -->
		{#if isOwner}
			<div class="mt-10">
				<h2 class="text-base font-medium text-[var(--color-text-primary)]">{m['settings.general.danger_zone']()}</h2>
				<div class="mt-3 rounded-lg border border-red-500/30 bg-[var(--color-bg-secondary)]">
					<div class="flex items-center justify-between px-5 py-4">
						<div>
							<p class="text-sm font-medium text-[var(--color-text-primary)]">{m['settings.general.delete_workspace']()}</p>
							<p class="text-xs text-[var(--color-text-tertiary)]">
								{m['settings.general.delete_workspace_desc']()}
							</p>
						</div>
						<Button variant="destructive" onclick={() => (showDelete = true)}>
							<Trash2 size={14} />
							{m['settings.general.delete_workspace']()}
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
			<Dialog.Title>{m['settings.general.delete_workspace']()}</Dialog.Title>
			<Dialog.Description>
				{m['settings.general.delete_workspace_desc']()}
			</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-2 py-4">
			<label for="delete-confirm" class="block text-sm text-[var(--color-text-secondary)]">
				{m['settings.general.type_slug_confirm']({ slug: workspace?.slug ?? '' })}
			</label>
			<Input variant="plain"
				id="delete-confirm"
				type="text"
				bind:value={deleteConfirm}
				placeholder={workspace?.slug}
				class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
			/>
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (showDelete = false)} disabled={deleting}>{m['settings.cancel']()}</Button>
			<Button
				variant="destructive"
				onclick={handleDelete}
				disabled={deleting || deleteConfirm !== workspace?.slug}
			>
				{deleting ? m['settings.deleting']() : m['settings.general.delete_workspace']()}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
