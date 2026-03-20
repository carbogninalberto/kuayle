<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Cycle } from '$lib/types/cycle';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS, STATUS_ORDER } from '$lib/types/issue';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import {
		User,
		Tag,
		FolderKanban
	} from 'lucide-svelte';

	let {
		open = $bindable(false),
		teams,
		projects = [],
		labels = [],
		members = [],
		cycles = [],
		defaultTeamId,
		defaultStatus,
		defaultPriority,
		defaultAssigneeId,
		onsubmit
	}: {
		open: boolean;
		teams: Team[];
		projects?: Project[];
		labels?: Label[];
		members?: WorkspaceMember[];
		cycles?: Cycle[];
		defaultTeamId?: string;
		defaultStatus?: IssueStatus;
		defaultPriority?: IssuePriority;
		defaultAssigneeId?: string;
		onsubmit: (req: {
			title: string;
			description?: string;
			status: IssueStatus;
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
	let status = $state<IssueStatus>('backlog');
	let priority = $state<IssuePriority>(0);
	let teamId = $state('');
	let projectId = $state<string | null>(null);
	let assigneeIds = $state<string[]>([]);
	let labelIds = $state<string[]>([]);
	let dueDate = $state<string | null>(null);
	let cycleId = $state<string | null>(null);
	let createMore = $state(false);

	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let teamOpen = $state(false);
	let projectOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);
	let cycleOpen = $state(false);

	$effect(() => {
		if (open) {
			title = '';
			description = '';
			status = defaultStatus ?? 'backlog';
			priority = defaultPriority ?? 0;
			teamId = defaultTeamId ?? teams[0]?.id ?? '';
			projectId = null;
			assigneeIds = defaultAssigneeId ? [defaultAssigneeId] : [];
			labelIds = [];
			dueDate = null;
			cycleId = null;
		}
	});

	let selectedTeam = $derived(teams.find((t) => t.id === teamId));
	let selectedProject = $derived(projects.find((p) => p.id === projectId));
	let selectedAssignees = $derived(members.filter((m) => assigneeIds.includes(m.user_id)));
	let selectedLabels = $derived(labels.filter((l) => labelIds.includes(l.id)));

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	function handleSubmit() {
		if (!title.trim() || !teamId) return;
		onsubmit({
			title: title.trim(),
			description: description.trim() || undefined,
			status,
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
	>
		<!-- Top bar: Team + Template -->
		<div class="flex items-center gap-1.5 border-b border-[var(--app-border)] px-4 py-2.5">
			<Popover.Root bind:open={teamOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-tertiary)] px-2.5 py-1 text-xs font-medium text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]">
						<span class="flex h-4 w-4 items-center justify-center rounded bg-[var(--app-accent)] text-[9px] font-bold text-white">
							{selectedTeam?.key?.charAt(0) ?? 'T'}
						</span>
						{selectedTeam?.key ?? 'Team'}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					{#each teams as team}
						<button
							onclick={() => { teamId = team.id; teamOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {team.id === teamId ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<span class="flex h-4 w-4 items-center justify-center rounded bg-[var(--app-accent)] text-[9px] font-bold text-white">
								{team.key.charAt(0)}
							</span>
							{team.name}
						</button>
					{/each}
					{#if teams.length === 0}
						<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No teams yet</p>
					{/if}
				</Popover.Content>
			</Popover.Root>
		</div>

		<!-- Title + Description -->
		<!-- svelte-ignore a11y_autofocus -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="px-4 pt-3" onkeydown={handleKeydown}>
			<input
				type="text"
				bind:value={title}
				placeholder="Issue title"
				autofocus
				class="w-full bg-transparent text-base font-medium text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
			/>
			<div class="mt-2">
				<RichEditor
					content={description}
					placeholder="Add description..."
					minimal={false}
					onupdate={(html) => (description = html)}
				/>
			</div>
		</div>

		<!-- Property pills -->
		<div class="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-border)] px-4 py-2.5">
			<!-- Status -->
			<Popover.Root bind:open={statusOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<IssueStatusIcon {status} size={12} />
						{STATUS_LABELS[status]}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each STATUS_ORDER as value}
						<button
							onclick={() => { status = value; statusOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {status === value ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<IssueStatusIcon status={value} size={14} />
							{STATUS_LABELS[value]}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>

			<!-- Priority -->
			<Popover.Root bind:open={priorityOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<IssuePriorityIcon {priority} size={12} />
						{PRIORITY_LABELS[priority]}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each priorityValues as value}
						<button
							onclick={() => { priority = value; priorityOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<IssuePriorityIcon priority={value} size={14} />
							{PRIORITY_LABELS[value]}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>

			<!-- Project -->
			<Popover.Root bind:open={projectOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {selectedProject ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						<FolderKanban size={12} />
						{selectedProject?.name ?? 'Project'}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					<button
						onclick={() => { projectId = null; projectOpen = false; }}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {projectId === null ? 'bg-[var(--color-bg-hover)]' : ''}"
					>
						No project
					</button>
					{#each projects as project}
						<button
							onclick={() => { projectId = project.id; projectOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {projectId === project.id ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
							{project.name}
						</button>
					{/each}
					{#if projects.length === 0}
						<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No projects yet</p>
					{/if}
				</Popover.Content>
			</Popover.Root>

			<!-- Assignees -->
			<Popover.Root bind:open={assigneeOpen}>
				<Popover.Trigger>
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
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					{#each members as member}
						<button
							onclick={() => {
								if (assigneeIds.includes(member.user_id)) {
									assigneeIds = assigneeIds.filter(id => id !== member.user_id);
								} else {
									assigneeIds = [...assigneeIds, member.user_id];
								}
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Checkbox checked={assigneeIds.includes(member.user_id)} />
							<User size={14} class="text-[var(--color-text-tertiary)]" />
							{member.name || member.email}
						</button>
					{/each}
					{#if members.length === 0}
						<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No members</p>
					{/if}
				</Popover.Content>
			</Popover.Root>

			<!-- Labels -->
			<Popover.Root bind:open={labelsOpen}>
				<Popover.Trigger>
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
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					{#each labels as label}
						<button
							onclick={() => toggleLabel(label.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Checkbox checked={labelIds.includes(label.id)} />
							<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
							<span class="truncate">{label.name}</span>
						</button>
					{/each}
					{#if labels.length === 0}
						<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No labels yet</p>
					{/if}
				</Popover.Content>
			</Popover.Root>

			<!-- Cycle -->
			<Popover.Root bind:open={cycleOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {cycleId ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						{#if cycleId}
							{cycles?.find(c => c.id === cycleId)?.name ?? 'Cycle'}
						{:else}
							Cycle
						{/if}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					<button
						onclick={() => { cycleId = null; cycleOpen = false; }}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
					>
						No cycle
					</button>
					{#each cycles ?? [] as cycle}
						<button
							onclick={() => { cycleId = cycle.id; cycleOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {cycleId === cycle.id ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							{cycle.name}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>

			<!-- Due Date -->
			<DatePickerPopover
				value={dueDate}
				onchange={(d) => (dueDate = d)}
				placeholder="Due date"
			/>
		</div>

		<!-- Footer -->
		<div class="flex items-center justify-between border-t border-[var(--app-border)] px-4 py-2.5">
			<label class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
				<Checkbox bind:checked={createMore} />
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
