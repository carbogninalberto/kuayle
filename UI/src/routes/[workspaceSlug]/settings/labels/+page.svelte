<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listLabels, createLabel, deleteLabel } from '$lib/api/labels';
	import type { Label } from '$lib/types/label';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2 } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let labels = $state<Label[]>([]);
	let showForm = $state(false);
	let newName = $state('');
	let newColor = $state('#6366f1');

	onMount(async () => {
		labels = await listLabels(slug);
	});

	async function handleCreate(e: Event) {
		e.preventDefault();
		try {
			const label = await createLabel(slug, { name: newName, color: newColor });
			labels = [...labels, label];
			newName = '';
			showForm = false;
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create label');
		}
	}

	async function handleDelete(id: string) {
		await deleteLabel(slug, id);
		labels = labels.filter((l) => l.id !== id);
	}
</script>

<div class="h-full">
	<div
		class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6"
	>
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Labels</h1>
		<button
			onclick={() => (showForm = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Label
		</button>
	</div>

	{#if showForm}
		<form
			onsubmit={handleCreate}
			class="flex items-center gap-2 border-b border-[var(--app-border)] px-6 py-3"
		>
			<input
				type="color"
				bind:value={newColor}
				class="h-8 w-8 cursor-pointer rounded border-0"
			/>
			<input
				type="text"
				bind:value={newName}
				placeholder="Label name"
				autofocus
				class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none"
			/>
			<button
				type="submit"
				class="rounded bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white">Create</button
			>
			<button
				type="button"
				onclick={() => (showForm = false)}
				class="rounded border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)]"
				>Cancel</button
			>
		</form>
	{/if}

	<div class="divide-y divide-[var(--app-border)]">
		{#each labels as label}
			<div class="flex items-center justify-between px-6 py-3">
				<div class="flex items-center gap-3">
					<div class="h-3 w-3 rounded-full" style="background-color: {label.color}"></div>
					<span class="text-sm text-[var(--color-text-primary)]">{label.name}</span>
				</div>
				<button
					onclick={() => handleDelete(label.id)}
					class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
				>
					<Trash2 size={14} />
				</button>
			</div>
		{/each}
	</div>
</div>
