<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { Switch } from '$lib/components/ui/switch';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Cycle } from '$lib/types/cycle';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import type { IssueTemplate } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { StatusCategory } from '$lib/types/team-status';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import { StatusSelector, PrioritySelector, AssigneeSelector, LabelSelector, ProjectSelector, CycleSelector, TeamSelector } from './selectors';
	import { listTemplates } from '$lib/api/issue-templates';
	import {
		User,
		Tag,
		FolderKanban,
		FileText
	} from 'lucide-svelte';

	let {
		open = $bindable(false),
		slug = '',
		teams,
		projects = [],
		labels = [],
		members = [],
		cycles = [],
		defaultTeamId,
		defaultStatus,
		defaultStatusId,
		defaultPriority,
		defaultAssigneeId,
		defaultTitle,
		onsubmit
	}: {
		open: boolean;
		slug?: string;
		teams: Team[];
		projects?: Project[];
		labels?: Label[];
		members?: WorkspaceMember[];
		cycles?: Cycle[];
		defaultTeamId?: string;
		defaultStatus?: IssueStatus;
		defaultStatusId?: string;
		defaultPriority?: IssuePriority;
		defaultAssigneeId?: string;
		defaultTitle?: string;
		onsubmit: (req: {
			title: string;
			description?: string;
			status?: IssueStatus;
			status_id?: string;
			priority: IssuePriority;
			team_id: string;
			project_id?: string;
			assignee_id?: string;
			assignee_ids?: string[];
			label_ids?: string[];
			due_date?: string;
			cycle_id?: string;
		}) => void;
	} = $props();

	let title = $state('');
	let description = $state('');
	let statusId = $state<string>('');
	let priority = $state<IssuePriority>(0);
	let teamId = $state('');
	let projectId = $state<string | null>(null);
	let assigneeIds = $state<string[]>([]);
	let labelIds = $state<string[]>([]);
	let dueDate = $state<string | null>(null);
	let cycleId = $state<string | null>(null);
	let createMore = $state(false);

	let templates = $state<IssueTemplate[]>([]);
	let templateOpen = $state(false);
	let descriptionVersion = $state(0);

	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let teamOpen = $state(false);
	let projectOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);
	let cycleOpen = $state(false);

	function resetForm() {
		title = defaultTitle ?? '';
		description = '';
		descriptionVersion++;
		statusId = defaultStatusId ?? teamStatusesState.defaultForCategory('backlog')?.id ?? '';
		priority = defaultPriority ?? 0;
		teamId = defaultTeamId ?? teams[0]?.id ?? '';
		projectId = null;
		assigneeIds = defaultAssigneeId ? [defaultAssigneeId] : [];
		labelIds = [];
		dueDate = null;
		cycleId = null;
		if (slug) listTemplates(slug).then(t => templates = t).catch(() => {});
	}

	$effect(() => {
		if (open) {
			untrack(() => resetForm());
		}
	});

	let selectedTeam = $derived(teams.find((t) => t.id === teamId));
	let selectedProject = $derived(projects.find((p) => p.id === projectId));
	let selectedAssignees = $derived(members.filter((m) => assigneeIds.includes(m.user_id)));
	let selectedLabels = $derived(labels.filter((l) => labelIds.includes(l.id)));

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	const selectedStatus = $derived(teamStatusesState.statusById.get(statusId));

	function handleSubmit() {
		if (!title.trim() || !teamId) return;
		onsubmit({
			title: title.trim(),
			description: description.trim() || undefined,
			status_id: statusId || undefined,
			priority,
			team_id: teamId,
			project_id: projectId || undefined,
			assignee_ids: assigneeIds.length > 0 ? assigneeIds : undefined,
			label_ids: labelIds.length > 0 ? labelIds : undefined,
			due_date: dueDate || undefined,
			cycle_id: cycleId || undefined
		});
		if (createMore) {
			title = '';
			description = '';
		} else {
			open = false;
		}
	}

	const STATUS_TO_CATEGORY: Record<string, StatusCategory> = {
		backlog: 'backlog',
		todo: 'unstarted',
		in_progress: 'started',
		in_review: 'started',
		done: 'completed',
		cancelled: 'cancelled',
	};

	function applyTemplate(tmpl: IssueTemplate) {
		title = tmpl.title || '';
		description = tmpl.description ?? '';
		descriptionVersion = Date.now();
		priority = tmpl.priority ?? 0;
		labelIds = Array.isArray(tmpl.label_ids) ? tmpl.label_ids : [];
		if (tmpl.assignee_id) assigneeIds = [tmpl.assignee_id];
		if (tmpl.status) {
			const category = STATUS_TO_CATEGORY[tmpl.status];
			if (category) {
				const defaultStatus = teamStatusesState.defaultForCategory(category);
				if (defaultStatus) statusId = defaultStatus.id;
			}
		}
		templateOpen = false;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
			handleSubmit();
		}
	}

	function toggleLabel(id: string) {
		if (labelIds.includes(id)) {
			labelIds = labelIds.filter((l) => l !== id);
		} else {
			labelIds = [...labelIds, id];
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content
		class="sm:max-w-[640px] gap-0 overflow-hidden rounded-xl border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-0"
		onOpenAutoFocus={(e) => {
			e.preventDefault();
			const input = document.getElementById('create-issue-title');
			input?.focus();
		}}
	>
		<!-- Top bar: Team + Template -->
		<div class="flex items-center gap-1.5 px-3 pr-10 py-2">
			<TeamSelector
				bind:open={teamOpen}
				{teams}
				value={teamId}
				onchange={(id) => { teamId = id; }}
			>
				{#snippet trigger()}
					<button tabindex="-1" class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] px-2.5 py-1 text-xs font-medium text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">
						<span class="flex h-4 w-4 items-center justify-center rounded bg-[var(--app-accent)] text-[9px] font-bold text-[var(--app-accent-foreground)]">
							{selectedTeam?.key?.charAt(0) ?? 'T'}
						</span>
						{selectedTeam?.key ?? 'Team'}
					</button>
				{/snippet}
			</TeamSelector>
			<span class="text-xs text-[var(--color-text-tertiary)]">›</span>
			<span class="text-xs font-medium text-[var(--color-text-secondary)]">New Issue</span>

			{#if templates.length > 0}
				<div class="ml-auto">
					<Popover.Root bind:open={templateOpen}>
						<Popover.Trigger>
							<button tabindex="-1" class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]">
								<FileText size={12} />
								Template
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-56 p-1" align="end">
							{#each templates as tmpl (tmpl.id)}
								<button
									onclick={() => applyTemplate(tmpl)}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
								>
									<FileText size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
									<span class="truncate">{tmpl.title || 'Untitled template'}</span>
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>
			{/if}
		</div>

		<!-- Title + Description -->
		<!-- svelte-ignore a11y_autofocus -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="px-4 py-3" onkeydown={handleKeydown}>
			<input
				id="create-issue-title"
				type="text"
				bind:value={title}
				placeholder="Issue title"
				class="w-full bg-transparent text-lg font-semibold text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
			/>
			<div class="mt-4 max-h-[calc(60vh-120px)] overflow-y-auto">
				{#key descriptionVersion}
				<RichEditor
					content={description}
					placeholder="Add description..."
					bubbleMenu={true}
					borderless={true}
					minHeight="120px"
					onupdate={(html) => (description = html)}
				/>
				{/key}
			</div>
		</div>

		<!-- Property pills -->
		<div class="flex flex-wrap items-center gap-1.5 px-4 py-2.5">
			<!-- Status -->
			<StatusSelector
				bind:open={statusOpen}
				statuses={teamStatusesState.statusOrder}
				value={statusId}
				onchange={(id) => { statusId = id; }}
				width="w-44"
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<IssueStatusIcon category={selectedStatus?.category} color={selectedStatus?.color} size={12} />
						{selectedStatus?.name ?? 'Status'}
					</button>
				{/snippet}
			</StatusSelector>

			<!-- Priority -->
			<PrioritySelector
				bind:open={priorityOpen}
				value={priority}
				onchange={(p) => { priority = p; }}
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<IssuePriorityIcon {priority} size={12} />
						{PRIORITY_LABELS[priority]}
					</button>
				{/snippet}
			</PrioritySelector>

			<!-- Project -->
			<ProjectSelector
				bind:open={projectOpen}
				{projects}
				value={projectId}
				onchange={(id) => { projectId = id; }}
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {selectedProject ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						<FolderKanban size={12} />
						{selectedProject?.name ?? 'Project'}
					</button>
				{/snippet}
			</ProjectSelector>

			<!-- Assignees -->
			<AssigneeSelector
				bind:open={assigneeOpen}
				{members}
				value={assigneeIds}
				onchange={(userId) => {
					if (assigneeIds.includes(userId)) {
						assigneeIds = assigneeIds.filter(id => id !== userId);
					} else {
						assigneeIds = [...assigneeIds, userId];
					}
				}}
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {selectedAssignees.length > 0 ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						<User size={12} />
						{#if selectedAssignees.length === 0}
							Assignee
						{:else if selectedAssignees.length === 1}
							{selectedAssignees[0].name || selectedAssignees[0].email}
						{:else}
							{selectedAssignees.length} assignees
						{/if}
					</button>
				{/snippet}
			</AssigneeSelector>

			<!-- Labels -->
			<LabelSelector
				bind:open={labelsOpen}
				{labels}
				value={labelIds}
				onchange={(labelId) => toggleLabel(labelId)}
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {selectedLabels.length > 0 ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						<Tag size={12} />
						{#if selectedLabels.length === 0}
							Labels
						{:else if selectedLabels.length === 1}
							{selectedLabels[0].name}
						{:else}
							{selectedLabels.length} labels
						{/if}
					</button>
				{/snippet}
			</LabelSelector>

			<!-- Cycle -->
			<CycleSelector
				bind:open={cycleOpen}
				cycles={cycles ?? []}
				value={cycleId}
				onchange={(id) => { cycleId = id; }}
			>
				{#snippet trigger()}
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {cycleId ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						{#if cycleId}
							{cycles?.find(c => c.id === cycleId)?.name ?? 'Cycle'}
						{:else}
							Cycle
						{/if}
					</button>
				{/snippet}
			</CycleSelector>

			<!-- Due Date -->
			<DatePickerPopover
				value={dueDate}
				onchange={(d) => (dueDate = d)}
				placeholder="Due date"
			/>
		</div>

		<!-- Footer -->
		<div class="flex items-center justify-end gap-3 px-4 py-2.5">
			<label class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
				<Switch bind:checked={createMore} size="sm" />
				Create more
			</label>
			<Button
				size="sm"
				disabled={!title.trim() || !teamId}
				onclick={handleSubmit}
			>
				Create issue
			</Button>
		</div>
	</Dialog.Content>
</Dialog.Root>
