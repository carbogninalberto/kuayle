import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildIssueTree, visibleTreeIssues } from '../../src/lib/features/issues/issue-tree.ts';

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
