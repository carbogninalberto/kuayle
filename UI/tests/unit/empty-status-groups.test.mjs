import { test } from 'node:test';
import assert from 'node:assert/strict';
import { showEmptyStatusGroups, withEmptyStatusGroups } from '../../src/lib/features/issues/empty-status-groups.ts';

const status = (id, team_id, name, category, position) => ({ id, team_id, name, category, position });
const statuses = [
	status('a-backlog', 'A', 'Backlog', 'backlog', 0),
	status('a-todo', 'A', 'Todo', 'unstarted', 1),
	status('a-doing', 'A', 'In Progress', 'started', 2),
	status('a-done', 'A', 'Done', 'completed', 3),
	status('b-todo', 'B', 'todo', 'unstarted', 0),
	status('b-review', 'B', 'Review', 'started', 1)
];
const group = (key, label) => ({ key, label, issues: [{ id: `i-${key}` }] });
const keys = (groups) => groups.map((g) => g.key);

test('every status of the team becomes a group, in position order', () => {
	const result = withEmptyStatusGroups([group('a-todo', 'Todo'), group('a-done', 'Done')], statuses, { teamIds: ['A'] });
	assert.deepEqual(keys(result), ['a-backlog', 'a-todo', 'a-doing', 'a-done']);
	assert.deepEqual(result[0].issues, []);
	assert.equal(result[2].label, 'In Progress');
});

test('groups with issues are kept as they are', () => {
	const todo = group('a-todo', 'Todo');
	const result = withEmptyStatusGroups([todo], statuses, { teamIds: ['A'] });
	assert.equal(result.find((g) => g.key === 'a-todo'), todo);
});

test('the workflow category order decides before position', () => {
	const result = withEmptyStatusGroups([group('a-todo', 'Todo')], statuses, {
		teamIds: ['A'],
		categoryOrder: ['started', 'unstarted', 'backlog', 'completed', 'cancelled']
	});
	assert.deepEqual(keys(result), ['a-doing', 'a-todo', 'a-backlog', 'a-done']);
});

test('only the given teams contribute statuses', () => {
	const result = withEmptyStatusGroups([group('a-todo', 'Todo')], statuses, { teamIds: ['A'] });
	assert.ok(!keys(result).includes('b-review'));
	assert.deepEqual(withEmptyStatusGroups([], statuses, { teamIds: [] }), []);
});

test('with several teams a status named like an existing group is not added again', () => {
	const result = withEmptyStatusGroups([group('a-todo', 'Todo')], statuses, { teamIds: ['A', 'B'] });
	assert.ok(!keys(result).includes('b-todo'));
	assert.ok(keys(result).includes('b-review'));
});

test('with several teams one empty group per status name', () => {
	const extra = [...statuses, status('c-review', 'C', 'review', 'started', 1)];
	const result = withEmptyStatusGroups([], extra, { teamIds: ['B', 'C'] });
	assert.deepEqual(keys(result).filter((k) => k.endsWith('review')), ['b-review']);
});

test('empty groups show only for status grouping, without a status filter, on the last page', () => {
	assert.equal(showEmptyStatusGroups('status', {}, false), true);
	assert.equal(showEmptyStatusGroups('priority', {}, false), false);
	assert.equal(showEmptyStatusGroups(null, {}, false), false);
	assert.equal(showEmptyStatusGroups('status', { status: 'a-todo' }, false), false);
	assert.equal(showEmptyStatusGroups('status', { status_type: 'started' }, false), false);
	assert.equal(showEmptyStatusGroups('status', { priority: '1' }, false), true);
	assert.equal(showEmptyStatusGroups('status', {}, true), false);
});
