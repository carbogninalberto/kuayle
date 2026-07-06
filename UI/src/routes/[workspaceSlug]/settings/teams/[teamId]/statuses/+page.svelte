<script lang="ts">
	import { flip } from 'svelte/animate';
	import { page } from '$app/state';
	import type { TeamStatus, StatusCategory } from '$lib/types/team-status';
	import { CATEGORY_ORDER, CATEGORY_LABELS } from '$lib/types/team-status';
	import type { Team } from '$lib/types/team';
	import { preferencesState, type TeamWorkflowSortMode } from '$lib/features/preferences/preferences.state.svelte';
	import { listTeams, updateTeam } from '$lib/api/teams';
	import { listTeamStatuses, createTeamStatus, updateTeamStatus, deleteTeamStatus } from '$lib/api/team-statuses';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import * as Select from '$lib/components/ui/select';
	import { toast } from 'svelte-sonner';
	import { Plus, Trash2, Pencil, X, Check, GripVertical, ArrowUp, ArrowDown } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let team = $state<Team | null>(null);
	let statuses = $state<TeamStatus[]>([]);
	let loading = $state(true);

	let addingCategory = $state<StatusCategory | null>(null);
	let addName = $state('');
	let addColor = $state('');

	let editingId = $state<string | null>(null);
	let editName = $state('');
	let editColor = $state('');

	let dragStatusId = $state<string | null>(null);
	let dragOverStatusId = $state<string | null>(null);
	let dragCategory = $state<StatusCategory | null>(null);
	let dragOverCategory = $state<StatusCategory | null>(null);
	let dropIndicator = $state<'above' | 'below'>('below');
	let workflowDragCategory = $state<StatusCategory | null>(null);
	let workflowDragOverCategory = $state<StatusCategory | null>(null);
	let workflowDropIndicator = $state<'above' | 'below'>('below');

	const teamWorkflowOverride = $derived(preferencesState.getTeamWorkflowSortOverride(slug, teamId));
	const teamWorkflowOrder = $derived(
		teamWorkflowOverride.mode === 'custom' && teamWorkflowOverride.workflowSortOrder
			? teamWorkflowOverride.workflowSortOrder
			: preferencesState.getWorkflowSortOrder(slug, teamId)
	);
	const workflowSortLabels: Record<TeamWorkflowSortMode, string> = {
		inherit: 'Use global',
		default: 'Workflow order',
		'active-first': 'Active first',
		custom: 'Custom',
	};

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

	async function updateSubIssueAutomation(field: 'parent_auto_close_enabled' | 'sub_issue_auto_close_enabled', value: boolean) {
		if (!team) return;
		const previous = team;
		team = { ...team, [field]: value };
		try {
			team = await updateTeam(slug, teamId, { [field]: value });
			toast.success('Sub-issue automation updated');
		} catch (err: any) {
			team = previous;
			toast.error(err?.error?.message || 'Failed to update automation');
		}
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

	function setTeamWorkflowSortMode(mode: TeamWorkflowSortMode) {
		preferencesState.setTeamWorkflowSortOverride(slug, teamId, {
			mode,
			workflowSortOrder: mode === 'custom' ? teamWorkflowOrder : teamWorkflowOverride.workflowSortOrder
		});
	}

	function moveTeamWorkflowCategory(category: StatusCategory, direction: -1 | 1) {
		const order = [...teamWorkflowOrder];
		const index = order.indexOf(category);
		const nextIndex = index + direction;
		if (index < 0 || nextIndex < 0 || nextIndex >= order.length) return;
		[order[index], order[nextIndex]] = [order[nextIndex], order[index]];
		preferencesState.setTeamWorkflowSortOverride(slug, teamId, {
			mode: 'custom',
			workflowSortOrder: order
		});
	}

	function handleWorkflowDragStart(e: DragEvent, category: StatusCategory) {
		workflowDragCategory = category;
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('text/plain', category);
		}
	}

	function handleWorkflowDragOver(e: DragEvent, category: StatusCategory) {
		if (!workflowDragCategory) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		workflowDragOverCategory = category;
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		workflowDropIndicator = e.clientY < rect.top + rect.height / 2 ? 'above' : 'below';
	}

	function handleWorkflowDragEnd() {
		workflowDragCategory = null;
		workflowDragOverCategory = null;
		workflowDropIndicator = 'below';
	}

	function handleWorkflowDrop(e: DragEvent, targetCategory: StatusCategory) {
		e.preventDefault();
		const sourceCategory = (e.dataTransfer?.getData('text/plain') || workflowDragCategory) as StatusCategory | null;
		if (!sourceCategory || sourceCategory === targetCategory) {
			handleWorkflowDragEnd();
			return;
		}

		const order = [...teamWorkflowOrder];
		const sourceIndex = order.indexOf(sourceCategory);
		const targetIndex = order.indexOf(targetCategory);
		if (sourceIndex === -1 || targetIndex === -1) {
			handleWorkflowDragEnd();
			return;
		}

		const [moved] = order.splice(sourceIndex, 1);
		const adjustedTargetIndex = order.indexOf(targetCategory);
		const insertIndex = workflowDropIndicator === 'below' ? adjustedTargetIndex + 1 : adjustedTargetIndex;
		order.splice(insertIndex, 0, moved);
		preferencesState.setTeamWorkflowSortOverride(slug, teamId, {
			mode: 'custom',
			workflowSortOrder: order
		});
		handleWorkflowDragEnd();
	}

	function handleDragStart(e: DragEvent, status: TeamStatus) {
		dragStatusId = status.id;
		dragCategory = status.category;
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('text/plain', status.id);
		}
	}

	function handleDragOver(e: DragEvent, status: TeamStatus) {
		if (!dragStatusId) return;
		dragOverCategory = status.category;

		if (dragCategory !== status.category) {
			dragOverStatusId = null;
			return;
		}

		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		dragOverStatusId = status.id;
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		const midY = rect.top + rect.height / 2;
		dropIndicator = e.clientY < midY ? 'above' : 'below';
	}

	function handleDragLeave() {
		dragOverStatusId = null;
	}

	function handleDragEnd() {
		dragStatusId = null;
		dragOverStatusId = null;
		dragCategory = null;
		dragOverCategory = null;
		dropIndicator = 'below';
	}

	function handleSectionDragOver(e: DragEvent, cat: StatusCategory) {
		if (!dragStatusId) return;
		dragOverCategory = cat;
	}

	function handleSectionDragLeave(e: DragEvent) {
		const related = e.relatedTarget as HTMLElement | null;
		if (related && (e.currentTarget as HTMLElement).contains(related)) return;
		dragOverCategory = null;
		dragOverStatusId = null;
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

		const reordered = [...catStatuses];
		const [moved] = reordered.splice(dragIdx, 1);
		const newTargetIdx = reordered.findIndex((s) => s.id === targetStatus.id);
		const insertIdx = dropIndicator === 'below' ? newTargetIdx + 1 : newTargetIdx;
		reordered.splice(insertIdx, 0, moved);

		const otherStatuses = statuses.filter((s) => s.category !== cat);
		const updatedCat = reordered.map((s, i) => ({ ...s, position: i }));
		statuses = [...otherStatuses, ...updatedCat];

		handleDragEnd();

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

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Issue statuses</h1>
	<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">
		Issue statuses define the workflow that issues go through from start to completion.
	</p>

	{#if team}
		<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<div class="border-b border-[var(--app-border)] px-5 py-4">
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Sub-issue automation</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Automatically keep parent and sub-issue statuses in sync for this team.</p>
			</div>
			<label class="flex items-center justify-between gap-4 px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">Parent auto-close</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Move a parent issue to done when all direct sub-issues are completed or cancelled.</p>
				</div>
				<input
					type="checkbox"
					checked={team.parent_auto_close_enabled}
					onchange={(e) => updateSubIssueAutomation('parent_auto_close_enabled', (e.currentTarget as HTMLInputElement).checked)}
					class="h-4 w-4 accent-[var(--app-accent)]"
				/>
			</label>
			<label class="flex items-center justify-between gap-4 border-t border-[var(--app-border)] px-5 py-4">
				<div>
					<p class="text-sm text-[var(--color-text-primary)]">Sub-issue auto-close</p>
					<p class="text-xs text-[var(--color-text-tertiary)]">Move remaining open direct sub-issues to done when their parent is completed.</p>
				</div>
				<input
					type="checkbox"
					checked={team.sub_issue_auto_close_enabled}
					onchange={(e) => updateSubIssueAutomation('sub_issue_auto_close_enabled', (e.currentTarget as HTMLInputElement).checked)}
					class="h-4 w-4 accent-[var(--app-accent)]"
				/>
			</label>
		</div>
	{/if}

	<div class="mt-6 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Issue list sorting</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Override status group sorting for this team. Kanban keeps the workflow below.</p>
			</div>
			<Select.Root
				type="single"
				value={teamWorkflowOverride.mode}
				onValueChange={(v) => {
					if (v) setTeamWorkflowSortMode(v as TeamWorkflowSortMode);
				}}
			>
				<Select.Trigger size="sm" class="w-[145px]">
					{workflowSortLabels[teamWorkflowOverride.mode]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="inherit">Use global</Select.Item>
					<Select.Item value="default">Workflow order</Select.Item>
					<Select.Item value="active-first">Active first</Select.Item>
					<Select.Item value="custom">Custom</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		{#if teamWorkflowOverride.mode === 'custom'}
			<div class="border-t border-[var(--app-border)]"></div>
			<div class="px-5 py-4">
				<p class="mb-2 text-xs text-[var(--color-text-tertiary)]">Custom category order</p>
				<div class="space-y-1">
					{#each teamWorkflowOrder as category, index (category)}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							animate:flip={{ duration: 180 }}
							class="group relative flex items-center justify-between rounded-md border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-2 transition-[background-color,border-color,box-shadow,opacity,scale] duration-200 ease-out hover:border-[var(--app-accent)]/40 hover:bg-[var(--color-bg-hover)]/40 hover:shadow-sm {workflowDragCategory === category ? 'scale-[0.99] opacity-70' : ''}"
							draggable="true"
							ondragstart={(e) => handleWorkflowDragStart(e, category)}
							ondragover={(e) => handleWorkflowDragOver(e, category)}
							ondragleave={() => (workflowDragOverCategory = null)}
							ondragend={handleWorkflowDragEnd}
							ondrop={(e) => handleWorkflowDrop(e, category)}
						>
							{#if workflowDragOverCategory === category && workflowDragCategory !== category}
								<div class="absolute {workflowDropIndicator === 'above' ? '-top-1' : '-bottom-1'} left-2 right-2 h-0.5 rounded-full bg-[var(--app-accent)] shadow-[0_0_12px_var(--app-accent)] transition-all"></div>
							{/if}
							<div class="flex items-center gap-2">
								<span class="cursor-grab rounded p-1 text-[var(--color-text-tertiary)] transition-colors group-hover:text-[var(--color-text-secondary)] active:cursor-grabbing">
									<GripVertical size={14} />
								</span>
								<span class="text-sm text-[var(--color-text-primary)] transition-colors group-hover:text-[var(--color-text-primary)]">{CATEGORY_LABELS[category]}</span>
							</div>
							<div class="flex items-center gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100">
								<button
									onclick={() => moveTeamWorkflowCategory(category, -1)}
									disabled={index === 0}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label="Move {CATEGORY_LABELS[category]} up"
								>
									<ArrowUp size={13} />
								</button>
								<button
									onclick={() => moveTeamWorkflowCategory(category, 1)}
									disabled={index === teamWorkflowOrder.length - 1}
									class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] disabled:opacity-30"
									aria-label="Move {CATEGORY_LABELS[category]} down"
								>
									<ArrowDown size={13} />
								</button>
							</div>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	<div class="mt-8">
		{#if loading}
			<div class="flex justify-center py-8">
				<div class="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-text-tertiary)] border-t-transparent"></div>
			</div>
		{:else}
			<div class="rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] overflow-hidden">
				{#each CATEGORY_ORDER as cat, catIdx}
					{@const catStatuses = statusesByCategory(cat)}

					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class="transition-colors {catIdx > 0 ? 'border-t' : ''} {dragStatusId && dragOverCategory === cat ? 'border-[var(--app-accent)]' : 'border-[var(--app-border)]'}"
						ondragover={(e) => handleSectionDragOver(e, cat)}
						ondragleave={(e) => handleSectionDragLeave(e)}
					>
					<div class="flex items-center justify-between px-5 py-2.5">
						<span class="text-xs font-medium text-[var(--color-text-tertiary)]">{CATEGORY_LABELS[cat]}</span>
						<button
							onclick={() => startAdd(cat)}
							class="rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors"
							title="Add status to {CATEGORY_LABELS[cat]}"
						>
							<Plus size={14} />
						</button>
					</div>

					{#each catStatuses as status (status.id)}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="relative flex items-center gap-1 px-1 py-0 border-t border-[var(--app-border)] transition-colors
								{dragStatusId === status.id ? 'opacity-40' : 'hover:bg-[var(--color-bg-hover)]/50'}"
							draggable={editingId !== status.id}
							ondragstart={(e) => handleDragStart(e, status)}
							ondragover={(e) => handleDragOver(e, status)}
							ondragleave={handleDragLeave}
							ondragend={handleDragEnd}
							ondrop={(e) => handleDrop(e, status)}
						>
							{#if dragOverStatusId === status.id && dragStatusId !== status.id}
								<div class="absolute {dropIndicator === 'above' ? '-top-px' : '-bottom-px'} left-3 right-3 h-0.5 bg-[var(--app-accent)] z-10 rounded-full"></div>
							{/if}
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
											class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg)] px-2 py-0.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
										/>
										<div class="flex items-center gap-0.5">
											{#each PRESET_COLORS as c}
												<button
													onclick={() => editColor = c}
													class="h-3.5 w-3.5 rounded-full {editColor === c ? 'ring-2 ring-[var(--app-accent)] ring-offset-1 ring-offset-[var(--color-bg)]' : ''}"
													style="background-color: {c}"
													aria-label="Select color {c}"
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

					{#if addingCategory === cat}
						<div class="flex items-center gap-3 px-5 py-2.5 border-t border-[var(--app-border)] bg-[var(--color-bg-hover)]/30">
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
										aria-label="Select color"
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
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
