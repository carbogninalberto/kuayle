import { SvelteSet } from 'svelte/reactivity';
import { loadExpandedParents, loadNestSubIssues, saveExpandedParents, saveNestSubIssues } from './sub-issue-tree-storage';

// "Nest sub-issues" display option of the issue lists and the parents whose sub-issues are shown.
// Kept in this browser, like the collapsed groups. `expanded` is a SvelteSet, so expanding or
// collapsing one parent only updates what reads that parent.
class SubIssueTreeState {
	nest = $state(loadNestSubIssues());
	readonly expanded = new SvelteSet<string>();
	private workspaceSlug = '';

	/** Loads the expanded parents of a workspace. Lists call it from `$effect.pre`, so their first render has them. */
	useWorkspace(workspaceSlug: string) {
		if (workspaceSlug === this.workspaceSlug) return;
		this.workspaceSlug = workspaceSlug;
		this.expanded.clear();
		for (const id of loadExpandedParents(workspaceSlug)) this.expanded.add(id);
	}

	setNest(nest: boolean) {
		this.nest = nest;
		saveNestSubIssues(nest);
	}

	setExpanded(issueId: string, open: boolean) {
		if (open === this.expanded.has(issueId)) return;
		if (open) this.expanded.add(issueId);
		else this.expanded.delete(issueId);
		if (this.workspaceSlug) saveExpandedParents(this.workspaceSlug, this.expanded);
	}
}

export const subIssueTreeState = new SubIssueTreeState();
