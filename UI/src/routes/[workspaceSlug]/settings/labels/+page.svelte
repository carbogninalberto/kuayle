<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listLabels, createLabel, updateLabel, deleteLabel } from '$lib/api/labels';
	import type { Label } from '$lib/types/label';
	import LabelDialog from '$lib/features/labels/LabelDialog.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { appToast } from '$lib/features/toast/toast';
	import { Plus, Trash2, Pencil, MoreHorizontal } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let labels = $state<Label[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);
	let editingLabel = $state<Label | null>(null);
	let showEdit = $state(false);
	let menuOpenId = $state<string | null>(null);

	onMount(async () => {
		try {
			labels = await listLabels(slug);
		} finally {
			loading = false;
		}
	});

	async function handleCreate(data: { name: string; color: string; description?: string }) {
		try {
			const label = await createLabel(slug, data);
			labels = [...labels, label];
			appToast.success('Label created');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to create label');
		}
	}

	async function handleEdit(data: { name: string; color: string; description?: string }) {
		if (!editingLabel) return;
		try {
			const updated = await updateLabel(slug, editingLabel.id, data);
			labels = labels.map((l) => (l.id === editingLabel!.id ? updated : l));
			editingLabel = null;
			appToast.success('Label updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update label');
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteLabel(slug, id);
			labels = labels.filter((l) => l.id !== id);
			appToast.success('Label deleted');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete label');
		}
	}

	function openEdit(label: Label) {
		editingLabel = label;
		menuOpenId = null;
		showEdit = true;
	}
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Labels</h1>
		<button
			onclick={() => (showCreate = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Label
		</button>
	</div>

	<div class="mt-8">
		{#if loading}
			<div class="flex h-64 items-center justify-center">
			</div>
		{:else if labels.length === 0}
			<EmptyState
				title="No labels yet"
				description="Create labels to categorize your issues"
				action={{ label: 'New Label', onclick: () => (showCreate = true) }}
			/>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
				{#each labels as label, i}
					<div class="group flex items-center justify-between px-5 py-3.5 {i > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<div class="flex items-center gap-3">
							<div class="h-3.5 w-3.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
							<div>
								<span class="text-sm text-[var(--color-text-primary)]">{label.name}</span>
								{#if label.description}
									<p class="text-xs text-[var(--color-text-tertiary)]">{label.description}</p>
								{/if}
							</div>
						</div>
						<div class="flex items-center gap-1 opacity-0 group-hover:opacity-100">
							<Popover.Root open={menuOpenId === label.id} onOpenChange={(open) => { menuOpenId = open ? label.id : null; }}>
								<Popover.Trigger>
									<Button variant="ghost" size="icon-sm" class="h-7 w-7">
										<MoreHorizontal size={14} />
									</Button>
								</Popover.Trigger>
								<Popover.Content class="w-36 p-1" align="end">
									<button
										onclick={() => openEdit(label)}
										class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
									>
										<Pencil size={13} />
										Edit
									</button>
									<button
										onclick={() => { menuOpenId = null; handleDelete(label.id); }}
										class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
									>
										<Trash2 size={13} />
										Delete
									</button>
								</Popover.Content>
							</Popover.Root>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<LabelDialog
	bind:open={showCreate}
	mode="create"
	onsubmit={handleCreate}
/>

<LabelDialog
	bind:open={showEdit}
	mode="edit"
	initialName={editingLabel?.name ?? ''}
	initialColor={editingLabel?.color ?? '#6366f1'}
	initialDescription={editingLabel?.description ?? ''}
	onsubmit={handleEdit}
/>
