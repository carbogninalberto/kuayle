<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import type { Issue } from '$lib/types/issue';
	import { buildIssueTree, descendantIds, issueRowTrees, type IssueRowTree, type IssueTreeNode } from './issue-tree';
	import { issuesState } from './issues.state.svelte';
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

	// Built once per data change. Each row object reads its expanded state live, so expanding or
	// collapsing a parent only updates that row and mounts or unmounts its sub-issues.
	const nodes = $derived(nested ? buildIssueTree(issues) : []);
	const rows = $derived(
		issueRowTrees(nodes, {
			isExpanded: (id) => subIssueTreeState.expanded.has(id),
			setExpanded: (node, open) => {
				subIssueTreeState.setExpanded(node.issue.id, open);
				// Bulk actions never include rows the user cannot see.
				if (!open) issuesState.deselect(descendantIds(node));
			}
		})
	);
</script>

{#snippet branch(list: IssueTreeNode<Issue>[])}
	{#each list as node (node.issue.id)}
		{@const tree = rows.get(node.issue.id)!}
		{#if node.children.length > 0}
			<!-- The row's chevron is this Collapsible's trigger (see IssueRow). Sub-issues mount only while expanded. -->
			<Collapsible.Root bind:open={() => tree.expanded, (open) => tree.onexpand(open)}>
				{@render row(node.issue, tree)}
				<Collapsible.Content>
					{#if tree.expanded}{@render branch(node.children)}{/if}
				</Collapsible.Content>
			</Collapsible.Root>
		{:else}
			{@render row(node.issue, tree)}
		{/if}
	{/each}
{/snippet}

{#if nested}
	<!-- The container IssueRow's Left-arrow key searches for the parent row. -->
	<div data-issue-tree>{@render branch(nodes)}</div>
{:else}
	{#each issues as issue (issue.id)}{@render row(issue, undefined)}{/each}
{/if}
