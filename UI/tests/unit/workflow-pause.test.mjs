import { test } from 'node:test';
import assert from 'node:assert/strict';
import { canResume, workflowPause } from '../../src/lib/features/assistant/workflow-pause.ts';

const task = (over) => ({
	state: 'running',
	kind: 'implement',
	workflow_version_id: 'version-1',
	workflow_status: 'running',
	...over
});

test('only a running workflow can be paused', () => {
	assert.equal(workflowPause(task({})), 'pausable');
	assert.equal(workflowPause(task({ state: 'awaiting_review', workflow_status: 'waiting' })), 'pausable');
	assert.equal(workflowPause(task({ workflow_version_id: null })), 'none', 'Standard tasks have no workflow');
	assert.equal(workflowPause(task({ kind: 'review' })), 'none');
	assert.equal(workflowPause(task({ state: 'blocked', workflow_status: 'paused' })), 'none', 'stopped at a limit');
	assert.equal(workflowPause(task({ state: 'blocked' })), 'none', 'paused by a usage limit');
	for (const state of ['completed', 'failed', 'cancelled', 'cancelling'])
		assert.equal(workflowPause(task({ state })), 'none', state);
});

test('a paused workflow resumes; a pause taking effect can be withdrawn', () => {
	const paused = task({ state: 'blocked', workflow_paused_at: '2026-11-11T10:00:00Z' });
	assert.equal(workflowPause(paused), 'paused');
	assert.ok(canResume(paused));
	const waiting = task({ state: 'awaiting_review', workflow_status: 'waiting', workflow_paused_at: '2026-11-11T10:00:00Z' });
	assert.equal(workflowPause(waiting), 'paused');

	const requested = task({ workflow_pause_request: 'after_step' });
	assert.equal(workflowPause(requested), 'pausing');
	assert.ok(canResume(requested));

	const stopping = task({ state: 'cancelling', workflow_paused_at: '2026-11-11T10:00:00Z' });
	assert.equal(workflowPause(stopping), 'pausing');
	assert.ok(!canResume(stopping), 'the agent is still being stopped');

	assert.ok(!canResume(task({})));
	assert.equal(workflowPause(task({ state: 'cancelled', workflow_paused_at: '2026-11-11T10:00:00Z' })), 'none');
});
