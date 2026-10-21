import assert from 'node:assert/strict';
import test from 'node:test';
import {
	belongsToScope,
	scopedIssueParams,
	updateIssueMembership
} from '../../src/lib/features/issues/scoped-issues.ts';

test('scope constraints win over filters and grouped pagination stays stable', () => {
	assert.deepEqual(
		scopedIssueParams(
			{ kind: 'cycle', id: 'cycle-a', teamId: 'team-a' },
			{ cycle: 'other', team: 'other', search: 'needle', priority: '2', empty: '' },
			'status'
		),
		{
			sort: 'sort_order',
			order: 'asc',
			cycle: 'cycle-a',
			team: 'team-a',
			search: 'needle',
			priority: '2',
			group_by: 'status'
		}
	);
	assert.equal(scopedIssueParams({ kind: 'project', id: 'p' }, { group_by: 'status' }).group_by, undefined);
});

test('picker excludes current membership even when the issue is not in the loaded list', () => {
	assert.equal(belongsToScope({ cycle_id: 'c' }, { kind: 'cycle', id: 'c' }), true);
	assert.equal(belongsToScope({ cycle_id: 'other' }, { kind: 'cycle', id: 'c' }), false);
	assert.equal(belongsToScope({ project_id: 'p' }, { kind: 'project', id: 'p' }), true);
});

test('assignment reports partial failure and retries only failed issues', async () => {
	const issues = [1, 2, 3].map((id) => ({ id: String(id), identifier: `ENG-${id}` }));
	const calls = [];
	const result = await updateIssueMembership(
		[...issues, issues[0]],
		{ kind: 'project', id: 'p' },
		false,
		async (identifier, patch) => {
			calls.push([identifier, patch]);
			if (identifier === 'ENG-2') throw new Error('denied');
		}
	);
	assert.equal(calls.length, 3);
	assert.deepEqual(calls[0][1], { project_id: 'p' });
	assert.deepEqual(result.failed, [issues[1]]);
	assert.equal(result.succeeded.length, 2);
	const retry = [];
	await updateIssueMembership(result.failed, { kind: 'project', id: 'p' }, false, async (id) => retry.push(id));
	assert.deepEqual(retry, ['ENG-2']);
});

test('removal clears only the membership field, never deletes issues; requests are bounded', async () => {
	let active = 0,
		maxActive = 0;
	const issues = Array.from({ length: 12 }, (_, i) => ({ id: String(i), identifier: `ENG-${i}` }));
	const result = await updateIssueMembership(issues, { kind: 'cycle', id: 'c' }, true, async (_id, patch) => {
		assert.deepEqual(patch, { cycle_id: '' });
		maxActive = Math.max(maxActive, ++active);
		await new Promise((resolve) => setTimeout(resolve, 1));
		active--;
	});
	assert.equal(maxActive, 4);
	assert.equal(result.succeeded.length, 12);
	assert.deepEqual(result.failed, []);
});

test('project removal uses the same explicit clear contract', async () => {
	const calls = [];
	await updateIssueMembership(
		[{ id: '1', identifier: 'ENG-1' }],
		{ kind: 'project', id: 'p' },
		true,
		async (_id, patch) => calls.push(patch)
	);
	assert.deepEqual(calls, [{ project_id: '' }]);
});
