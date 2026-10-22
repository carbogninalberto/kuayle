<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Plus, Loader2 } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import WorkspaceImportDialog from '$lib/features/workspaces/WorkspaceImportDialog.svelte';
	import { createWorkspace, listWorkspaces } from '$lib/api/workspaces';
	import { appToast } from '$lib/features/toast/toast';
	import { toSlug } from '$lib/utils/slug';
	import { m } from '$lib/paraglide/messages.js';

	let name = $state('');
	let slug = $state('');
	let slugTouched = $state(false);
	let creating = $state(false);
	let ready = $state(false);

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			await goto('/login');
			return;
		}
		const workspaces = await listWorkspaces();
		if (workspaces.length > 0) {
			await goto(`/${workspaces[0].slug}/inbox`);
			return;
		}
		name = authState.user?.name ? `${authState.user.name}'s Workspace` : '';
		slug = toSlug(authState.user?.name ?? 'workspace');
		ready = true;
	});

	function updateName(value: string) {
		name = value;
		if (!slugTouched) slug = toSlug(value);
	}

	async function create() {
		if (!name.trim() || !slug.trim()) return;
		creating = true;
		try {
			const workspace = await createWorkspace(name.trim(), slug.trim());
			await goto(`/${workspace.slug}/inbox`);
		} catch (error: any) {
			appToast.apiError(error, m['workspace_setup.create_failed']());
		} finally {
			creating = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-[var(--color-bg)] px-6 py-12">
	{#if ready}
		<div class="w-full max-w-2xl">
			<div class="text-center">
				<img src="/favicon.svg" alt="Kuayle" class="mx-auto mb-4 h-12 w-12" />
				<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">{m['workspace_setup.title']()}</h1>
				<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">{m['workspace_setup.description']()}</p>
			</div>
			<div class="mt-8 grid gap-4 md:grid-cols-2">
				<section class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
					<div class="flex items-center gap-2">
						<Plus size={17} class="text-[var(--app-accent-light)]" />
						<h2 class="font-medium text-[var(--color-text-primary)]">{m['workspace_setup.create_title']()}</h2>
					</div>
					<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">{m['workspace_setup.create_description']()}</p>
					<div class="mt-4 space-y-3">
						<label class="block text-sm text-[var(--color-text-secondary)]">
							{m['workspace_transfer.workspace_name']()}
							<Input value={name} oninput={(event) => updateName(event.currentTarget.value)} maxlength={100} class="mt-1 w-full" />
						</label>
						<label class="block text-sm text-[var(--color-text-secondary)]">
							{m['workspace_transfer.workspace_slug']()}
							<Input bind:value={slug} oninput={() => (slugTouched = true)} maxlength={50} class="mt-1 w-full font-mono" />
						</label>
						<Button class="w-full" onclick={create} disabled={creating || !name.trim() || !slug.trim()}>
							{#if creating}<Loader2 size={14} class="animate-spin" />{/if}
							{m['workspace_setup.create_button']()}
						</Button>
					</div>
				</section>

				<section class="flex flex-col rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5">
					<h2 class="font-medium text-[var(--color-text-primary)]">{m['workspace_setup.import_title']()}</h2>
					<p class="mt-1 flex-1 text-xs text-[var(--color-text-tertiary)]">{m['workspace_setup.import_description']()}</p>
					<div class="mt-4"><WorkspaceImportDialog /></div>
				</section>
			</div>
		</div>
	{:else}
		<Loader2 size={20} class="animate-spin text-[var(--color-text-tertiary)]" />
	{/if}
</div>
