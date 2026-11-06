<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';
	import type { PullRequestHeadChange } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';

	// Commits others pushed after Kuayle published the pull request, on a merge
	// card (merge_pull_request or ship_pull_request): authors and whether the
	// files changed.
	let { change, publishedCommit }: { change: PullRequestHeadChange; publishedCommit?: string } = $props();
	const published = $derived((publishedCommit ?? '').slice(0, 7));
</script>

<div class="space-y-1 text-xs text-muted-foreground" data-testid="merge-head-change">
	<p>
		{change.kind === 'merge'
			? m['assistant.merge_head_merge']({ published })
			: change.kind === 'rewritten'
				? m['assistant.merge_head_rewritten']({ published })
				: m['assistant.merge_head_pushed']({ published })}
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
