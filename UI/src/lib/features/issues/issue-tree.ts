// Nests sub-issues under their parent inside one list of issues, and flattens the rows
// that are visible. Pure: the input issues are never mutated and every node is new.

export interface TreeIssue {
	id: string;
	parent_id?: string | null;
}

export interface IssueTreeNode<I extends TreeIssue = TreeIssue> {
	issue: I;
	children: IssueTreeNode<I>[];
}

/** What IssueRow needs to draw one row of a nested list. */
export interface IssueRowTree {
	/** Nesting level, 0 for a top-level row. */
	depth: number;
	/** Sub-issues nested under this row in the current list. */
	childCount: number;
	expanded: boolean;
	/** Id of the row this one is nested under, null at the top level. */
	parentId: string | null;
	/** Expands (true) or collapses (false) this row's sub-issues. */
	onexpand: (open: boolean) => void;
}

export function buildIssueTree<I extends TreeIssue>(issues: readonly I[]): IssueTreeNode<I>[] {
	// Keep the first occurrence of every id, in list order.
	const unique: I[] = [];
	const byId = new Map<string, I>();
	for (const issue of issues) {
		if (byId.has(issue.id)) continue;
		byId.set(issue.id, issue);
		unique.push(issue);
	}

	// A parent only nests when it is in this list and is not the issue itself.
	const childrenOf = new Map<string, I[]>();
	const roots = new Set<string>();
	for (const issue of unique) {
		const parentId = issue.parent_id;
		if (parentId && parentId !== issue.id && byId.has(parentId)) {
			const siblings = childrenOf.get(parentId);
			if (siblings) siblings.push(issue);
			else childrenOf.set(parentId, [issue]);
			continue;
		}
		roots.add(issue.id);
	}

	// Walk from the roots, then break any remaining parent cycle with an extra root.
	const visited = new Set<string>();
	const walk = (start: I) => {
		const stack: I[] = [start];
		while (stack.length > 0) {
			const current = stack.pop();
			if (!current || visited.has(current.id)) continue;
			visited.add(current.id);
			for (const child of childrenOf.get(current.id) ?? []) stack.push(child);
		}
	};
	for (const issue of unique) if (roots.has(issue.id)) walk(issue);
	for (const issue of unique) {
		if (visited.has(issue.id)) continue;
		roots.add(issue.id);
		walk(issue);
	}

	// Build nodes in list order, placing each issue once.
	const placed = new Set<string>();
	const build = (issue: I): IssueTreeNode<I> => {
		placed.add(issue.id);
		const children: IssueTreeNode<I>[] = [];
		for (const child of childrenOf.get(issue.id) ?? []) {
			if (roots.has(child.id) || placed.has(child.id)) continue;
			children.push(build(child));
		}
		return { issue, children };
	};

	const tree: IssueTreeNode<I>[] = [];
	for (const issue of unique) if (roots.has(issue.id)) tree.push(build(issue));
	return tree;
}

export function visibleTreeIssues<I extends TreeIssue>(
	nodes: readonly IssueTreeNode<I>[],
	expanded: ReadonlySet<string>
): I[] {
	const visible: I[] = [];
	const visit = (node: IssueTreeNode<I>) => {
		visible.push(node.issue);
		if (!expanded.has(node.issue.id)) return;
		for (const child of node.children) visit(child);
	};
	for (const node of nodes) visit(node);
	return visible;
}
