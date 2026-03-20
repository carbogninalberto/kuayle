<script lang="ts">
	import type { Issue, RelationType, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import IssueContextMenu from './IssueContextMenu.svelte';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as Popover from '$lib/components/ui/popover';
	import { issuesState } from './issues.state.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { CalendarDays, CircleUser } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';

	let {
		issue,
		slug = '',
		members = [],
		labels = [],
		projects = [],
		cycles = [],
		onclick,
		lastSelectedId = null,
		onlastselected,
		onaddrelation
	}: {
		issue: Issue;
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		projects?: Project[];
		cycles?: Cycle[];
		onclick: (issue: Issue) => void;
		lastSelectedId?: string | null;
		onlastselected?: (id: string) => void;
		onaddrelation?: (issue: Issue, type: RelationType) => void;
	} = $props();

	const isSelected = $derived(issuesState.selectedIds.has(issue.id));
	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	let editingTitle = $state(false);
	let titleValue = $state('');
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);

	function startEditing(e: MouseEvent) {
		e.stopPropagation();
		e.preventDefault();
		editingTitle = true;
		titleValue = issue.title;
	}

	async function saveTitle() {
		editingTitle = false;
		if (titleValue.trim() && titleValue !== issue.title) {
			try {
				await issuesState.update(slug, issue.identifier, { title: titleValue.trim() });
			} catch {
				toast.error('Failed to update title');
			}
		}
	}

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
		} catch {
			toast.error(`Failed to update ${field}`);
		}
	}

	function handleCheckboxChange(e: Event) {
		e.stopPropagation();
		if (e instanceof MouseEvent && e.shiftKey && lastSelectedId) {
			issuesState.selectRange(lastSelectedId, issue.id);
		} else {
			issuesState.toggleSelect(issue.id);
		}
		onlastselected?.(issue.id);
	}

	function handleClick(e: MouseEvent) {
		if (e.shiftKey && lastSelectedId) {
			e.preventDefault();
			issuesState.selectRange(lastSelectedId, issue.id);
			onlastselected?.(issue.id);
		} else {
			onclick(issue);
		}
	}
</script>

