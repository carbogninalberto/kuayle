<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import * as Popover from '$lib/components/ui/popover';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import {
		Circle,
		CircleDot,
		CircleDashed,
		Loader,
		CheckCircle2,
		XCircle,
		SignalHigh,
		Signal,
		SignalMedium,
		SignalLow,
		Minus,
		User,
		Tag,
		FolderKanban,
		MoreHorizontal
	} from 'lucide-svelte';

	let {
		open = $bindable(false),
		teams,
		projects = [],
		labels = [],
		members = [],
		defaultTeamId,
		onsubmit
	}: {
		open: boolean;
		teams: Team[];
		projects?: Project[];
		labels?: Label[];
		members?: WorkspaceMember[];
		defaultTeamId?: string;
		onsubmit: (req: {
			title: string;
			description?: string;
			status: IssueStatus;
			priority: IssuePriority;
			team_id: string;
			project_id?: string;
			assignee_id?: string;
			label_ids?: string[];
		}) => void;
	} = $props();

	let title = $state('');
	let description = $state('');
	let status = $state<IssueStatus>('backlog');
	let priority = $state<IssuePriority>(0);
	let teamId = $state(defaultTeamId ?? teams[0]?.id ?? '');
	let projectId = $state<string | null>(null);
	let assigneeId = $state<string | null>(null);
	let labelIds = $state<string[]>([]);
	let createMore = $state(false);

	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let teamOpen = $state(false);
	let projectOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);

	$effect(() => {
		if (open) {
			title = '';
			description = '';
			status = 'backlog';
			priority = 0;
			teamId = defaultTeamId ?? teams[0]?.id ?? '';
			projectId = null;
			assigneeId = null;
			labelIds = [];
		}
	});

	let selectedTeam = $derived(teams.find((t) => t.id === teamId));
	let selectedProject = $derived(projects.find((p) => p.id === projectId));
	let selectedAssignee = $derived(members.find((m) => m.user_id === assigneeId));
	let selectedLabels = $derived(labels.filter((l) => labelIds.includes(l.id)));

	const statusIcons: Record<IssueStatus, typeof Circle> = {
		backlog: CircleDashed,
		todo: Circle,
		in_progress: Loader,
		in_review: CircleDot,
		done: CheckCircle2,
		cancelled: XCircle
	};

	const priorityIcons: Record<IssuePriority, typeof Minus> = {
		0: Minus,
		1: SignalHigh,
		2: SignalHigh,
		3: SignalMedium,
		4: SignalLow
	};

	function handleSubmit() {
		if (!title.trim() || !teamId) return;
		onsubmit({
			title: title.trim(),
			description: description.trim() || undefined,
			status,
			priority,
			team_id: teamId,
			project_id: projectId || undefined,
			assignee_id: assigneeId || undefined,
			label_ids: labelIds.length > 0 ? labelIds : undefined
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
		<div class="px-4 pt-3" onkeydown={handleKeydown}>
			<input
				type="text"
				bind:value={title}
				placeholder="Issue title"
				autofocus
				class="w-full bg-transparent text-base font-medium text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
			/>
			<textarea
				bind:value={description}
				placeholder="Add description..."
				rows={4}
				class="mt-2 w-full resize-none bg-transparent text-sm text-[var(--color-text-secondary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
			></textarea>
		</div>

		<!-- Property pills -->
		<div class="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-border)] px-4 py-2.5">
			<!-- Status -->
			<Popover.Root bind:open={statusOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<svelte:component this={statusIcons[status]} size={12} />
						{STATUS_LABELS[status]}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each Object.entries(STATUS_LABELS) as [value, label]}
						<button
							onclick={() => { status = value as IssueStatus; statusOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {status === value ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<svelte:component this={statusIcons[value as IssueStatus]} size={14} />
							{label}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>

			<!-- Priority -->
			<Popover.Root bind:open={priorityOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						<svelte:component this={priorityIcons[priority]} size={12} />
						{PRIORITY_LABELS[priority]}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each Object.entries(PRIORITY_LABELS) as [value, label]}
						<button
							onclick={() => { priority = Number(value) as IssuePriority; priorityOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {priority === Number(value) ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<svelte:component this={priorityIcons[Number(value) as IssuePriority]} size={14} />
							{label}
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

			<!-- Assignee -->
			<Popover.Root bind:open={assigneeOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] px-2.5 py-1 text-xs {selectedAssignee ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} hover:bg-[var(--color-bg-hover)]">
						<User size={12} />
						{selectedAssignee?.name || 'Assignee'}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					<button
						onclick={() => { assigneeId = null; assigneeOpen = false; }}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {assigneeId === null ? 'bg-[var(--color-bg-hover)]' : ''}"
					>
						Unassigned
					</button>
					{#each members as member}
						<button
							onclick={() => { assigneeId = member.user_id; assigneeOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {assigneeId === member.user_id ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
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
