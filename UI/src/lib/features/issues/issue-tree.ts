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
	/** Read live, so the object stays the same when the row is expanded or collapsed. */
	readonly expanded: boolean;
	/** Id of the row this one is nested under, null at the top level. */
	parentId: string | null;
	/** Expands (true) or collapses (false) this row's sub-issues. */
	onexpand: (open: boolean) => void;
}

/** How a nested list reads and changes which parents are expanded. */
export interface TreeExpansion<I extends TreeIssue = TreeIssue> {
	isExpanded(id: string): boolean;
	setExpanded(node: IssueTreeNode<I>, open: boolean): void;
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
	expanded: { has(id: string): boolean }
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

/**
 * One row object per node, built once per tree. `expanded` is a getter, so expanding or
 * collapsing a row never rebuilds these objects.
 */
export function issueRowTrees<I extends TreeIssue>(
	nodes: readonly IssueTreeNode<I>[],
	expansion: TreeExpansion<I>
): Map<string, IssueRowTree> {
	const rows = new Map<string, IssueRowTree>();
	const stack: Array<{ node: IssueTreeNode<I>; depth: number; parentId: string | null }> = [];
	for (const node of [...nodes].reverse()) stack.push({ node, depth: 0, parentId: null });

	while (stack.length > 0) {
		const current = stack.pop();
		if (!current) continue;
		const id = current.node.issue.id;
		rows.set(id, {
			depth: current.depth,
			childCount: current.node.children.length,
			parentId: current.parentId,
			get expanded() {
				return expansion.isExpanded(id);
			},
			onexpand: (open: boolean) => expansion.setExpanded(current.node, open)
		});
		for (const child of [...current.node.children].reverse()) {
			stack.push({ node: child, depth: current.depth + 1, parentId: id });
		}
	}

	return rows;
}

/** Ids of every issue nested anywhere under `node`, not counting `node` itself. */
export function descendantIds<I extends TreeIssue>(node: IssueTreeNode<I>): string[] {
	const found: string[] = [];
	const stack: IssueTreeNode<I>[] = [...node.children].reverse();
	while (stack.length > 0) {
		const current = stack.pop();
		if (!current) continue;
		found.push(current.issue.id);
		for (const child of [...current.children].reverse()) stack.push(child);
	}
	return found;
}

/** Ids nested under a collapsed parent: in the tree, but not among `visibleTreeIssues`. */
export function hiddenTreeIds<I extends TreeIssue>(
	nodes: readonly IssueTreeNode<I>[],
	expanded: { has(id: string): boolean }
): Set<string> {
	const hidden = new Set<string>();
	// Top-level nodes are never hidden. Below a closed row everything is hidden, so the
	// stack carries that down instead of asking about each node inside it.
	const stack: Array<{ node: IssueTreeNode<I>; hidden: boolean }> = [];
	for (const node of [...nodes].reverse()) stack.push({ node, hidden: false });

	while (stack.length > 0) {
		const current = stack.pop();
		if (!current) continue;
		if (current.hidden) hidden.add(current.node.issue.id);
		const open = !current.hidden && expanded.has(current.node.issue.id);
		for (const child of [...current.node.children].reverse()) {
			stack.push({ node: child, hidden: !open });
		}
	}

	return hidden;
}

/**
 * How many selected rows collapsing `node` takes off screen: its sub-issues shown below it that
 * are in `selected`. Sub-issues already hidden under a collapsed row are not counted, and neither
 * is anything when `node` is collapsed already. The collapse deselects these rows.
 */
export function selectedRowsHiddenByCollapse<I extends TreeIssue>(
	node: IssueTreeNode<I>,
	expanded: { has(id: string): boolean },
	selected: { has(id: string): boolean }
): number {
	if (!expanded.has(node.issue.id)) return 0;
	let count = 0;
	for (const issue of visibleTreeIssues(node.children, expanded)) if (selected.has(issue.id)) count += 1;
	return count;
}

/**
 * Ids a shift-click selects: the rows of `order` (top to bottom, as on screen) from the anchor
 * `fromId` to the clicked `toId`. An anchor that is not in `order`, because it is hidden under a
 * collapsed parent, starts the range at its nearest ancestor that is, found through `parentOf`.
 * When neither the anchor nor an ancestor is in `order`, or `toId` is not, only `toId` is selected.
 */
export function rangeSelection(
	order: readonly TreeIssue[],
	fromId: string,
	toId: string,
	parentOf: (id: string) => string | null | undefined
): string[] {
	const indexById = new Map<string, number>();
	for (let i = 0; i < order.length; i += 1) {
		const issue = order[i];
		if (issue) indexById.set(issue.id, i);
	}

	const to = indexById.get(toId);
	if (to === undefined) return [toId];

	// Walk up from the anchor until a row is on screen, guarding against parent cycles.
	let anchor: number | undefined;
	const seen = new Set<string>();
	let id: string | null | undefined = fromId;
	while (id && !seen.has(id)) {
		seen.add(id);
		const index = indexById.get(id);
		if (index !== undefined) {
			anchor = index;
			break;
		}
		id = parentOf(id);
	}
	if (anchor === undefined) return [toId];

	const selected: string[] = [];
	for (let i = Math.min(anchor, to); i <= Math.max(anchor, to); i += 1) {
		const issue = order[i];
		if (issue) selected.push(issue.id);
	}
	return selected;
}
