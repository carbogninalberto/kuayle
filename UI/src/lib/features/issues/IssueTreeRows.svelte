<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import type { Issue } from '$lib/types/issue';
	import { buildIssueTree, type IssueRowTree, type IssueTreeNode } from './issue-tree';
	import { subIssueTreeState } from './sub-issue-tree.state.svelte';

	let {
		issues,
		nested = false,
		row
	}: {
		issues: Issue[];
		/** Nest sub-issues under their parent when it is in `issues`. */
		nested?: boolean;
		/** Renders one row; `tree` is undefined in a flat list. */
		row: Snippet<[Issue, IssueRowTree | undefined]>;
	} = $props();

	const nodes = $derived(nested ? buildIssueTree(issues) : []);
</script>

{#snippet branch(list: IssueTreeNode<Issue>[], depth: number, parentId: string | null)}
	{#each list as node (node.issue.id)}
		{@const id = node.issue.id}
		{@const expanded = subIssueTreeState.expanded.has(id)}
		{@const tree = {
			depth,
			childCount: node.children.length,
			expanded,
			parentId,
			onexpand: (open: boolean) => subIssueTreeState.setExpanded(id, open)
		}}
		{#if node.children.length > 0}
			<!-- The row's chevron is this Collapsible's trigger (see IssueRow). -->
			<Collapsible.Root open={expanded} onOpenChange={(open) => subIssueTreeState.setExpanded(id, open)}>
				{@render row(node.issue, tree)}
				<Collapsible.Content>
					{@render branch(node.children, depth + 1, id)}
				</Collapsible.Content>
			</Collapsible.Root>
		{:else}
			{@render row(node.issue, tree)}
		{/if}
	{/each}
{/snippet}

{#if nested}
	{@render branch(nodes, 0, null)}
{:else}
	{#each issues as issue (issue.id)}{@render row(issue, undefined)}{/each}
{/if}
