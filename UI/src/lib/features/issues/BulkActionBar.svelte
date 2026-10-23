<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import type { Issue, IssuePriority, RelationType } from '$lib/types/issue';
	import { getPriorityLabel } from '$lib/types/issue';
	import type { Label } from '$lib/types/label';
	import type { Cycle } from '$lib/types/cycle';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { issuesState } from './issues.state.svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { appToast } from '$lib/features/toast/toast';
	import {
		CalendarDays,
		ChevronLeft,
		CircleDot,
		Command,
		CornerDownRight,
		Copy,
		Flag,
		GitBranch,
		Link,
		Plus,
		RefreshCw,
		Search,
		Tag,
		Trash2,
		Users,
		X
	} from '@lucide/svelte';
	import * as issueApi from '$lib/api/issues';
	import { createLabel } from '$lib/api/labels';
	import { createRelation } from '$lib/api/issue-relations';
	import { onMount } from 'svelte';
	import { showIssueDeletedToast, showIssuesDeletedToast } from './issue-deleted-toast';
	import IssuePickerDialog from './IssuePickerDialog.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import DueDatePickerPanel from '$lib/components/shared/DueDatePickerPanel.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		slug,
		labels = [],
		members = [],
		cycles,
		onlabelcreated,
		canDelete = true,
		statusOptions,
		onchanged
	}: {
		slug: string;
		labels?: Label[];
		members?: WorkspaceMember[];
		cycles?: Cycle[];
		onlabelcreated?: (label: Label) => void;
		canDelete?: boolean;
		statusOptions?: import('$lib/types/team-status').TeamStatus[];
		onchanged?: () => void;
	} = $props();

	type BulkCommand =
		| 'assignee'
		| 'status'
		| 'priority'
		| 'label'
		| 'cycle'
		| 'due_date'
		| 'parent'
		| 'subissue'
		| 'duplicate'
		| 'related'
		| 'unparent';

	interface BulkCommandOption {
		id: BulkCommand;
		title: string;
		description: string;
		keywords: string;
	}

	const ANIM_DURATION = 100;
	function _s(n: number): string {
		return n > 1 ? 's' : '';
	}
	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
	const commandButtonClass =
		'flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors hover:bg-[var(--color-bg-hover)] data-[selected=true]:bg-[var(--color-bg-hover)] data-[selected=true]:ring-1 data-[selected=true]:ring-[var(--app-border)]';
	const optionButtonClass =
		'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-bg-hover)] disabled:opacity-60 data-[selected=true]:bg-[var(--color-bg-hover)] data-[selected=true]:ring-1 data-[selected=true]:ring-[var(--app-border)]';

	let actionsOpen = $state(false);
	let actionsVisible = $state(false);
	let closingActions = false;
	let activeCommand = $state<BulkCommand | null>(null);
	let searchQuery = $state('');
	let selectedIndex = $state(0);
	let selectedAssigneeIds = $state<string[]>([]);
	let deleteOpen = $state(false);
	let parentPickerOpen = $state(false);
	let parentPickerMode = $state<'parent' | 'subissue'>('parent');
	let relationPickerOpen = $state(false);
	let relationPickerType = $state<RelationType>('related');
	let unparentOpen = $state(false);
	let creatingLabel = $state(false);
	let createdLabels = $state<Label[]>([]);
	let showCycleActions = $derived(cycles !== undefined);
	let visibleLabels = $derived([
		...createdLabels.filter((createdLabel) => !labels.some((label) => label.id === createdLabel.id)),
		...labels
	]);
	let commands = $derived.by<BulkCommandOption[]>(() => {
		const options: BulkCommandOption[] = [
			{
				id: 'assignee',
				title: m['bulkActions.commands.assignee_title'](),
				description: m['bulkActions.commands.assignee_desc'](),
				keywords: 'assignee assign users members owner'
			},
			{
				id: 'status',
				title: m['bulkActions.commands.status_title'](),
				description: m['bulkActions.commands.status_desc'](),
				keywords: 'status workflow state'
			},
			{
				id: 'priority',
				title: m['bulkActions.commands.priority_title'](),
				description: m['bulkActions.commands.priority_desc'](),
				keywords: 'priority urgent high medium low none'
			},
			{
				id: 'label',
				title: m['bulkActions.commands.label_title'](),
				description: m['bulkActions.commands.label_desc'](),
				keywords: 'label tag'
			},
			{
				id: 'due_date',
				title: m['bulkActions.commands.due_date_title'](),
				description: m['bulkActions.commands.due_date_desc'](),
				keywords: 'due date deadline calendar'
			}
		];

		if (showCycleActions) {
			options.push({
				id: 'cycle',
				title: m['bulkActions.commands.cycle_title'](),
				description: m['bulkActions.commands.cycle_desc'](),
				keywords: 'cycle sprint iteration'
			});
		}

		options.push(
			{
				id: 'parent',
				title: m['bulkActions.commands.parent_title'](),
				description: m['bulkActions.commands.parent_desc'](),
				keywords: 'parent subissue sub issue'
			},
			{
				id: 'subissue',
				title: m['bulkActions.commands.subissue_title'](),
				description: m['bulkActions.commands.subissue_desc'](),
				keywords: 'subissue sub issue child parent'
			},
			{
				id: 'duplicate',
				title: m['bulkActions.commands.duplicate_title'](),
				description: m['bulkActions.commands.duplicate_desc'](),
				keywords: 'duplicate duplicated copy'
			},
			{
				id: 'related',
				title: m['bulkActions.commands.related_title'](),
				description: m['bulkActions.commands.related_desc'](),
				keywords: 'related relation link'
			},
			{
				id: 'unparent',
				title: m['bulkActions.commands.unparent_title'](),
				description: m['bulkActions.commands.unparent_desc'](),
				keywords: 'unparent remove parent top level'
			}
		);

		return statusOptions?.length === 0 ? options.filter((option) => option.id !== 'status') : options;
	});
	let filteredCommands = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return commands;
		return commands.filter((command) =>
			`${command.title} ${command.description} ${command.keywords}`.toLowerCase().includes(term)
		);
	});
	let filteredStatuses = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return statusOptions ?? teamStatusesState.statusOrder;
		return (statusOptions ?? teamStatusesState.statusOrder).filter((status) =>
			`${status.name} ${status.category}`.toLowerCase().includes(term)
		);
	});
	let filteredMembers = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return members;
		return members.filter((member) => `${member.name ?? ''} ${member.email}`.toLowerCase().includes(term));
	});
	let filteredPriorities = $derived.by(() => {
		const term = searchQuery.trim().toLowerCase();
		if (!term) return priorityValues;
		return priorityValues.filter((priority) => getPriorityLabel(priority).toLowerCase().includes(term));
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
	let activeTitle = $derived(
		commands.find((command) => command.id === activeCommand)?.title ?? m['bulkActions.title']()
	);
	let searchPlaceholder = $derived(
		activeCommand
			? m['bulkActions.search_scope']({ scope: activeTitle.toLowerCase() })
			: m['bulkActions.search_placeholder']()
	);
	let canCreateLabel = $derived(
		activeCommand === 'label' &&
			searchQuery.trim() &&
			!visibleLabels.some((label) => label.name.toLowerCase() === searchQuery.trim().toLowerCase())
	);
	let relationPickerTitle = $derived(
		relationPickerType === 'duplicate'
			? m['bulkActions.relation_picker.duplicate_title']()
			: m['bulkActions.relation_picker.related_title']()
	);
	let relationPickerDescription = $derived(
		relationPickerType === 'duplicate'
			? m['bulkActions.relation_picker.duplicate_desc']({
					n: issuesState.selectionCount,
					s: _s(issuesState.selectionCount)
				})
			: m['bulkActions.relation_picker.related_desc']({
					n: issuesState.selectionCount,
					s: _s(issuesState.selectionCount)
				})
	);
	let parentPickerTitle = $derived(
		parentPickerMode === 'subissue'
			? m['bulkActions.parent_picker.title_subissue']()
			: m['bulkActions.parent_picker.title_parent']()
	);
	let parentPickerDescription = $derived(
		m['bulkActions.parent_picker.desc']({ n: issuesState.selectionCount, s: _s(issuesState.selectionCount) })
	);
	let activeOptionCount = $derived.by(() => {
		if (!activeCommand) return filteredCommands.length;
		if (activeCommand === 'assignee') return filteredMembers.length + 1;
		if (activeCommand === 'status') return filteredStatuses.length;
		if (activeCommand === 'priority') return filteredPriorities.length;
		if (activeCommand === 'label') return filteredLabels.length + (canCreateLabel ? 1 : 0);
		if (activeCommand === 'cycle') return filteredCycles.length + 1;
		if (activeCommand === 'due_date') return 0;
		return 0;
	});

	onMount(() => {
		const onRequestDelete = () => {
			if (canDelete && issuesState.selectionCount > 0) {
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
		if (command === 'parent' || command === 'subissue') {
			parentPickerMode = command;
			closeActions();
			setTimeout(() => (parentPickerOpen = true), ANIM_DURATION);
			return;
		}

		if (command === 'duplicate' || command === 'related') {
			relationPickerType = command === 'duplicate' ? 'duplicate' : 'related';
			closeActions();
			setTimeout(() => (relationPickerOpen = true), ANIM_DURATION);
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
		if (command === 'assignee') selectedAssigneeIds = [];
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

		if (activeCommand === 'assignee') {
			if (selectedIndex === 0) {
				selectedAssigneeIds = [];
			} else {
				const member = filteredMembers[selectedIndex - 1];
				if (member) toggleAssignee(member.user_id);
			}
		} else if (activeCommand === 'status') {
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

	function toggleAssignee(userId: string) {
		if (selectedAssigneeIds.includes(userId)) {
			selectedAssigneeIds = selectedAssigneeIds.filter((id) => id !== userId);
		} else {
			selectedAssigneeIds = [...selectedAssigneeIds, userId];
		}
	}

	function formatDisplayDate(value: string) {
		return new Date(`${value}T00:00:00`).toLocaleDateString(getLocale(), {
			month: 'short',
			day: 'numeric',
			year: 'numeric'
		});
	}

	async function bulkSetAssignees(assigneeIds = selectedAssigneeIds) {
		const selectedIssues = issuesState.issues.filter((issue) => issuesState.selectedIds.has(issue.id));
		if (selectedIssues.length === 0) return;

		try {
			await Promise.all(
				selectedIssues.map((issue) => issuesState.update(slug, issue.identifier, { assignee_ids: assigneeIds }))
			);
			issuesState.clearSelection();
			onchanged?.();
			appToast.success(
				assigneeIds.length === 0
					? m['bulkActions.toast.cleared_assignees']({ n: selectedIssues.length, s: _s(selectedIssues.length) })
					: m['bulkActions.toast.assigned']({ n: selectedIssues.length, s: _s(selectedIssues.length) })
			);
			closeActions();
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_assignees']());
		}
	}

	async function bulkSetDueDate(date: string | null) {
		const selectedIssues = issuesState.issues.filter((issue) => issuesState.selectedIds.has(issue.id));
		if (selectedIssues.length === 0) return;

		try {
			await Promise.all(
				selectedIssues.map((issue) => issuesState.update(slug, issue.identifier, { due_date: date ?? '' }))
			);
			issuesState.clearSelection();
			onchanged?.();
			appToast.success(
				date
					? m['bulkActions.toast.due_date_set']({
							date: formatDisplayDate(date),
							n: selectedIssues.length,
							s: _s(selectedIssues.length)
						})
					: m['bulkActions.toast.due_date_cleared']({ n: selectedIssues.length, s: _s(selectedIssues.length) })
			);
			closeActions();
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_due_date']());
		}
	}

	async function bulkAddRelation(target: Issue) {
		const selectedIssues = issuesState.issues.filter((issue) => issuesState.selectedIds.has(issue.id));
		if (selectedIssues.length === 0) return;

		try {
			await Promise.all(
				selectedIssues.map((issue) =>
					createRelation(slug, issue.identifier, { related_identifier: target.identifier, type: relationPickerType })
				)
			);
			issuesState.clearSelection();
			onchanged?.();
			appToast.success(
				relationPickerType === 'duplicate'
					? m['bulkActions.toast.marked_duplicate']({
							n: selectedIssues.length,
							s: _s(selectedIssues.length),
							id: target.identifier
						})
					: m['bulkActions.toast.related']({
							n: selectedIssues.length,
							s: _s(selectedIssues.length),
							id: target.identifier
						})
			);
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_relation']());
		}
	}

	function commandIcon(command: BulkCommand) {
		return command;
	}

	async function bulkSetStatus(statusId: string) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { status_id: statusId } as any);
			onchanged?.();
			appToast.success(m['bulkActions.toast.updated']({ n: count, s: _s(count) }));
			closeActions();
		} catch {
			appToast.error('Bulk update failed');
		}
	}

	async function bulkSetPriority(priority: IssuePriority) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { priority });
			onchanged?.();
			appToast.success(m['bulkActions.toast.updated']({ n: count, s: _s(count) }));
			closeActions();
		} catch {
			appToast.error('Bulk update failed');
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
			onchanged?.();
			appToast.success(
				successMessage ?? m['bulkActions.toast.updated']({ n: selectedIssues.length, s: _s(selectedIssues.length) })
			);
			closeActions();
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_labels']());
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
			appToast.apiError(err, m['sharedComponents.selectors.create_label_failed']());
		} finally {
			creatingLabel = false;
		}
	}

	function randomLabelColor() {
		const presetColors = [
			'#ef4444',
			'#f97316',
			'#eab308',
			'#22c55e',
			'#06b6d4',
			'#3b82f6',
			'#6366f1',
			'#8b5cf6',
			'#ec4899',
			'#6b7280'
		];
		return presetColors[Math.floor(Math.random() * presetColors.length)];
	}

	async function bulkSetCycle(cycleId: string | null) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { cycle_id: cycleId ?? '' });
			onchanged?.();
			appToast.success(
				cycleId
					? m['bulkActions.toast.cycle_assigned']({ n: count, s: _s(count) })
					: m['bulkActions.toast.cycle_removed']({ n: count, s: _s(count) })
			);
			closeActions();
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_cycle']());
		}
	}

	async function bulkSetParent(parent: Issue) {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { parent_id: parent.id } as any);
			onchanged?.();
			appToast.success(m['bulkActions.toast.moved_under']({ n: count, s: _s(count), id: parent.identifier }));
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_parent']());
		}
	}

	async function bulkRemoveParent() {
		const count = issuesState.selectionCount;
		try {
			await issuesState.bulkUpdate(slug, { parent_id: '' } as any);
			onchanged?.();
			appToast.success(m['bulkActions.toast.removed_parent']({ n: count, s: _s(count) }));
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_remove_parent']());
		}
		unparentOpen = false;
	}

	async function bulkDelete() {
		if (!canDelete) return;
		const ids = Array.from(issuesState.selectedIds);
		if (ids.length === 0) return;
		const idsToDelete = new Set(ids);
		const selectedIssues = issuesState.issues.filter((issue) => idsToDelete.has(issue.id));
		try {
			await issueApi.bulkDeleteIssues(slug, { issue_ids: ids });
			issuesState.issues = issuesState.issues.filter((i) => !idsToDelete.has(i.id));
			issuesState.totalCount -= ids.length;
			issuesState.clearSelection();
			onchanged?.();
			if (selectedIssues.length === 1) {
				showIssueDeletedToast(selectedIssues[0]);
			} else {
				showIssuesDeletedToast(ids.length);
			}
		} catch (err: any) {
			appToast.apiError(err, m['bulkActions.toast.failed_delete']());
		}
		deleteOpen = false;
	}