<IssueContextMenu {issue} {slug} {members} {labels} {projects} {cycles} onaddrelation={(type) => onaddrelation?.(issue, type)}>
	<button
		class="group mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md px-3 py-1.5 text-left transition-colors duration-100 hover:bg-black/[0.02] dark:hover:bg-white/[0.02] {isSelected ? 'bg-black/[0.02] dark:bg-white/[0.02]' : ''}"
		onclick={handleClick}
		draggable="true"
		ondragstart={(e) => {
			e.dataTransfer?.setData('text/plain', issue.identifier);
			e.dataTransfer?.setData('application/issue-id', issue.id);
			if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
		}}
	>
		<!-- Checkbox hover zone -->
		<span
			class="shrink-0 -my-1.5 -ml-3 flex items-center justify-center pl-3 pr-1 self-stretch transition-opacity duration-100 {isSelected ? 'opacity-100' : 'opacity-0 hover:opacity-100'}"
			onclick={handleCheckboxChange}
			onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleCheckboxChange(e); }}
			role="checkbox"
			aria-checked={isSelected}
			tabindex={0}
		>
			<Checkbox checked={isSelected} />
		</span>

		<!-- Priority -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<span class="shrink-0 flex items-center" onclick={(e) => e.stopPropagation()}>
			<Popover.Root bind:open={priorityOpen}>
				<Popover.Trigger class="flex items-center cursor-pointer rounded p-0.5 opacity-60 hover:opacity-100 hover:bg-[var(--color-bg-tertiary)] transition-all">
					<IssuePriorityIcon priority={issue.priority} size={14} />
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="start">
					{#each priorityValues as value}
						<button
							onclick={() => { updateField('priority', value); priorityOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<IssuePriorityIcon priority={value} size={14} />
							{PRIORITY_LABELS[value]}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		</span>

		<!-- Identifier -->
		<span class="w-[3.75rem] shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)]">{issue.identifier}</span>

		<!-- Status -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<span class="shrink-0 flex items-center" onclick={(e) => e.stopPropagation()}>
			<Popover.Root bind:open={statusOpen}>
				<Popover.Trigger class="flex items-center cursor-pointer rounded p-0.5 opacity-60 hover:opacity-100 hover:bg-[var(--color-bg-tertiary)] transition-all">
					<IssueStatusIcon
						status={issue.status}
						category={issue.status_info?.category}
						color={issue.status_info?.color}
						size={14}
					/>
				</Popover.Trigger>
				<Popover.Content class="w-44 p-1" align="start">
					{#each teamStatusesState.statusOrder as ts}
						<button
							onclick={() => { updateField('status_id', ts.id); statusOpen = false; }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.status_id === ts.id ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
							{ts.name}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		</span>

		<!-- Title -->
		{#if editingTitle}
			<input
				type="text"
				bind:value={titleValue}
				onblur={saveTitle}
				onkeydown={(e) => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { editingTitle = false; } }}
				onclick={(e) => e.stopPropagation()}
				autofocus
				class="flex-1 truncate text-[13px] text-[var(--color-text-primary)] bg-transparent outline-none border-b border-[var(--app-accent)]"
			/>
		{:else}
			<span
				class="flex-1 truncate text-[13px] text-[var(--color-text-primary)]"
				ondblclick={startEditing}
			>{issue.title}</span>
		{/if}

		<!-- Labels (Linear-style: dot + name) -->
		{#if issue.labels && issue.labels.length > 0}
			<div class="hidden gap-1 shrink-0 sm:flex">
				{#each issue.labels.slice(0, 2) as label}
					<span class="flex items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] transition-colors">
						<span class="h-1.5 w-1.5 rounded-full shrink-0" style="background-color: {label.color}"></span>
						{label.name}
					</span>
				{/each}
				{#if issue.labels.length > 2}
					<span class="text-[10px] text-[var(--color-text-tertiary)]">+{issue.labels.length - 2}</span>
				{/if}
			</div>
		{/if}

		<!-- Due date -->
		{#if issue.due_date}
			{@const due = new Date(issue.due_date)}
			{@const now = new Date()}
			{@const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000)}
			<span class="group/due hidden shrink-0 items-center gap-1 rounded-full border border-[var(--app-border)] px-1.5 py-0 text-[11px] leading-5 sm:inline-flex hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] transition-colors">
				<CalendarDays size={11} class={diffDays < 0 ? 'text-red-500' : diffDays === 0 ? 'text-orange-500' : diffDays <= 7 ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} />
				<span class="text-[var(--color-text-tertiary)] group-hover/due:text-[var(--color-text-primary)] transition-colors">{due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}</span>
			</span>
		{/if}

		<!-- Assignees -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<span class="shrink-0 flex items-center" onclick={(e) => e.stopPropagation()}>
			<Popover.Root bind:open={assigneeOpen}>
				<Popover.Trigger class="flex items-center cursor-pointer transition-all">
					{#if issue.assignees && issue.assignees.length > 1}
						<div class="flex -space-x-2 rounded-full hover:ring-2 hover:ring-[var(--app-accent)]">
							<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white ring-1 ring-[var(--color-bg)]" title={issue.assignees[0].name}>
								{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
							</div>
							<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[8px] font-medium text-[var(--color-text-secondary)] ring-1 ring-[var(--color-bg)]">
								+{issue.assignees.length - 1}
							</div>
						</div>
					{:else if issue.assignees && issue.assignees.length === 1}
						<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white hover:ring-2 hover:ring-[var(--app-accent)]" title={issue.assignees[0].name}>
							{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
						</div>
					{:else if issue.assignee}
						<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white hover:ring-2 hover:ring-[var(--app-accent)]" title={issue.assignee.name}>
							{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
						</div>
					{:else}
						<span class="text-[var(--color-text-tertiary)] opacity-40 hover:opacity-100 hover:text-[var(--color-text-secondary)] transition-all">
							<CircleUser size={20} />
						</span>
					{/if}
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="end">
					<button
						onclick={() => { updateField('assignee_ids', []); assigneeOpen = false; }}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						Clear all
					</button>
					{#each members as member}
						{@const isAssigned = (issue.assignees ?? []).some(a => a.id === member.user_id)}
						<button
							onclick={() => {
								const currentIds = (issue.assignees ?? []).map(a => a.id);
								const newIds = isAssigned ? currentIds.filter(id => id !== member.user_id) : [...currentIds, member.user_id];
								updateField('assignee_ids', newIds);
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {isAssigned ? 'bg-[var(--color-bg-hover)]' : ''}"
						>
							<div class="flex h-4 w-4 items-center justify-center rounded-full {isAssigned ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]'} text-[8px]">
								{isAssigned ? '✓' : (member.name || member.email || 'U').charAt(0).toUpperCase()}
							</div>
							{member.name || member.email}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		</span>

		<!-- Created -->
		{#if issue.created_at}
			<span class="hidden shrink-0 text-[11px] text-[var(--color-text-tertiary)] sm:inline">
				{formatRelativeTime(issue.created_at)}
			</span>
		{/if}
	</button>
</IssueContextMenu>
