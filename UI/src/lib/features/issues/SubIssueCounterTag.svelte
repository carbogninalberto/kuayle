<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import * as HoverCard from '$lib/components/ui/hover-card';
	import SubIssuesList from './SubIssuesList.svelte';

	let {
		issue,
		slug,
		members = [],
		onclickissue,
		onupdated,
		compact = false
	}: {
		issue: Issue;
		slug: string;
		members?: WorkspaceMember[];
		onclickissue?: (issue: Issue) => void;
		onupdated?: () => void | Promise<void>;
		compact?: boolean;
	} = $props();

	let total = $derived(issue.sub_issue_count ?? 0);
	let done = $derived(issue.sub_issue_done ?? 0);
	let progressPercent = $derived(total > 0 ? Math.round((done / total) * 100) : 0);
	let progressOffset = $derived(31.416 - (31.416 * progressPercent) / 100);
</script>

{#if total > 0}
	<span onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="presentation">
		<HoverCard.Root openDelay={200} closeDelay={120}>
			<HoverCard.Trigger
				class="inline-flex shrink-0 cursor-default items-center gap-1.5 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)] transition-colors hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)] {compact ? 'px-1.5' : 'px-2'}"
				title={`${done}/${total} sub-issues`}
			>
					<svg class="h-3.5 w-3.5 -rotate-90" viewBox="0 0 12 12" aria-hidden="true">
						<circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="2" class="text-[var(--color-text-tertiary)] opacity-70" />
						<circle
							cx="6"
							cy="6"
							r="5"
							fill="none"
							stroke="currentColor"
							stroke-width="2"
							stroke-linecap="round"
							stroke-dasharray="31.416"
							stroke-dashoffset={progressOffset}
							class="text-[var(--color-success)]"
						/>
					</svg>
					<span>{done}/{total}</span>
			</HoverCard.Trigger>
			<HoverCard.Content class="max-h-[26rem] w-[min(34rem,calc(100vw-2rem))] overflow-y-auto p-0" align="start">
				<SubIssuesList
					{slug}
					identifier={issue.identifier}
					subIssueCount={total}
					subIssueDone={done}
					{members}
					defaultOpen={true}
					editable={false}
					{onclickissue}
					{onupdated}
				/>
			</HoverCard.Content>
		</HoverCard.Root>
	</span>
{/if}
