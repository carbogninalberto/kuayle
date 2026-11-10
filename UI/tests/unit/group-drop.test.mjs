import { test } from 'node:test';
import assert from 'node:assert/strict';
import { issueGroupKey, resolveGroupDrop, statusForIssueTeam } from '../../src/lib/features/issues/group-drop.ts';

const status = (id, team_id, name, category, position = 0) => ({ id, team_id, name, category, position });
const statuses = [
	status('a-todo', 'A', 'Todo', 'unstarted'),
	status('a-doing', 'A', 'In Progress', 'started', 1),
	status('a-done', 'A', 'Done', 'completed', 2),
	status('b-todo', 'B', 'todo', 'unstarted'),
	status('b-review', 'B', 'Review', 'started', 1),
	status('b-late', 'B', 'Later', 'started', 2),
	status('b-shipped', 'B', 'Shipped', 'completed', 3)
];
const issue = (over = {}) => ({
	team_id: 'A',
	status_id: 'a-todo',
	priority: 2,
	assignee_id: null,
	project_id: null,
	...over
});

test('same-team status drop sets status_id', () => {
	assert.deepEqual(resolveGroupDrop(issue(), 'status', 'a-doing', statuses), {
		kind: 'update',
		patch: { status_id: 'a-doing' }
	});
});

test('dropping on the current status does nothing', () => {
	assert.deepEqual(resolveGroupDrop(issue(), 'status', 'a-todo', statuses), { kind: 'noop' });
});

test('another team status maps by name, ignoring case', () => {
	assert.equal(statusForIssueTeam(issue({ team_id: 'B', status_id: 'b-review' }), 'a-todo', statuses), 'b-todo');
	assert.equal(statusForIssueTeam(issue(), 'b-todo', statuses), 'a-todo');
});

test('another team status falls back to the first of the same category', () => {
	assert.equal(statusForIssueTeam(issue({ team_id: 'B' }), 'a-doing', statuses), 'b-review');
	assert.equal(statusForIssueTeam(issue({ team_id: 'B' }), 'a-done', statuses), 'b-shipped');
});

test('a cross-team drop that maps to the current status is a no-op', () => {
	const own = issue({ team_id: 'B', status_id: 'b-todo' });
	assert.deepEqual(resolveGroupDrop(own, 'status', 'a-todo', statuses), { kind: 'noop' });
});

test('no matching status is invalid', () => {
	const cancelled = [...statuses, status('a-cancel', 'A', 'Cancelled', 'cancelled', 3)];
	assert.deepEqual(resolveGroupDrop(issue({ team_id: 'B' }), 'status', 'a-cancel', cancelled), { kind: 'invalid' });
	assert.deepEqual(resolveGroupDrop(issue(), 'status', 'unknown', statuses), { kind: 'invalid' });
});

test('priority, assignee and project drops', () => {
	assert.deepEqual(resolveGroupDrop(issue(), 'priority', '1', statuses), { kind: 'update', patch: { priority: 1 } });
	assert.deepEqual(resolveGroupDrop(issue(), 'priority', '2', statuses), { kind: 'noop' });
	assert.deepEqual(resolveGroupDrop(issue(), 'assignee', 'u1', statuses), {
		kind: 'update',
		patch: { assignee_id: 'u1' }
	});
	assert.deepEqual(resolveGroupDrop(issue({ assignee_id: 'u1' }), 'assignee', 'unassigned', statuses), {
		kind: 'update',
		patch: { assignee_id: null }
	});
	assert.deepEqual(resolveGroupDrop(issue({ project_id: 'p1' }), 'project', 'no-project', statuses), {
		kind: 'update',
		patch: { project_id: null }
	});
	assert.deepEqual(resolveGroupDrop(issue(), 'project', 'no-project', statuses), { kind: 'noop' });
});

test('no grouping does nothing and group keys follow the list grouping', () => {
	assert.deepEqual(resolveGroupDrop(issue(), null, 'x', statuses), { kind: 'noop' });
	assert.equal(issueGroupKey(issue(), 'status'), 'a-todo');
	assert.equal(issueGroupKey(issue({ status_id: null, status: 'todo' }), 'status'), 'todo');
	assert.equal(issueGroupKey(issue(), 'assignee'), 'unassigned');
});
