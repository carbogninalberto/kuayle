import { test } from 'node:test';
import assert from 'node:assert/strict';
import { approvedBy } from '../../src/lib/features/assistant/review-approval.ts';

const signal = (over) => ({
	kind: 'review',
	run_id: 'run-2',
	outcome: 'approved',
	action: 'recorded',
	name: 'reviewer',
	created_at: '2026-10-27T20:15:00Z',
	...over
});

test('the latest review on the current attempt decides approval', () => {
	assert.equal(approvedBy([signal({})], 'run-2'), 'reviewer');
	assert.equal(
		approvedBy([signal({}), signal({ outcome: 'changes_requested', created_at: '2026-10-27T20:20:00Z' })], 'run-2'),
		undefined
	);
	assert.equal(
		approvedBy([signal({ outcome: 'changes_requested', created_at: '2026-10-27T19:00:00Z' }), signal({})], 'run-2'),
		'reviewer'
	);
});

test('approvals of earlier attempts, CI and ignored reviews do not count', () => {
	assert.equal(approvedBy([signal({ run_id: 'run-1' })], 'run-2'), undefined);
	assert.equal(approvedBy([signal({ kind: 'ci', outcome: 'success' })], 'run-2'), undefined);
	assert.equal(approvedBy([signal({ action: 'untrusted' })], 'run-2'), undefined);
	assert.equal(
		approvedBy([signal({ outcome: 'commented', created_at: '2026-10-27T21:00:00Z' }), signal({})], 'run-2'),
		'reviewer'
	);
});
