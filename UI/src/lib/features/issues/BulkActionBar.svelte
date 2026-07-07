<script lang="ts">
	import type { Issue, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import type { Label } from '$lib/types/label';
	import type { Cycle } from '$lib/types/cycle';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { issuesState } from './issues.state.svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { toast } from 'svelte-sonner';
	import { ChevronLeft, CircleDot, Command, CornerDownRight, Flag, Plus, RefreshCw, Search, Tag, Trash2, X } from 'lucide-svelte';
	import * as issueApi from '$lib/api/issues';
	import { createLabel } from '$lib/api/labels';
	import { onMount } from 'svelte';
	import { showIssueDeletedToast, showIssuesDeletedToast } from './issue-deleted-toast';
	import IssuePickerDialog from './IssuePickerDialog.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';

	let {
		slug,
		labels = [],
		cycles,
		onlabelcreated
	}: {
		slug: string;
		labels?: Label[];
		cycles?: Cycle[];
		onlabelcreated?: (label: Label) => void;
	} = $props();

	type BulkCommand = 'status' | 'priority' | 'label' | 'cycle' | 'parent' | 'unparent';

	interface BulkCommandOption {
		id: BulkCommand;
		title: string;
		description: string;
		keywords: string;
	}

	const ANIM_DURATION = 100;
	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
	const commandButtonClass = 'flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors hover:bg-[var(--color-bg-hover)] data-[selected=true]:bg-[var(--color-bg-hover)] data-[selected=true]:ring-1 data-[selected=true]:ring-[var(--app-border)]';
	const optionButtonClass = 'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-bg-hover)] disabled:opacity-60 data-[selected=true]:bg-[var(--color-bg-hover)] data-[selected=true]:ring-1 data-[selected=true]:ring-[var(--app-border)]';

	let actionsOpen = $state(false);
	let actionsVisible = $state(false);
	let closingActions = false;
	let activeCommand = $state<BulkCommand | null>(null);
	let searchQuery = $state('');
	let selectedIndex = $state(0);
	let deleteOpen = $state(false);
	let parentPickerOpen = $state(false);
	let unparentOpen = $state(false);
	let creatingLabel = $state(false);
	let createdLabels = $state<Label[]>([]);
	let showCycleActions = $derived(cycles !== undefined);
	let visibleLabels = $derived([
		...createdLabels.filter((createdLabel) => !labels.some((label) => label.id === createdLabel.id)),
		...labels,
	]);
	let commands = $derived.by<BulkCommandOption[]>(() => {
		const options: BulkCommandOption[] = [
			{ id: 'status', title: 'Change status', description: 'Move selected issues to a workflow status', keywords: 'status workflow state' },
			{ id: 'priority', title: 'Set priority', description: 'Apply a priority to selected issues', keywords: 'priority urgent high medium low none' },
			{ id: 'label', title: 'Add label', description: 'Add a label to selected issues', keywords: 'label tag' },
		];

		if (showCycleActions) {
			options.push({ id: 'cycle', title: 'Assign to cycle', description: 'Add selected issues to a cycle or remove their cycle', keywords: 'cycle sprint iteration' });
		}

		options.push(
			{ id: 'parent', title: 'Set parent', description: 'Move selected issues under another issue', keywords: 'parent subissue sub issue' },
			{ id: 'unparent', title: 'Remove parent', description: 'Make selected sub-issues top-level issues', keywords: 'unparent remove parent top level' }
		);

		return options;
	});
	let filteredCommands = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return commands;
		return commands.filter((command) => `${command.title} ${command.description} ${command.keywords}`.toLowerCase().includes(term));
	});
	let filteredStatuses = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return teamStatusesState.statusOrder;
		return teamStatusesState.statusOrder.filter((status) => `${status.name} ${status.category}`.toLowerCase().includes(term));
	});
	let filteredPriorities = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return priorityValues;
		return priorityValues.filter((priority) => PRIORITY_LABELS[priority].toLowerCase().includes(term));
	});
	let filteredLabels = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return visibleLabels;
		return visibleLabels.filter((label) => label.name.toLowerCase().includes(term));
	});
	let filteredCycles = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return cycles ?? [];
		return (cycles ?? []).filter((cycle) => cycle.name.toLowerCase().includes(term));
	});
	let activeTitle = $derived(commands.find((command) => command.id === activeCommand)?.title ?? 'Bulk actions');
	let searchPlaceholder = $derived(activeCommand ? `Search ${activeTitle.toLowerCase()}...` : 'Search actions...');
	let canCreateLabel = $derived(activeCommand === 'label' && searchQuery.trim() && !visibleLabels.some((label) => label.name.toLowerCase() === searchQuery.trim().toLowerCase()));
	let activeOptionCount = $derived.by(() => {
		if (!activeCommand) return filteredCommands.length;
		if (activeCommand === 'status') return filteredStatuses.length;
		if (activeCommand === 'priority') return filteredPriorities.length;
		if (activeCommand === 'label') return filteredLabels.length + (canCreateLabel ? 1 : 0);
		if (activeCommand === 'cycle') return filteredCycles.length + 1;
		return 0;
	});

	onMount(() => {
		const onRequestDelete = () => {
			if (issuesState.selectionCount > 0) {
				deleteOpen = true;
			}
		};
		window.addEventListener('issues:bulk-delete-request', onRequestDelete);
		return () => window.removeEventListener('issues:bulk-delete-request', onRequestDelete);
	});

	$effect(() => {
		if (actionsOpen) {
			closingActions = false;
			actionsVisible = false;
			activeCommand = null;
			searchQuery = '';
			selectedIndex = 0;
			requestAnimationFrame(() => {
				requestAnimationFrame(() => {
					actionsVisible = true;
					document.getElementById('bulk-actions-search')?.focus();
				});
			});
		} else {
			actionsVisible = false;
			closingActions = false;
		}
	});

	$effect(() => {
		activeCommand;
		searchQuery;
		selectedIndex = 0;
	});

	$effect(() => {
		const count = activeOptionCount;
		if (count === 0) {
			selectedIndex = 0;
		} else if (selectedIndex >= count) {
			selectedIndex = count - 1;
		}
	});

	$effect(() => {
		selectedIndex;
		activeCommand;
		requestAnimationFrame(() => {
			document.getElementById(`bulk-action-row-${selectedIndex}`)?.scrollIntoView({ block: 'nearest' });
		});
	});

	function closeActions() {
		if (closingActions) return;
		closingActions = true;
		actionsVisible = false;
		setTimeout(() => {
			actionsOpen = false;
			closingActions = false;
		}, ANIM_DURATION);
	}

	function backToCommands() {
		activeCommand = null;
		searchQuery = '';
		selectedIndex = 0;
		requestAnimationFrame(() => document.getElementById('bulk-actions-search')?.focus());
	}

	function selectCommand(command: BulkCommand) {
		if (command === 'parent') {
			closeActions();
			setTimeout(() => (parentPickerOpen = true), ANIM_DURATION);
			return;
		}

		if (command === 'unparent') {
			closeActions();
			setTimeout(() => (unparentOpen = true), ANIM_DURATION);
			return;
		}

		activeCommand = command;
		searchQuery = '';
		selectedIndex = 0;
		requestAnimationFrame(() => document.getElementById('bulk-actions-search')?.focus());
	}

	function handleActionsKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			if (activeCommand) {
				backToCommands();
			} else {
				closeActions();
			}
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (activeOptionCount > 0) selectedIndex = Math.min(selectedIndex + 1, activeOptionCount - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (activeOptionCount > 0) selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			activateSelectedOption();
		} else if (e.key === 'Backspace' && activeCommand && searchQuery === '') {
			backToCommands();
		}
	}

	function activateSelectedOption() {
		if (activeOptionCount === 0) return;
		if (!activeCommand) {
			const command = filteredCommands[selectedIndex];
			if (command) selectCommand(command.id);
			return;
		}

		if (activeCommand === 'status') {
			const status = filteredStatuses[selectedIndex];
			if (status) void bulkSetStatus(status.id);
		} else if (activeCommand === 'priority') {
			const priority = filteredPriorities[selectedIndex];
			if (priority !== undefined) void bulkSetPriority(priority);
		} else if (activeCommand === 'label') {
			const labelIndex = selectedIndex - (canCreateLabel ? 1 : 0);
			if (canCreateLabel && selectedIndex === 0) {
				void bulkCreateAndAddLabel();
			} else {
				const label = filteredLabels[labelIndex];
				if (label) void bulkAddLabel(label.id);
			}
		} else if (activeCommand === 'cycle') {
			if (selectedIndex === 0) {
				void bulkSetCycle(null);
			} else {
				const cycle = filteredCycles[selectedIndex - 1];
				if (cycle) void bulkSetCycle(cycle.id);
			}
		}
	}

	function commandIcon(command: BulkCommand) {
		return command;
	}

	async function bulkSetStatus(statusId: string) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { status_id: statusId } as any);
			toast.success(`Updated ${count} issue${count > 1 ? 's' : ''}`);
			closeActions();
		} catch {
			toast.error('Bulk update failed');
		}
	}

	async function bulkSetPriority(priority: IssuePriority) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { priority });
			toast.success(`Updated ${count} issue${count > 1 ? 's' : ''}`);
			closeActions();
		} catch {
			toast.error('Bulk update failed');
		}
	}

	async function bulkAddLabel(labelId: string, successMessage?: string) {
		const selectedIssues = issuesState.issues.filter((issue) => issuesState.selectedIds.has(issue.id));
		if (selectedIssues.length === 0) return;

		try {
			await Promise.all(
				selectedIssues.map((issue) => {
					const labelIds = new Set((issue.labels ?? []).map((label) => label.id));
					labelIds.add(labelId);
					return issuesState.update(slug, issue.identifier, { label_ids: Array.from(labelIds) });
				})
			);
			issuesState.clearSelection();
			toast.success(successMessage ?? `Updated ${selectedIssues.length} issue${selectedIssues.length > 1 ? 's' : ''}`);
			closeActions();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update labels');
		}
	}

	async function bulkCreateAndAddLabel() {
		const name = searchQuery.trim();
		if (!name || creatingLabel) return;
		creatingLabel = true;
		try {
			const label = await createLabel(slug, { name, color: randomLabelColor() });
			createdLabels = [label, ...createdLabels.filter((createdLabel) => createdLabel.id !== label.id)];
			onlabelcreated?.(label);
			await bulkAddLabel(label.id, `Created and added ${label.name}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create label');
		} finally {
			creatingLabel = false;
		}
	}

	function randomLabelColor() {
		const presetColors = ['#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4', '#3b82f6', '#6366f1', '#8b5cf6', '#ec4899', '#6b7280'];
		return presetColors[Math.floor(Math.random() * presetColors.length)];
	}

	async function bulkSetCycle(cycleId: string | null) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { cycle_id: cycleId ?? '' });
			toast.success(`${cycleId ? 'Assigned' : 'Removed cycle from'} ${count} issue${count > 1 ? 's' : ''}`);
			closeActions();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update cycle');
		}
	}

	async function bulkSetParent(parent: Issue) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { parent_id: parent.id } as any);
			toast.success(`Moved ${count} issue${count > 1 ? 's' : ''} under ${parent.identifier}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to set parent');
		}
	}

	async function bulkRemoveParent() {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { parent_id: '' } as any);
			toast.success(`Removed parent from ${count} issue${count > 1 ? 's' : ''}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to remove parent');
		}
		unparentOpen = false;
	}

	async function bulkDelete() {
		const ids = Array.from(issuesState.selectedIds);
		if (ids.length === 0) return;
		const idsToDelete = new Set(ids);
		const selectedIssues = issuesState.issues.filter((issue) => idsToDelete.has(issue.id));
		try {
			await issueApi.bulkDeleteIssues(slug, { issue_ids: ids });
			issuesState.issues = issuesState.issues.filter((i) => !idsToDelete.has(i.id));
			issuesState.totalCount -= ids.length;
			issuesState.clearSelection();
			if (selectedIssues.length === 1) {
				showIssueDeletedToast(selectedIssues[0]);
			} else {
				showIssuesDeletedToast(ids.length);
			}
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete issues');
		}
		deleteOpen = false;
	}
</script>

{#if issuesState.selectionCount > 0}
	<div class="fixed bottom-4 left-1/2 z-40 flex max-w-[calc(100vw-1.5rem)] -translate-x-1/2 items-center justify-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/95 p-1.5 shadow-xl backdrop-blur max-sm:bottom-3">
		<span class="inline-flex h-7 items-center rounded-md bg-[var(--color-bg-tertiary)] px-2.5 text-xs font-medium whitespace-nowrap text-[var(--color-text-primary)]">
			{issuesState.selectionCount} selected
		</span>

		<div class="mx-0.5 h-5 w-px bg-[var(--app-border)] max-sm:hidden"></div>

		<button
			onclick={() => (actionsOpen = true)}
			class="inline-flex h-7 items-center gap-1.5 rounded-md border border-[var(--app-border)] px-2.5 text-xs font-medium text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
		>
			<Command size={12} />
			Actions
		</button>

		<div class="mx-0.5 h-5 w-px bg-[var(--app-border)] max-sm:hidden"></div>

		<button
			onclick={() => (deleteOpen = true)}
			class="inline-flex h-7 items-center rounded-md border border-red-500/30 px-2 text-xs text-red-500 transition-colors hover:bg-red-500/10"
			title="Delete selected issues"
		>
			<Trash2 size={12} />
		</button>

		<button
			onclick={() => issuesState.clearSelection()}
			class="inline-flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			title="Clear selection"
		>
			<X size={16} />
		</button>
	</div>

	{#if actionsOpen}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[12vh]" onkeydown={handleActionsKeydown}>
			<button
				class="fixed inset-0 cursor-default"
				style="background: rgba(0,0,0,{actionsVisible ? 0.5 : 0}); transition: background {ANIM_DURATION}ms ease;"
				onclick={closeActions}
				tabindex={-1}
				aria-label="Close bulk actions"
			></button>

			<div
				class="relative z-10 w-full max-w-lg overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl"
				style="opacity: {actionsVisible ? 1 : 0}; transform: scale({actionsVisible ? 1 : 0.95}); transition: opacity {ANIM_DURATION}ms ease, transform {ANIM_DURATION}ms ease;"
			>
				<div class="sr-only">
					<h2>Bulk actions</h2>
					<p>Search and run a bulk action for selected issues.</p>
				</div>

				<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-3">
					{#if activeCommand}
						<button
							onclick={backToCommands}
							class="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							title="Back to actions"
						>
							<ChevronLeft size={16} />
						</button>
					{:else}
						<Search size={16} class="text-[var(--color-text-tertiary)]" />
					{/if}
					<!-- svelte-ignore a11y_autofocus -->
					<input
						id="bulk-actions-search"
						type="text"
						aria-label={activeTitle}
						bind:value={searchQuery}
						placeholder={searchPlaceholder}
						autofocus
						class="min-w-0 flex-1 bg-transparent py-4 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
					/>
					<button
						onclick={closeActions}
						class="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title="Close"
					>
						<X size={16} />
					</button>
				</div>

				<div class="max-h-[60vh] min-h-72 overflow-y-auto p-2">
					{#if !activeCommand}
						{#if filteredCommands.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">No actions found.</div>
						{:else}
							{#each filteredCommands as command, index (command.id)}
								<button id={`bulk-action-row-${index}`} class={commandButtonClass} data-selected={selectedIndex === index} onpointerenter={() => (selectedIndex = index)} onclick={() => selectCommand(command.id)}>
									<span class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]">
										{#if commandIcon(command.id) === 'status'}
											<CircleDot size={16} />
										{:else if commandIcon(command.id) === 'priority'}
											<Flag size={16} />
										{:else if commandIcon(command.id) === 'label'}
											<Tag size={16} />
										{:else if commandIcon(command.id) === 'cycle'}
											<RefreshCw size={16} />
										{:else if commandIcon(command.id) === 'parent'}
											<CornerDownRight size={16} />
										{:else}
											<X size={16} />
										{/if}
									</span>
									<span class="min-w-0 flex-1">
										<span class="block truncate font-medium text-[var(--color-text-primary)]">{command.title}</span>
										<span class="block truncate text-xs text-[var(--color-text-tertiary)]">{command.description}</span>
									</span>
								</button>
							{/each}
						{/if}
					{:else if activeCommand === 'status'}
						{#if filteredStatuses.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">No statuses found.</div>
						{:else}
							{#each filteredStatuses as status, index (status.id)}
								<button id={`bulk-action-row-${index}`} class={optionButtonClass} data-selected={selectedIndex === index} onpointerenter={() => (selectedIndex = index)} onclick={() => bulkSetStatus(status.id)}>
									<IssueStatusIcon category={status.category} color={status.color} size={14} />
									<span class="truncate">{status.name}</span>
								</button>
							{/each}
						{/if}
					{:else if activeCommand === 'priority'}
						{#if filteredPriorities.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">No priorities found.</div>
						{:else}
							{#each filteredPriorities as priority, index (priority)}
								<button id={`bulk-action-row-${index}`} class={optionButtonClass} data-selected={selectedIndex === index} onpointerenter={() => (selectedIndex = index)} onclick={() => bulkSetPriority(priority)}>
									<IssuePriorityIcon {priority} size={14} />
									<span class="truncate">{PRIORITY_LABELS[priority]}</span>
								</button>
							{/each}
						{/if}
					{:else if activeCommand === 'label'}
						{#if canCreateLabel}
							<button id="bulk-action-row-0" class={optionButtonClass} data-selected={selectedIndex === 0} onpointerenter={() => (selectedIndex = 0)} onclick={bulkCreateAndAddLabel} disabled={creatingLabel}>
								<Plus size={14} />
								<span class="truncate">{creatingLabel ? 'Creating...' : `Create label "${searchQuery.trim()}"`}</span>
							</button>
						{/if}
						{#if filteredLabels.length === 0 && !canCreateLabel}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">No labels found.</div>
						{:else}
							{#each filteredLabels as label, index (label.id)}
								{@const rowIndex = index + (canCreateLabel ? 1 : 0)}
								<button id={`bulk-action-row-${rowIndex}`} class={optionButtonClass} data-selected={selectedIndex === rowIndex} onpointerenter={() => (selectedIndex = rowIndex)} onclick={() => bulkAddLabel(label.id)}>
									<span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background-color: {label.color}"></span>
									<span class="truncate">{label.name}</span>
								</button>
							{/each}
						{/if}
					{:else if activeCommand === 'cycle'}
						<button id="bulk-action-row-0" class={optionButtonClass} data-selected={selectedIndex === 0} onpointerenter={() => (selectedIndex = 0)} onclick={() => bulkSetCycle(null)}>
							<RefreshCw size={14} class="text-[var(--color-text-tertiary)]" />
							<span class="truncate text-[var(--color-text-tertiary)]">No cycle</span>
						</button>
						{#if filteredCycles.length === 0}
							<div class="py-8 text-center text-xs text-[var(--color-text-tertiary)]">No cycles found.</div>
						{:else}
							{#each filteredCycles as cycle, index (cycle.id)}
								{@const rowIndex = index + 1}
								<button id={`bulk-action-row-${rowIndex}`} class={optionButtonClass} data-selected={selectedIndex === rowIndex} onpointerenter={() => (selectedIndex = rowIndex)} onclick={() => bulkSetCycle(cycle.id)}>
									<RefreshCw size={14} class="text-[var(--color-text-tertiary)]" />
									<span class="truncate">{cycle.name}</span>
								</button>
							{/each}
						{/if}
					{/if}
				</div>
			</div>
		</div>
	{/if}

	<IssuePickerDialog
		bind:open={parentPickerOpen}
		{slug}
		title="Set parent for selected issues"
		description={`${issuesState.selectionCount} selected issue${issuesState.selectionCount > 1 ? 's' : ''} will become sub-issues of the selected issue.`}
		actionLabel="Set parent"
		excludeIds={Array.from(issuesState.selectedIds)}
		onselect={bulkSetParent}
	/>

	<AlertDialog.Root bind:open={deleteOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>Delete {issuesState.selectionCount} issue{issuesState.selectionCount > 1 ? 's' : ''}?</AlertDialog.Title>
				<AlertDialog.Description>This action cannot be undone.</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel
					variant="outline"
					class="border-[var(--app-border)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
				>
					Cancel
				</AlertDialog.Cancel>
				<AlertDialog.Action
					variant="destructive"
					class="bg-red-600 text-white hover:bg-red-700"
					onclick={bulkDelete}
				>
					Delete
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>

	<AlertDialog.Root bind:open={unparentOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>Remove parent from {issuesState.selectionCount} issue{issuesState.selectionCount > 1 ? 's' : ''}?</AlertDialog.Title>
				<AlertDialog.Description>Selected sub-issues will become regular top-level issues.</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel variant="outline" class="border-[var(--app-border)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
					Cancel
				</AlertDialog.Cancel>
				<AlertDialog.Action variant="destructive" class="bg-red-600 text-white hover:bg-red-700" onclick={bulkRemoveParent}>
					Remove parent
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{/if}
