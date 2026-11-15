import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	buildIssueTree,
	descendantIds,
	hiddenTreeIds,
	issueRowTrees,
	rangeSelection,
	selectedRowsHiddenByCollapse,
	visibleTreeIssues
} from '../../src/lib/features/issues/issue-tree.ts';

const issue = (id, parent_id = null) => ({ id, parent_id });
// Compact picture of a tree: "a(b,c(d))".
const shape = (nodes) =>
	nodes.map((n) => (n.children.length ? `${n.issue.id}(${shape(n.children)})` : n.issue.id)).join(',');
const ids = (issues) => issues.map((i) => i.id);

test('sub-issues nest under their parent when the parent is in the list', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('c'), issue('d', 'a')]);
	assert.equal(shape(tree), 'a(b,d),c');
	assert.equal(tree[0].issue.id, 'a');
});

test('a sub-issue whose parent is not in the list stays at the top level', () => {
	// Parent filtered out, on a later page, or in another group.
	const tree = buildIssueTree([issue('b', 'missing'), issue('c'), issue('d', 'elsewhere')]);
	assert.equal(shape(tree), 'b,c,d');
});

test('deeper levels nest too', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('c', 'b'), issue('d', 'c')]);
	assert.equal(shape(tree), 'a(b(c(d)))');
});

test('list order is kept for top-level issues and for each parent’s sub-issues', () => {
	// A sub-issue listed before its parent still nests, in list order among its siblings.
	const tree = buildIssueTree([issue('c', 'p'), issue('x'), issue('p'), issue('a', 'p'), issue('y')]);
	assert.equal(shape(tree), 'x,p(c,a),y');
});

test('every issue appears exactly once, even with a parent cycle in the data', () => {
	const list = [issue('a', 'b'), issue('b', 'a'), issue('self', 'self'), issue('c')];
	const tree = buildIssueTree(list);
	const all = visibleTreeIssues(tree, new Set(['a', 'b', 'self', 'c']));
	assert.deepEqual(ids(all).sort(), ['a', 'b', 'c', 'self']);
	assert.equal(shape(tree), 'a(b),self,c');
});

test('the same id listed twice is shown once', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('a')]);
	assert.equal(shape(tree), 'a(b)');
});

test('visible order lists expanded sub-issues right after their parent', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('c', 'b'), issue('d'), issue('e', 'd')]);
	assert.deepEqual(ids(visibleTreeIssues(tree, new Set(['a', 'b', 'd']))), ['a', 'b', 'c', 'd', 'e']);
});

test('collapsed parents hide their whole subtree from the visible order', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('c', 'b'), issue('d'), issue('e', 'd')]);
	assert.deepEqual(ids(visibleTreeIssues(tree, new Set())), ['a', 'd']);
	// An expanded sub-issue stays hidden while an ancestor is collapsed.
	assert.deepEqual(ids(visibleTreeIssues(tree, new Set(['b']))), ['a', 'd']);
	assert.deepEqual(ids(visibleTreeIssues(tree, new Set(['a']))), ['a', 'b', 'd']);
});

test('an empty list builds an empty tree', () => {
	assert.deepEqual(buildIssueTree([]), []);
	assert.deepEqual(visibleTreeIssues([], new Set(['a'])), []);
});

test('issueRowTrees describes every node and reads expansion live', () => {
	const tree = buildIssueTree([issue('a'), issue('b', 'a'), issue('c', 'b'), issue('d')]);
	const open = new Set();
	const calls = [];
	let reads = 0;
	const expansion = {
		isExpanded: (id) => {
			reads += 1;
			return open.has(id);
		},
		setExpanded: (node, value) => calls.push([node.issue.id, value])
	};

	const rows = issueRowTrees(tree, expansion);
	assert.equal(reads, 0, 'building rows must not ask which parents are expanded');

	const about = (id) => {
		const row = rows.get(id);
		return { depth: row.depth, childCount: row.childCount, parentId: row.parentId };
	};
	assert.deepEqual(about('a'), { depth: 0, childCount: 1, parentId: null });
	assert.deepEqual(about('b'), { depth: 1, childCount: 1, parentId: 'a' });
	assert.deepEqual(about('c'), { depth: 2, childCount: 0, parentId: 'b' });
	assert.deepEqual(about('d'), { depth: 0, childCount: 0, parentId: null });

	// The same object expands and collapses without being rebuilt.
	const rowA = rows.get('a');
	assert.equal(rowA.expanded, false);
	open.add('a');
	assert.equal(rows.get('a'), rowA);
	assert.equal(rowA.expanded, true);

	rowA.onexpand(false);
	assert.deepEqual(calls, [['a', false]]);
});

test('descendantIds lists the whole subtree but not the node itself', () => {
	const tree = buildIssueTree([
		issue('a'),
		issue('b', 'a'),
		issue('c', 'b'),
		issue('e', 'a'),
		issue('x')
	]);
	assert.equal(shape(tree), 'a(b(c),e),x');
	assert.deepEqual(descendantIds(tree[0]).sort(), ['b', 'c', 'e']);
	assert.deepEqual(descendantIds(tree[1]), []);
});

