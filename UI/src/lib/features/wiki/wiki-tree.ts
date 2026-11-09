// The wiki's page tree: nesting, ordering and where a drag-and-drop or a
// keyboard move puts a page. The server validates every move again (same
// scope, no cycles, depth limit); this only shapes the UI. Free of Svelte and
// $lib imports for the Node unit tests.

export const WIKI_PAGE_DRAG_TYPE = 'application/x-kuayle-wiki-page';
export const MAX_WIKI_DEPTH = 6;

export interface TreeItem {
	id: string;
	parent_id: string | null;
	position: number;
	kind: 'page' | 'folder';
}

export interface TreeNode<T extends TreeItem = TreeItem> {
	item: T;
	depth: number;
	children: TreeNode<T>[];
}

/** Nested nodes ordered by position; items whose parent is missing go to the top level. */
export function buildTree<T extends TreeItem>(items: T[]): TreeNode<T>[] {
	const byId = new Map(items.map((item) => [item.id, item]));
	const children = new Map<string | null, T[]>();
	for (const item of items) {
		const parent = item.parent_id && byId.has(item.parent_id) && item.parent_id !== item.id ? item.parent_id : null;
		children.set(parent, [...(children.get(parent) ?? []), item]);
	}
	const seen = new Set<string>();
	const build = (parent: string | null, depth: number): TreeNode<T>[] =>
		(children.get(parent) ?? [])
			.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id))
			.filter((item) => !seen.has(item.id) && seen.add(item.id))
			.map((item) => ({ item, depth, children: build(item.id, depth + 1) }));
	return build(null, 0);
}

/** Ids of `id` and everything nested under it. */
export function subtreeIds(items: TreeItem[], id: string): Set<string> {
	const ids = new Set([id]);
	let grew = true;
	while (grew) {
		grew = false;
		for (const item of items)
			if (item.parent_id && ids.has(item.parent_id) && !ids.has(item.id)) {
				ids.add(item.id);
				grew = true;
			}
	}
	return ids;
}

function depthOf(items: TreeItem[], id: string | null): number {
	let depth = 0;
	const byId = new Map(items.map((item) => [item.id, item]));
	for (
		let current = id ? byId.get(id) : undefined;
		current && depth <= items.length;
		current = current.parent_id ? byId.get(current.parent_id) : undefined
	)
		depth++;
	return depth;
}

function height(items: TreeItem[], id: string): number {
	const below = items.filter((item) => item.parent_id === id);
	return 1 + Math.max(0, ...below.map((child) => height(items, child.id)));
}

export type DropZone = 'before' | 'inside' | 'after';

/** Where on a row a pointer is: top and bottom quarters reorder, the middle nests. */
export function dropZone(offsetY: number, rowHeight: number): DropZone {
	if (offsetY < rowHeight / 4) return 'before';
	if (offsetY > (rowHeight * 3) / 4) return 'after';
	return 'inside';
}

export interface Move {
	id: string;
	parent_id: string | null;
	/** Position among the new siblings, counted without the moved page. */
	index: number;
}

/**
 * The move a drop of `id` on `target` asks for, or null when it is not
 * allowed (into itself or its own subtree, too deep) or changes nothing.
 */
export function moveFor(items: TreeItem[], id: string, target: string | null, zone: DropZone): Move | null {
	const moving = items.find((item) => item.id === id);
	if (!moving) return null;
	if (target && subtreeIds(items, id).has(target)) return null;
	const targetItem = target ? items.find((item) => item.id === target) : undefined;
	if (target && !targetItem) return null;
	const parent = !targetItem ? null : zone === 'inside' ? targetItem.id : targetItem.parent_id;
	if (depthOf(items, parent) + height(items, id) > MAX_WIKI_DEPTH) return null;
	const siblings = items
		.filter((item) => item.parent_id === parent && item.id !== id)
		.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id));
	let index = siblings.length;
	if (targetItem && zone !== 'inside') {
		const at = siblings.findIndex((item) => item.id === targetItem.id);
		index = zone === 'before' ? at : at + 1;
	}
	const currentIndex = items
		.filter((item) => item.parent_id === moving.parent_id)
		.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id))
		.findIndex((item) => item.id === id);
	if (moving.parent_id === parent && currentIndex === index) return null;
	return { id, parent_id: parent, index };
}

/** Applies a move locally (optimistically), renumbering both sibling lists. */
export function applyMove<T extends TreeItem>(items: T[], move: Move): T[] {
	const moving = items.find((item) => item.id === move.id);
	if (!moving) return items;
	const next = items.map((item) => ({ ...item }));
	const moved = next.find((item) => item.id === move.id)!;
	const renumber = (parent: string | null) =>
		next
			.filter((item) => item.parent_id === parent && item.id !== move.id)
			.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id));
	const oldSiblings = renumber(moving.parent_id);
	oldSiblings.forEach((item, index) => (item.position = index));
	const newSiblings = moving.parent_id === move.parent_id ? oldSiblings : renumber(move.parent_id);
	newSiblings.splice(Math.max(0, Math.min(move.index, newSiblings.length)), 0, moved);
	moved.parent_id = move.parent_id;
	newSiblings.forEach((item, index) => (item.position = index));
	return next;
}
