import { loadExpandedParents, loadNestSubIssues, saveExpandedParents, saveNestSubIssues } from './sub-issue-tree-storage';

// "Nest sub-issues" display option of the issue lists and the parents whose sub-issues are shown.
// Kept in this browser, like the collapsed groups.
class SubIssueTreeState {
	nest = $state(loadNestSubIssues());
	expanded = $state<Set<string>>(new Set());
	private workspaceSlug = '';

	/** Loads the expanded parents of a workspace. Lists call it from an effect. */
	useWorkspace(workspaceSlug: string) {
		if (workspaceSlug === this.workspaceSlug) return;
		this.workspaceSlug = workspaceSlug;
		this.expanded = loadExpandedParents(workspaceSlug);
	}

	setNest(nest: boolean) {
		this.nest = nest;
		saveNestSubIssues(nest);
	}

	setExpanded(issueId: string, open: boolean) {
		if (open === this.expanded.has(issueId)) return;
		const next = new Set(this.expanded);
		if (open) next.add(issueId);
		else next.delete(issueId);
		this.expanded = next;
		if (this.workspaceSlug) saveExpandedParents(this.workspaceSlug, next);
	}
}

export const subIssueTreeState = new SubIssueTreeState();
