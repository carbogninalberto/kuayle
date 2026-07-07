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
	import SubIssueCounterTag from './SubIssueCounterTag.svelte';
	import IssueLabelChips from './IssueLabelChips.svelte';
	import IssueContextMenu from './IssueContextMenu.svelte';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as HoverCard from '$lib/components/ui/hover-card';
	import * as Popover from '$lib/components/ui/popover';
	import { issuesState } from './issues.state.svelte';
	import { formatRelativeTime, formatDate } from '$lib/utils/format';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';
	import { Ban, CalendarDays, CircleUser, Copy, Link, OctagonAlert, RefreshCw } from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';

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
		onaddrelation,
		singleSelect = false
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
		singleSelect?: boolean;
	} = $props();

	const isSelected = $derived(issuesState.selectedIds.has(issue.id));
	const issueCycle = $derived(issue.cycle_id ? cycles.find(c => c.id === issue.cycle_id) : null);
	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];
	const isMobile = new IsMobile();
	const relatedCount = $derived(issue.relation_counts?.related ?? 0);
	const blockedByCount = $derived(issue.relation_counts?.blocked_by ?? 0);
	const blockingCount = $derived(issue.relation_counts?.blocking ?? 0);
	const duplicateCount = $derived(issue.relation_counts?.duplicate ?? 0);
	const createdAtText = $derived(issue.created_at ? formatRelativeTime(issue.created_at) : '');
	const createdAtTooltip = $derived(createdAtText ? `${createdAtText} • ${formatDate(issue.created_at)}` : '');
	const relatedIssues = $derived(issue.relation_summary?.related ?? []);
	const blockedByIssues = $derived(issue.relation_summary?.blocked_by ?? []);
	const blockingIssues = $derived(issue.relation_summary?.blocking ?? []);
	const duplicateIssues = $derived(issue.relation_summary?.duplicate ?? []);
	const relationBadges = $derived([
		{
			label: 'Related',
			count: relatedCount,
			issues: relatedIssues,
			Icon: Link,
			triggerClass: 'border-[var(--app-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-tertiary)] hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]',
			headerClass: 'text-[var(--color-text-secondary)]',
			fallback: `${relatedCount} related ${relatedCount === 1 ? 'issue' : 'issues'}.`
		},
		{
			label: 'Blocked by',
			count: blockedByCount,
			issues: blockedByIssues,
			Icon: Ban,
			triggerClass: 'border-red-500/20 bg-red-500/10 text-red-500 hover:border-red-500/40 hover:bg-red-500/15 hover:text-red-400',
			headerClass: 'text-red-500',
			fallback: `${blockedByCount} ${blockedByCount === 1 ? 'issue is' : 'issues are'} blocking this.`
		},
		{
			label: 'Blocking',
			count: blockingCount,
			issues: blockingIssues,
			Icon: OctagonAlert,
			triggerClass: 'border-red-500/20 bg-red-500/10 text-red-500 hover:border-red-500/40 hover:bg-red-500/15 hover:text-red-400',
			headerClass: 'text-red-500',
			fallback: `This blocks ${blockingCount} ${blockingCount === 1 ? 'issue' : 'issues'}.`
		},
		{
			label: 'Duplicate',
			count: duplicateCount,
			issues: duplicateIssues,
			Icon: Copy,
			triggerClass: 'border-purple-400/20 bg-purple-400/10 text-purple-300 hover:border-purple-400/40 hover:bg-purple-400/15 hover:text-purple-200',
			headerClass: 'text-purple-300',
			fallback: `${duplicateCount} duplicate ${duplicateCount === 1 ? 'issue' : 'issues'}.`
		}
	].filter((badge) => badge.count > 0));

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
				appToast.error('Failed to update title');
			}
		}
	}

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
		} catch {
			appToast.error(`Failed to update ${field}`);
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
		if (!singleSelect && e.shiftKey && lastSelectedId) {
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
		class="group flex min-h-12 w-full items-center gap-2 rounded-none border-b border-[var(--app-border)] px-3 py-2 text-left transition-colors duration-100 hover:bg-black/[0.02] dark:hover:bg-white/[0.02] sm:mx-2 sm:min-h-0 sm:w-[calc(100%-1rem)] sm:rounded-md sm:border-b-0 sm:py-1.5 {isSelected ? 'bg-black/[0.02] dark:bg-white/[0.02]' : ''}"
		onclick={handleClick}
		draggable={!isMobile.current}
		ondragstart={(e) => {
			e.dataTransfer?.setData('text/plain', issue.identifier);
			e.dataTransfer?.setData('application/issue-id', issue.id);
			if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
		}}
	>
		{#if !singleSelect}
			<!-- Checkbox hover zone -->
			<span
				class="-ml-1 flex h-8 shrink-0 items-center justify-center pr-1 transition-opacity duration-100 sm:-my-1.5 sm:-ml-3 sm:h-auto sm:self-stretch sm:pl-3 {isSelected ? 'opacity-100' : 'opacity-100 sm:opacity-0 sm:hover:opacity-100'}"
				onclick={handleCheckboxChange}
				onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleCheckboxChange(e); }}
				role="checkbox"
				aria-checked={isSelected}
				tabindex={0}
			>
				<Checkbox checked={isSelected} />
			</span>
		{/if}

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
		<span class="w-auto shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)] sm:w-[3.75rem]">{issue.identifier}</span>

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
		<div class="min-w-0 flex-1">
			{#if editingTitle}
				<input
					type="text"
					bind:value={titleValue}
					onblur={saveTitle}
					onkeydown={(e) => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { editingTitle = false; } }}
					onclick={(e) => e.stopPropagation()}
					class="w-full truncate text-[13px] text-[var(--color-text-primary)] bg-transparent outline-none border-b border-[var(--app-accent)]"
				/>
			{:else}
				<div class="flex min-w-0 flex-1 items-center gap-1">
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<span
						role="textbox"
						tabindex={0}
						class="min-w-0 shrink truncate text-sm leading-5 text-[var(--color-text-primary)] sm:text-[13px] sm:leading-normal"
						ondblclick={startEditing}
					>{issue.title}</span>

					<SubIssueCounterTag issue={issue} {slug} {members} onclickissue={onclick} compact />

					{#if relationBadges.length > 0}
						<span class="inline-flex shrink-0 items-center gap-1" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="presentation">
							{#each relationBadges as badge (badge.label)}
								{@const Icon = badge.Icon}
								<HoverCard.Root openDelay={150} closeDelay={100}>
									<HoverCard.Trigger
										class="inline-flex cursor-default items-center gap-1 rounded-full border px-1.5 py-0 text-[11px] leading-5 transition-colors {badge.triggerClass}"
										title={`${badge.label} ${badge.count} ${badge.count === 1 ? 'issue' : 'issues'}`}
									>
										<Icon size={10} />
										{badge.count}
									</HoverCard.Trigger>
									<HoverCard.Content class="w-56 p-2" align="end">
										<div class="flex items-center gap-2 text-xs {badge.headerClass}">
											<Icon size={13} />
											<span class="font-medium">{badge.label}</span>
										</div>
										{#if badge.issues.length > 0}
											<div class="mt-2 max-h-48 space-y-1 overflow-y-auto">
												{#each badge.issues as relatedIssue (relatedIssue.id)}
													<a
														href="/{slug}/issue/{relatedIssue.identifier}"
														onclick={(e) => e.stopPropagation()}
														title={`Open ${relatedIssue.identifier}`}
														class="flex min-w-0 items-center gap-2 rounded-md px-1 py-1 text-xs text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
													>
														<IssueStatusIcon status={relatedIssue.status} category={relatedIssue.status_info?.category} color={relatedIssue.status_info?.color} size={12} />
														<span class="shrink-0 tabular-nums text-[var(--color-text-tertiary)]">{relatedIssue.identifier}</span>
														<span class="min-w-0 flex-1 truncate">{relatedIssue.title}</span>
													</a>
												{/each}
											</div>
										{:else}
											<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
												{badge.fallback}
											</p>
										{/if}
									</HoverCard.Content>
								</HoverCard.Root>
							{/each}
						</span>
					{/if}
				</div>
			{/if}
		</div>

		<IssueLabelChips labels={issue.labels ?? []} />

		<!-- Cycle -->
		{#if issueCycle}
			<span class="hidden shrink-0 items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] transition-colors sm:inline-flex">
				<RefreshCw size={10} class="text-[var(--color-text-tertiary)]" />
				{issueCycle.name}
			</span>
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
							<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] ring-1 ring-[var(--color-bg)]" title={issue.assignees[0].name}>
								{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
							</div>
							<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[8px] font-medium text-[var(--color-text-secondary)] ring-1 ring-[var(--color-bg)]">
								+{issue.assignees.length - 1}
							</div>
						</div>
					{:else if issue.assignees && issue.assignees.length === 1}
						<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] hover:ring-2 hover:ring-[var(--app-accent)]" title={issue.assignees[0].name}>
							{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
						</div>
					{:else if issue.assignee}
						<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] hover:ring-2 hover:ring-[var(--app-accent)]" title={issue.assignee.name}>
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
							<div class="flex h-4 w-4 items-center justify-center rounded-full {isAssigned ? 'bg-[var(--app-accent)] text-[var(--app-accent-foreground)]' : 'bg-[var(--color-bg-tertiary)] text-[var(--color-text-tertiary)]'} text-[8px]">
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
				<span
					class="hidden w-[4.5rem] min-w-[4.5rem] max-w-[4.5rem] shrink-0 justify-end truncate text-right text-[11px] text-[var(--color-text-tertiary)] sm:inline-flex"
					title={createdAtTooltip}
					aria-label={createdAtTooltip}
				>{createdAtText}</span>
			{/if}
		</button>
	</IssueContextMenu>