</script>

{#if issuesState.selectionCount > 0}
	<div
		class="fixed bottom-4 left-1/2 z-40 flex max-w-[calc(100vw-1.5rem)] -translate-x-1/2 items-center justify-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/95 p-1.5 shadow-xl backdrop-blur max-sm:bottom-3"
	>
		<span
			class="inline-flex h-7 items-center rounded-md bg-[var(--color-bg-tertiary)] px-2.5 text-xs font-medium whitespace-nowrap text-[var(--color-text-primary)]"
		>
			{m['bulkActions.selected_count']({ n: issuesState.selectionCount })}
		</span>

		<div class="mx-0.5 h-5 w-px bg-[var(--app-border)] max-sm:hidden"></div>

		<Button variant="plain" onclick={() => (actionsOpen = true)}
			class="inline-flex h-7 items-center gap-1.5 rounded-md border border-[var(--app-border)] px-2.5 text-xs font-medium text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
		>
			<Command size={12} />
			{m['bulkActions.actions_button']()}
		</Button>

		<div class="mx-0.5 h-5 w-px bg-[var(--app-border)] max-sm:hidden"></div>

		{#if canDelete}
			<Button variant="plain" onclick={() => (deleteOpen = true)}
				class="inline-flex h-7 items-center rounded-md border border-red-500/30 px-2 text-xs text-red-500 transition-colors hover:bg-red-500/10"
				title={m['bulkActions.delete_selected']()}
			>
				<Trash2 size={12} />
			</Button>
		{/if}

		<Button variant="plain" onclick={() => issuesState.clearSelection()}
			class="inline-flex h-7 w-7 items-center justify-center rounded-md text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			title={m['bulkActions.clear_selection']()}
		>
			<X size={16} />
		</Button>
	</div>

	{#if actionsOpen}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[12vh]" onkeydown={handleActionsKeydown}>
			<Button variant="plain" class="fixed inset-0 cursor-default"
				style="background: rgba(0,0,0,{actionsVisible ? 0.5 : 0}); transition: background {ANIM_DURATION}ms ease;"
				onclick={closeActions}
				tabindex={-1}
				aria-label={m['bulkActions.close_bulk']()}
			></Button>

			<div
				class="relative z-10 w-full overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-2xl {activeCommand ===
				'due_date'
					? 'max-w-[31rem]'
					: 'max-w-lg'}"
				style="opacity: {actionsVisible ? 1 : 0}; transform: scale({actionsVisible
					? 1
					: 0.95}); transition: opacity {ANIM_DURATION}ms ease, transform {ANIM_DURATION}ms ease;"
			>
				<div class="sr-only">
					<h2>{m['bulkActions.title']()}</h2>
					<p>{m['bulkActions.a11y_description']()}</p>
				</div>

				<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-3">
					{#if activeCommand}
						<Button variant="plain" onclick={backToCommands}
							class="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							title={m['bulkActions.back']()}
						>
							<ChevronLeft size={16} />
						</Button>
					{:else}
						<Search size={16} class="text-[var(--color-text-tertiary)]" />
					{/if}
					{#if activeCommand === 'due_date'}
						<div
							id="bulk-actions-search"
							class="min-w-0 flex-1 py-4 text-sm font-medium text-[var(--color-text-primary)]"
							tabindex="-1"
						>
							{m['bulkActions.due_date.choose']()}
						</div>
					{:else}
						<!-- svelte-ignore a11y_autofocus -->
						<Input variant="plain"
							id="bulk-actions-search"
							type="text"
							aria-label={activeTitle}
							bind:value={searchQuery}
							placeholder={searchPlaceholder}
							autofocus
							class="min-w-0 flex-1 bg-transparent py-4 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
						/>
					{/if}
					<Button variant="plain" onclick={closeActions}
						class="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title={m['bulkActions.close']()}
					>
						<X size={16} />
					</Button>
				</div>

				<div class="max-h-[60vh] min-h-72 overflow-y-auto p-2">
					{#if !activeCommand}
						{#if filteredCommands.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.no_actions']()}
							</div>
						{:else}
							{#each filteredCommands as command, index (command.id)}
								<Button variant="plain" id={`bulk-action-row-${index}`}
									class={commandButtonClass}
									data-selected={selectedIndex === index}
									onpointerenter={() => (selectedIndex = index)}
									onclick={() => selectCommand(command.id)}
								>
									<span
										class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]"
									>
										{#if commandIcon(command.id) === 'assignee'}
											<Users size={16} />
										{:else if commandIcon(command.id) === 'status'}
											<CircleDot size={16} />
										{:else if commandIcon(command.id) === 'priority'}
											<Flag size={16} />
										{:else if commandIcon(command.id) === 'label'}
											<Tag size={16} />
										{:else if commandIcon(command.id) === 'cycle'}
											<RefreshCw size={16} />
										{:else if commandIcon(command.id) === 'due_date'}
											<CalendarDays size={16} />
										{:else if commandIcon(command.id) === 'parent'}
											<CornerDownRight size={16} />
										{:else if commandIcon(command.id) === 'subissue'}
											<GitBranch size={16} />
										{:else if commandIcon(command.id) === 'duplicate'}
											<Copy size={16} />
										{:else if commandIcon(command.id) === 'related'}
											<Link size={16} />
										{:else}
											<X size={16} />
										{/if}
									</span>
									<span class="min-w-0 flex-1">
										<span class="block truncate font-medium text-[var(--color-text-primary)]">{command.title}</span>
										<span class="block truncate text-xs text-[var(--color-text-tertiary)]">{command.description}</span>
									</span>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'assignee'}
						<div
							class="mb-2 flex items-center justify-between gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg)]/50 px-3 py-2"
						>
							<span class="text-xs text-[var(--color-text-tertiary)]"
								>{m['bulkActions.assignee.selected']({
									n: selectedAssigneeIds.length,
									s: _s(selectedAssigneeIds.length)
								})}</span
							>
							<Button variant="plain" class="rounded-md bg-[var(--app-accent)] px-2.5 py-1 text-xs font-medium text-[var(--app-accent-foreground)] disabled:opacity-50"
								disabled={selectedAssigneeIds.length === 0}
								onclick={() => bulkSetAssignees()}
							>
								{m['bulkActions.assignee.assign']()}
							</Button>
						</div>
						<Button variant="plain" id="bulk-action-row-0"
							class={optionButtonClass}
							data-selected={selectedIndex === 0}
							onpointerenter={() => (selectedIndex = 0)}
							onclick={() => bulkSetAssignees([])}
						>
							<X size={14} class="text-[var(--color-text-tertiary)]" />
							<span class="truncate text-[var(--color-text-tertiary)]">{m['bulkActions.assignee.clear']()}</span>
						</Button>
						{#if filteredMembers.length === 0}
							<div class="py-8 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.assignee.no_members']()}
							</div>
						{:else}
							{#each filteredMembers as member, index (member.user_id)}
								{@const rowIndex = index + 1}
								{@const isSelected = selectedAssigneeIds.includes(member.user_id)}
								<Button variant="plain" id={`bulk-action-row-${rowIndex}`}
									class={optionButtonClass}
									data-selected={selectedIndex === rowIndex}
									onpointerenter={() => (selectedIndex = rowIndex)}
									onclick={() => toggleAssignee(member.user_id)}
								>
									<span
										class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[10px] text-[var(--app-accent-foreground)]"
										>{(member.name || member.email).charAt(0).toUpperCase()}</span
									>
									<span class="min-w-0 flex-1 truncate">{member.name || member.email}</span>
									<span
										class="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-[var(--app-border)] text-[10px] {isSelected
											? 'bg-[var(--app-accent)] text-[var(--app-accent-foreground)]'
											: 'text-transparent'}">✓</span
									>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'status'}
						{#if filteredStatuses.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.no_statuses']()}
							</div>
						{:else}
							{#each filteredStatuses as status, index (status.id)}
								<Button variant="plain" id={`bulk-action-row-${index}`}
									class={optionButtonClass}
									data-selected={selectedIndex === index}
									onpointerenter={() => (selectedIndex = index)}
									onclick={() => bulkSetStatus(status.id)}
								>
									<IssueStatusIcon category={status.category} color={status.color} size={14} />
									<span class="truncate">{status.name}</span>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'priority'}
						{#if filteredPriorities.length === 0}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.no_priorities']()}
							</div>
						{:else}
							{#each filteredPriorities as priority, index (priority)}
								<Button variant="plain" id={`bulk-action-row-${index}`}
									class={optionButtonClass}
									data-selected={selectedIndex === index}
									onpointerenter={() => (selectedIndex = index)}
									onclick={() => bulkSetPriority(priority)}
								>
									<IssuePriorityIcon {priority} size={14} />
									<span class="truncate">{getPriorityLabel(priority)}</span>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'label'}
						{#if canCreateLabel}
							<Button variant="plain" id="bulk-action-row-0"
								class={optionButtonClass}
								data-selected={selectedIndex === 0}
								onpointerenter={() => (selectedIndex = 0)}
								onclick={bulkCreateAndAddLabel}
								disabled={creatingLabel}
							>
								<Plus size={14} />
								<span class="truncate"
									>{creatingLabel
										? m['bulkActions.label.creating']()
										: m['bulkActions.label.create']({ name: searchQuery.trim() })}</span
								>
							</Button>
						{/if}
						{#if filteredLabels.length === 0 && !canCreateLabel}
							<div class="py-12 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.no_labels']()}
							</div>
						{:else}
							{#each filteredLabels as label, index (label.id)}
								{@const rowIndex = index + (canCreateLabel ? 1 : 0)}
								<Button variant="plain" id={`bulk-action-row-${rowIndex}`}
									class={optionButtonClass}
									data-selected={selectedIndex === rowIndex}
									onpointerenter={() => (selectedIndex = rowIndex)}
									onclick={() => bulkAddLabel(label.id)}
								>
									<span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background-color: {label.color}"></span>
									<span class="truncate">{label.name}</span>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'cycle'}
						<Button variant="plain" id="bulk-action-row-0"
							class={optionButtonClass}
							data-selected={selectedIndex === 0}
							onpointerenter={() => (selectedIndex = 0)}
							onclick={() => bulkSetCycle(null)}
						>
							<RefreshCw size={14} class="text-[var(--color-text-tertiary)]" />
							<span class="truncate text-[var(--color-text-tertiary)]">{m['bulkActions.cycle.no_cycle']()}</span>
						</Button>
						{#if filteredCycles.length === 0}
							<div class="py-8 text-center text-xs text-[var(--color-text-tertiary)]">
								{m['bulkActions.no_cycles']()}
							</div>
						{:else}
							{#each filteredCycles as cycle, index (cycle.id)}
								{@const rowIndex = index + 1}
								<Button variant="plain" id={`bulk-action-row-${rowIndex}`}
									class={optionButtonClass}
									data-selected={selectedIndex === rowIndex}
									onpointerenter={() => (selectedIndex = rowIndex)}
									onclick={() => bulkSetCycle(cycle.id)}
								>
									<RefreshCw size={14} class="text-[var(--color-text-tertiary)]" />
									<span class="truncate">{cycle.name}</span>
								</Button>
							{/each}
						{/if}
					{:else if activeCommand === 'due_date'}
						<DueDatePickerPanel value={null} onchange={bulkSetDueDate} close={closeActions} />
					{/if}
				</div>
			</div>
		</div>
	{/if}

	<IssuePickerDialog
		bind:open={parentPickerOpen}
		{slug}
		title={parentPickerTitle}
		description={parentPickerDescription}
		actionLabel={m['bulkActions.parent_picker.set_parent']()}
		excludeIds={Array.from(issuesState.selectedIds)}
		onselect={bulkSetParent}
	/>

	<IssuePickerDialog
		bind:open={relationPickerOpen}
		{slug}
		title={relationPickerTitle}
		description={relationPickerDescription}
		actionLabel={relationPickerType === 'duplicate'
			? m['bulkActions.relation_picker.mark_duplicate']()
			: m['bulkActions.relation_picker.relate']()}
		excludeIds={Array.from(issuesState.selectedIds)}
		onselect={bulkAddRelation}
	/>

	<AlertDialog.Root bind:open={deleteOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title
					>{m['bulkActions.delete.title']({
						n: issuesState.selectionCount,
						s: _s(issuesState.selectionCount)
					})}</AlertDialog.Title
				>
				<AlertDialog.Description>{m['bulkActions.delete.desc']()}</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel
					variant="outline"
					class="border-[var(--app-border)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
				>
					{m['bulkActions.delete.cancel']()}
				</AlertDialog.Cancel>
				<AlertDialog.Action variant="destructive" class="bg-red-600 text-white hover:bg-red-700" onclick={bulkDelete}>
					{m['bulkActions.delete.confirm']()}
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>

	<AlertDialog.Root bind:open={unparentOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title
					>{m['bulkActions.unparent.title']({
						n: issuesState.selectionCount,
						s: _s(issuesState.selectionCount)
					})}</AlertDialog.Title
				>
				<AlertDialog.Description>{m['bulkActions.unparent.desc']()}</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel
					variant="outline"
					class="border-[var(--app-border)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
				>
					{m['bulkActions.unparent.cancel']()}
				</AlertDialog.Cancel>
				<AlertDialog.Action
					variant="destructive"
					class="bg-red-600 text-white hover:bg-red-700"
					onclick={bulkRemoveParent}
				>
					{m['bulkActions.unparent.confirm']()}
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{/if}
