<script lang="ts">
	import { page } from '$app/state';
	import type { TeamStatus, StatusCategory } from '$lib/types/team-status';
	import { CATEGORY_ORDER, CATEGORY_LABELS } from '$lib/types/team-status';
	import type { Team } from '$lib/types/team';
	import { listTeams } from '$lib/api/teams';
	import { listTeamStatuses, createTeamStatus, updateTeamStatus, deleteTeamStatus } from '$lib/api/team-statuses';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import * as Select from '$lib/components/ui/select';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2, Lock, Pencil, X, Check, GripVertical } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let team = $state<Team | null>(null);
	let statuses = $state<TeamStatus[]>([]);
	let loading = $state(true);

	// Add status form per category
	let addingCategory = $state<StatusCategory | null>(null);
	let addName = $state('');
	let addColor = $state('');

	// Edit status
	let editingId = $state<string | null>(null);
	let editName = $state('');
	let editColor = $state('');

	// Drag state
	let dragStatusId = $state<string | null>(null);
	let dragOverStatusId = $state<string | null>(null);
	let dragCategory = $state<StatusCategory | null>(null);

	$effect(() => {
		const s = slug;
		const t = teamId;
		if (!s || !t) return;
		loading = true;
		editingId = null;
		addingCategory = null;
		Promise.all([listTeams(s), listTeamStatuses(s, t)]).then(([teams, st]) => {
			team = teams.find((tm) => tm.id === t) ?? null;
			statuses = st;
			loading = false;
		});
	});

	async function loadStatuses() {
		statuses = await listTeamStatuses(slug, teamId);
	}

	async function handleAdd() {
		if (!addName.trim() || !addingCategory) return;
		try {
			await createTeamStatus(slug, teamId, {
				name: addName.trim(),
				category: addingCategory,
				color: addColor || undefined,
			});
			addName = '';
			addColor = '';
			addingCategory = null;
			await loadStatuses();
			toast.success('Status created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create status');
		}
	}

	function startEdit(status: TeamStatus) {
		editingId = status.id;
		editName = status.name;
		editColor = status.color ?? '';
	}

	async function saveEdit() {
		if (!editingId || !editName.trim()) return;
		try {
			await updateTeamStatus(slug, teamId, editingId, {
				name: editName.trim(),
				color: editColor || undefined,
			});
			editingId = null;
			await loadStatuses();
			toast.success('Status updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update status');
		}
	}

	async function handleDelete(statusId: string) {
		try {
			await deleteTeamStatus(slug, teamId, statusId);
			await loadStatuses();
			toast.success('Status deleted');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete status');
		}
	}

	function statusesByCategory(cat: StatusCategory): TeamStatus[] {
		return statuses
			.filter((s) => s.category === cat)
			.sort((a, b) => a.position - b.position);
	}

	function startAdd(cat: StatusCategory) {
		addingCategory = cat;
		addName = '';
		addColor = '';
	}

	// Drag and drop handlers
	function handleDragStart(e: DragEvent, status: TeamStatus) {
		dragStatusId = status.id;
		dragCategory = status.category;
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('text/plain', status.id);
		}
	}

	function handleDragOver(e: DragEvent, status: TeamStatus) {
		// Only allow reorder within the same category
		if (dragCategory !== status.category) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		dragOverStatusId = status.id;
	}

	function handleDragLeave() {
		dragOverStatusId = null;
	}

	function handleDragEnd() {
		dragStatusId = null;
		dragOverStatusId = null;
		dragCategory = null;
	}

	async function handleDrop(e: DragEvent, targetStatus: TeamStatus) {
		e.preventDefault();
		if (!dragStatusId || dragStatusId === targetStatus.id) {
			handleDragEnd();
			return;
		}

		const cat = targetStatus.category;
		const catStatuses = statusesByCategory(cat);
		const dragIdx = catStatuses.findIndex((s) => s.id === dragStatusId);
		const targetIdx = catStatuses.findIndex((s) => s.id === targetStatus.id);

		if (dragIdx === -1 || targetIdx === -1) {
			handleDragEnd();
			return;
		}

		// Reorder: remove dragged item, insert at target position
		const reordered = [...catStatuses];
		const [moved] = reordered.splice(dragIdx, 1);
		reordered.splice(targetIdx, 0, moved);

		// Optimistically update local state
		const otherStatuses = statuses.filter((s) => s.category !== cat);
		const updatedCat = reordered.map((s, i) => ({ ...s, position: i }));
		statuses = [...otherStatuses, ...updatedCat];

		handleDragEnd();

		// Persist new positions
		try {
			await Promise.all(
				updatedCat.map((s, i) =>
					updateTeamStatus(slug, teamId, s.id, { position: i })
				)
			);
		} catch {
			toast.error('Failed to reorder statuses');
			await loadStatuses();
		}
	}

	const PRESET_COLORS = ['#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899'];
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Issue statuses</h1>
	</div>
	<div class="max-w-2xl p-6 space-y-2">
		<p class="text-xs text-[var(--color-text-tertiary)] mb-4">
			Issue statuses define the workflow that issues go through from start to completion.
		</p>

		{#if loading}
			<div class="flex justify-center py-8">
				<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
			</div>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] overflow-hidden">
				{#each CATEGORY_ORDER as cat, catIdx}
					{@const catStatuses = statusesByCategory(cat)}

					<!-- Category header -->
					<div class="flex items-center justify-between bg-[var(--color-bg-secondary)] px-4 py-2 {catIdx > 0 ? 'border-t border-[var(--app-border)]' : ''}">
						<span class="text-xs font-medium text-[var(--color-text-tertiary)]">{CATEGORY_LABELS[cat]}</span>
						<button
							onclick={() => startAdd(cat)}
							class="rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors"
							title="Add status to {CATEGORY_LABELS[cat]}"
						>
							<Plus size={14} />
						</button>
					</div>

					<!-- Statuses in this category -->
					{#each catStatuses as status (status.id)}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="flex items-center gap-1 px-1 py-0 border-t border-[var(--app-border)] transition-colors
								{dragOverStatusId === status.id && dragStatusId !== status.id ? 'border-t-2 border-t-[var(--app-accent)]' : ''}
								{dragStatusId === status.id ? 'opacity-40' : 'hover:bg-[var(--color-bg-hover)]/50'}"
							draggable={editingId !== status.id}
							ondragstart={(e) => handleDragStart(e, status)}
							ondragover={(e) => handleDragOver(e, status)}
							ondragleave={handleDragLeave}
							ondragend={handleDragEnd}
							ondrop={(e) => handleDrop(e, status)}
						>
							<!-- Drag handle -->
							<span class="shrink-0 cursor-grab text-[var(--color-text-tertiary)] opacity-0 hover:opacity-100 transition-opacity group-hover:opacity-50 {dragStatusId ? 'opacity-50' : ''}" style="cursor: grab;">
								<GripVertical size={14} />
							</span>

							<div class="flex flex-1 items-center gap-3 px-2 py-2.5 group">
								<IssueStatusIcon category={status.category} color={status.color} size={18} />

								{#if editingId === status.id}
									<div class="flex flex-1 items-center gap-2">
										<input
											type="text"
											bind:value={editName}
											onkeydown={(e) => { if (e.key === 'Enter') saveEdit(); if (e.key === 'Escape') editingId = null; }}
											class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-0.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
										/>
										<div class="flex items-center gap-0.5">
											{#each PRESET_COLORS as c}
												<button
													onclick={() => editColor = c}
													class="h-3.5 w-3.5 rounded-full {editColor === c ? 'ring-2 ring-[var(--app-accent)] ring-offset-1 ring-offset-[var(--color-bg)]' : ''}"
													style="background-color: {c}"
												></button>
											{/each}
										</div>
										<button onclick={saveEdit} class="rounded p-0.5 text-[var(--color-success)] hover:bg-[var(--color-bg-tertiary)]">
											<Check size={14} />
										</button>
										<button onclick={() => editingId = null} class="rounded p-0.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)]">
											<X size={14} />
										</button>
									</div>
								{:else}
									<div class="flex-1 min-w-0">
										<div class="flex items-center gap-2">
											<span class="text-sm font-medium text-[var(--color-text-primary)]">{status.name}</span>
											{#if status.is_default}
												<span class="text-[10px] text-[var(--color-text-tertiary)]">· Default</span>
											{/if}
										</div>
									</div>
									<div class="flex items-center gap-1">
										<button
											onclick={() => startEdit(status)}
											class="hidden rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-tertiary)] group-hover:block transition-colors"
										>
											<Pencil size={12} />
										</button>
										{#if !status.is_default}
											<button
												onclick={() => handleDelete(status.id)}
												class="hidden rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-error)] hover:bg-[var(--color-bg-tertiary)] group-hover:block transition-colors"
											>
												<Trash2 size={12} />
											</button>
										{/if}
									</div>
								{/if}
							</div>
						</div>
					{/each}

					<!-- Inline add form for this category -->
					{#if addingCategory === cat}
						<div class="flex items-center gap-3 px-4 py-2.5 border-t border-[var(--app-border)] bg-[var(--color-bg-hover)]/30">
							<IssueStatusIcon category={cat} size={18} />
							<input
								type="text"
								bind:value={addName}
								placeholder="Status name..."
								onkeydown={(e) => { if (e.key === 'Enter') handleAdd(); if (e.key === 'Escape') addingCategory = null; }}
								class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-1 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
							/>
							<div class="flex items-center gap-0.5">
								{#each PRESET_COLORS as c}
									<button
										onclick={() => addColor = c}
										class="h-3.5 w-3.5 rounded-full {addColor === c ? 'ring-2 ring-[var(--app-accent)] ring-offset-1 ring-offset-[var(--color-bg)]' : ''}"
										style="background-color: {c}"
									></button>
								{/each}
							</div>
							<button
								onclick={handleAdd}
								disabled={!addName.trim()}
								class="rounded-md bg-[var(--app-accent)] px-2.5 py-1 text-xs text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
							>
								Add
							</button>
							<button onclick={() => addingCategory = null} class="rounded p-0.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-tertiary)]">
								<X size={14} />
							</button>
						</div>
					{/if}
				{/each}
			</div>
		{/if}
	</div>
</div>