test('hiddenTreeIds is exactly the rows visibleTreeIssues leaves out', () => {
	const tree = buildIssueTree([
		issue('a'),
		issue('b', 'a'),
		issue('c', 'b'),
		issue('d'),
		issue('e', 'd')
	]);
	const all = ['a', 'b', 'c', 'd', 'e'];
	for (const expanded of [new Set(), new Set(['a']), new Set(['b']), new Set(['a', 'b', 'd'])]) {
		const shown = new Set(ids(visibleTreeIssues(tree, expanded)));
		assert.deepEqual(
			[...hiddenTreeIds(tree, expanded)].sort(),
			all.filter((id) => !shown.has(id))
		);
	}
	assert.deepEqual([...hiddenTreeIds(tree, new Set(['a']))].sort(), ['c', 'e']);
});

test('selectedRowsHiddenByCollapse counts the selected sub-issues a collapse takes off screen', () => {
	const tree = buildIssueTree([
		issue('a'),
		issue('b', 'a'),
		issue('c', 'b'),
		issue('e', 'a'),
		issue('f', 'e'),
		issue('x')
	]);
	assert.equal(shape(tree), 'a(b(c),e(f)),x');
	const [a, x] = tree;
	const e = a.children[1];

	// Every depth counts while it is on screen; the parent itself and other rows do not.
	const allOpen = new Set(['a', 'b', 'e']);
	assert.equal(selectedRowsHiddenByCollapse(a, allOpen, new Set(['a', 'b', 'c', 'x'])), 2);
	assert.equal(selectedRowsHiddenByCollapse(a, allOpen, new Set(['a', 'b', 'c', 'e', 'f'])), 4);

	// Nothing selected below the parent: nothing to report.
	assert.equal(selectedRowsHiddenByCollapse(a, allOpen, new Set(['a', 'x'])), 0);
	assert.equal(selectedRowsHiddenByCollapse(a, allOpen, new Set()), 0);
	assert.equal(selectedRowsHiddenByCollapse(x, allOpen, new Set(['x'])), 0);

	// A sub-issue already hidden under a collapsed child was not on screen, so it is not counted.
	assert.equal(selectedRowsHiddenByCollapse(a, new Set(['a', 'e']), new Set(['b', 'c', 'f'])), 2);
	assert.equal(selectedRowsHiddenByCollapse(e, new Set(['a']), new Set(['f'])), 0);

	// Collapsing a parent that is already collapsed hides nothing.
	assert.equal(selectedRowsHiddenByCollapse(a, new Set(), new Set(['b', 'c'])), 0);
});

test('rangeSelection spans the visible rows from the anchor to the clicked row', () => {
	const order = [issue('p'), issue('q'), issue('r'), issue('s')];
	const parents = new Map([
		['h1', 'q'],
		['h2', 'h1'],
		['loop1', 'loop2'],
		['loop2', 'loop1']
	]);
	const parentOf = (id) => parents.get(id) ?? null;

	// Either direction selects the same rows, in on-screen order.
	assert.deepEqual(rangeSelection(order, 'q', 's', parentOf), ['q', 'r', 's']);
	assert.deepEqual(rangeSelection(order, 's', 'q', parentOf), ['q', 'r', 's']);
	// A hidden anchor starts at its nearest visible ancestor (h2 -> h1 -> q).
	assert.deepEqual(rangeSelection(order, 'h2', 's', parentOf), ['q', 'r', 's']);
	// No visible ancestor, or a parent cycle that never reaches one.
	assert.deepEqual(rangeSelection(order, 'gone', 'r', parentOf), ['r']);
	assert.deepEqual(rangeSelection(order, 'loop1', 'r', parentOf), ['r']);
	// A clicked row that is not on screen selects only itself, as does a range of one row.
	assert.deepEqual(rangeSelection(order, 'p', 'nowhere', parentOf), ['nowhere']);
	assert.deepEqual(rangeSelection(order, 'r', 'r', parentOf), ['r']);
});

test('performance guard: nesting and flattening 2,000 issues stays fast', () => {
	const list = [];
	for (let p = 0; p < 500; p += 1) {
		list.push(issue(`f${p}`));
		for (let c = 1; c <= 3; c += 1) list.push(issue(`f${p}c${c}`, `f${p}`));
	}

	const started = performance.now();
	const tree = buildIssueTree(list);
	issueRowTrees(tree, { isExpanded: () => false, setExpanded() {} });
	const hidden = hiddenTreeIds(tree, new Set());
	const elapsed = performance.now() - started;

	assert.equal(tree.length, 500);
	assert.equal(hidden.size, 1500);
	assert.ok(elapsed < 1000, `nesting 2,000 issues took ${elapsed} ms`);
});
