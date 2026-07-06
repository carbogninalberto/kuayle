<script lang="ts">
	import type { Issue, RelationType } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Project } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import IssueRow from './IssueRow.svelte';
	import SubIssuesList from './SubIssuesList.svelte';

	let {
		issue,
		slug,
		members = [],
		labels = [],
		projects = [],
		cycles = [],
		lastSelectedId = null,
		singleSelect = false,
		onclick,
		onlastselected,
		onaddrelation,
		onupdated
	}: {
		issue: Issue;
		slug: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		projects?: Project[];
		cycles?: Cycle[];
		lastSelectedId?: string | null;
		singleSelect?: boolean;
		onclick: (issue: Issue) => void;
		onlastselected?: (id: string) => void;
		onaddrelation?: (issue: Issue, type: RelationType) => void;
		onupdated?: () => void | Promise<void>;
	} = $props();
</script>

<div>
	<IssueRow
		{issue}
		{slug}
		{members}
		{labels}
		{projects}
		{cycles}
		{lastSelectedId}
		{singleSelect}
		{onclick}
		onlastselected={onlastselected}
		onaddrelation={onaddrelation}
	/>
	{#if (issue.sub_issue_count ?? 0) > 0}
		<div class="ml-8 flex">
			<svg class="mr-1 shrink-0" width="14" height="100%" viewBox="0 0 14 28" preserveAspectRatio="xMinYMin" fill="none" aria-hidden="true">
				<path d="M1 0 L1 18 C1 23, 5 23, 9 23 L14 23" stroke="var(--color-text-tertiary)" stroke-width="1.5" opacity="0.4" fill="none" />
			</svg>
			<div class="min-w-0 flex-1">
				<SubIssuesList
					{slug}
					identifier={issue.identifier}
					subIssueCount={issue.sub_issue_count ?? 0}
					subIssueDone={issue.sub_issue_done ?? 0}
					{members}
					defaultOpen={true}
					showHeader={false}
					editable={false}
					onclickissue={onclick}
					{onupdated}
				/>
			</div>
		</div>
	{/if}
</div>
