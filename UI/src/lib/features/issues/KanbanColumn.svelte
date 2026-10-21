<script lang="ts">
	import type { Issue, RelationType } from '$lib/types/issue';
	import type { TeamStatus } from '$lib/types/team-status';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Project } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import type { Label } from '$lib/types/label';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import SubIssueCounterTag from './SubIssueCounterTag.svelte';
	import IssueContextMenu from './IssueContextMenu.svelte';
	import { dndzone } from 'svelte-dnd-action';

	let {
		statusId,
		teamStatus,
		issues,
		slug = '',
		members = [],
		labels = [],
		projects = [],
		cycles = [],
		membershipClearValue = null,
		readOnly = false,
		restrictToTeam = false,
		accessibleRow = false,
		canDeleteIssue,
		onremove,
		removeLabel,
		onchanged,
		onaddrelation,
		onissueclick,
		onconsider,
		onfinalize
	}: {
		statusId: string;
		teamStatus: TeamStatus;
		issues: Issue[];
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		projects?: Project[];
		cycles?: Cycle[];
		readOnly?: boolean;
		membershipClearValue?: '' | null;
		restrictToTeam?: boolean;
		accessibleRow?: boolean;
		canDeleteIssue?: (issue: Issue) => boolean;
		onremove?: (issue: Issue) => void;
		removeLabel?: string;
		onchanged?: () => void;
		onaddrelation?: (issue: Issue, type: RelationType) => void;
		onissueclick: (issue: Issue) => void;
		onconsider?: (statusId: string, items: Issue[]) => void;
		onfinalize?: (statusId: string, items: Issue[]) => void;
	} = $props();

	function handleConsider(e: CustomEvent<{ items: Issue[] }>) {
		onconsider?.(statusId, e.detail.items);
	}

	function handleFinalize(e: CustomEvent<{ items: Issue[] }>) {
		onfinalize?.(statusId, e.detail.items);
	}
</script>

<div class="flex w-72 shrink-0 flex-col">
	<div class="flex items-center gap-2 px-2 py-2">
		<IssueStatusIcon category={teamStatus.category} color={teamStatus.color} />
		<span class="text-sm font-medium text-[var(--color-text-primary)]">{teamStatus.name}</span>
		<span class="text-xs text-[var(--color-text-tertiary)]">{issues.length}</span>
	</div>

	<div
		class="flex-1 space-y-1.5 overflow-y-auto px-1 pb-4 min-h-[60px]"
		use:dndzone={{
			items: issues,
			type: restrictToTeam ? `issue-${teamStatus.team_id}` : 'issue',
			dragDisabled: readOnly,
			dropFromOthersDisabled: readOnly,
			dropTargetStyle: {}
		}}
		onconsider={handleConsider}
		onfinalize={handleFinalize}
	>
		{#each issues as issue (issue.id)}
			<IssueContextMenu
				{issue}
				{slug}
				{members}
				{labels}
				{projects}
				cycles={cycles.filter((cycle) => !restrictToTeam || cycle.team_id === issue.team_id)}
				{readOnly}
				{membershipClearValue}
				{restrictToTeam}
				canDelete={canDeleteIssue?.(issue) ?? true}
				onremove={onremove ? () => onremove?.(issue) : undefined}
				{removeLabel}
				{onchanged}
				onaddrelation={onaddrelation ? (type) => onaddrelation?.(issue, type) : undefined}
			>
				<svelte:element
					this={accessibleRow ? 'div' : 'button'}
					role={accessibleRow ? 'button' : undefined}
					tabindex={accessibleRow ? 0 : undefined}
					onkeydown={(e: KeyboardEvent) => {
						if (accessibleRow && e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
							e.preventDefault();
							onissueclick(issue);
						}
					}}
					class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-left transition-colors hover:border-[var(--app-border-hover)]"
					onclick={() => onissueclick(issue)}
				>
					<div class="flex items-center gap-2">
						<span class="text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
						<IssuePriorityIcon priority={issue.priority} size={14} />
					</div>
					<div class="mt-1 flex items-start gap-2">
						<p class="line-clamp-2 min-w-0 flex-1 text-sm text-[var(--color-text-primary)]">{issue.title}</p>
						<SubIssueCounterTag {issue} {slug} {members} onclickissue={onissueclick} compact />
					</div>
				</svelte:element>
			</IssueContextMenu>
		{/each}
	</div>
</div>
