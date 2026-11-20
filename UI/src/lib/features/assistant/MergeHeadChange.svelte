<script module lang="ts">
	import type { PullRequestHeadChange } from '$lib/api/conversations';
	import { m } from '$lib/features/i18n/messages';

	export function mergeHeadSummary(change: PullRequestHeadChange, publishedCommit?: string) {
		const published = (publishedCommit ?? '').slice(0, 7);
		if (change.kind === 'merge') return m['assistant.merge_head_merge']({ published });
		if (change.kind === 'rewritten') return m['assistant.merge_head_rewritten']({ published });
		return m['assistant.merge_head_pushed']({ published });
	}
</script>

<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';

	// Commits others pushed after Kuayle published the pull request, on a merge
	// card (merge_pull_request or ship_pull_request): authors and whether the
	// files changed.
	let { change, publishedCommit }: { change: PullRequestHeadChange; publishedCommit?: string } = $props();
</script>

<div class="space-y-1 text-xs text-muted-foreground" data-testid="merge-head-change">
	<p>
		{mergeHeadSummary(change, publishedCommit)}
		{change.tree_changed ? m['assistant.merge_head_tree_changed']() : m['assistant.merge_head_tree_same']()}
	</p>
	<p data-testid="merge-review-stale">{m['assistant.merge_head_review_stale']()}</p>
	<ul class="list-disc space-y-0.5 pl-4">
		{#each change.commits as commit (commit.sha)}
			<li class="break-words">
				{m['assistant.merge_head_commit']({
					commit: commit.sha.slice(0, 7),
					author: commit.author,
					subject: commit.subject
				})}
			</li>
		{/each}
	</ul>
	{#if change.truncated}
		<!-- The list is a prefix: approving must not imply it is complete. -->
		<Alert.Root
			class="border-[var(--status-warning)]/40 text-[var(--status-warning)]"
			data-testid="merge-head-truncated"
		>
			<Alert.Description
				>{m['assistant.merge_head_truncated']({
					count: Math.max((change.total_commits ?? 0) - change.commits.length, 1)
				})}</Alert.Description
			>
		</Alert.Root>
	{/if}
</div>
