import assert from 'node:assert/strict';
import test from 'node:test';
import { applyMove, buildTree, dropZone, moveFor, subtreeIds } from '../../src/lib/features/wiki/wiki-tree.ts';

const item = (id, parent_id = null, position = 0, kind = 'page') => ({ id, parent_id, position, kind });
// docs/ (folder): intro, guides/ (folder): setup ; then faq at the top level.
const items = [
	item('docs', null, 0, 'folder'),
	item('intro', 'docs', 0),
	item('guides', 'docs', 1, 'folder'),
	item('setup', 'guides', 0),
	item('faq', null, 1)
];

test('the tree nests by parent and orders by position', () => {
	const tree = buildTree(items);
	assert.deepEqual(
		tree.map((n) => [n.item.id, n.depth, n.children.map((c) => c.item.id)]),
		[
			['docs', 0, ['intro', 'guides']],
			['faq', 0, []]
		]
	);
	assert.equal(tree[0].children[1].children[0].depth, 2);
	// A page whose parent is gone, or that names itself, is shown at the top level.
	assert.deepEqual(
		buildTree([item('a', 'missing'), item('b', 'b', 1)]).map((n) => n.item.id),
		['a', 'b']
	);
});

test('drops above or below a row reorder; the middle nests', () => {
	assert.equal(dropZone(2, 32), 'before');
	assert.equal(dropZone(16, 32), 'inside');
	assert.equal(dropZone(30, 32), 'after');
	assert.deepEqual(moveFor(items, 'faq', 'intro', 'before'), { id: 'faq', parent_id: 'docs', index: 0 });
	assert.deepEqual(moveFor(items, 'faq', 'guides', 'inside'), { id: 'faq', parent_id: 'guides', index: 1 });
	assert.deepEqual(moveFor(items, 'intro', 'faq', 'after'), { id: 'intro', parent_id: null, index: 2 });
	assert.deepEqual(
		moveFor(items, 'setup', null, 'inside'),
		{ id: 'setup', parent_id: null, index: 2 },
		'the empty list area is the top level'
	);
});

test('a page cannot move into itself or its own subtree, and no-op drops are ignored', () => {
	assert.deepEqual([...subtreeIds(items, 'docs')].sort(), ['docs', 'guides', 'intro', 'setup']);
	assert.equal(moveFor(items, 'docs', 'setup', 'inside'), null);
	assert.equal(moveFor(items, 'docs', 'docs', 'after'), null);
	assert.equal(moveFor(items, 'intro', 'guides', 'before'), null, 'already there');
});

test('moves that would nest deeper than the limit are refused', () => {
	const chain = Array.from({ length: 6 }, (_, i) => item(`p${i}`, i ? `p${i - 1}` : null));
	assert.equal(moveFor([...chain, item('x')], 'x', 'p5', 'inside'), null);
	assert.deepEqual(moveFor([...chain, item('x')], 'x', 'p4', 'inside'), { id: 'x', parent_id: 'p4', index: 1 });
});

test('applying a move renumbers the old and new siblings', () => {
	const moved = applyMove(items, { id: 'faq', parent_id: 'docs', index: 1 });
	const order = (parent) =>
		moved
			.filter((i) => i.parent_id === parent)
			.sort((a, b) => a.position - b.position)
			.map((i) => i.id);
	assert.deepEqual(order('docs'), ['intro', 'faq', 'guides']);
	assert.deepEqual(order(null), ['docs']);
	assert.deepEqual(
		order('docs').map((id) => moved.find((i) => i.id === id).position),
		[0, 1, 2]
	);
});
