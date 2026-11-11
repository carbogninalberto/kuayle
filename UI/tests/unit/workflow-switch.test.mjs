import assert from 'node:assert/strict';
import test from 'node:test';
import { mayHaveWorkflowUpdate, workflowUpdate } from '../../src/lib/features/assistant/workflow-switch.ts';

test('a task is offered the newest version of its workflow only when there is one', () => {
	assert.deepEqual(workflowUpdate(null), { kind: 'none' });
	assert.deepEqual(workflowUpdate({ from_version: 2, to_version: 2, refused: 'no_newer_version' }), { kind: 'none' });
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 3, node: 'implement', node_label: 'Build it' }), {
		kind: 'ready',
		version: 3,
		step: 'Build it'
	});
	// A step without a label is named by its id.
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 2, node: 'ci' }), { kind: 'ready', version: 2, step: 'ci' });
});

test('a task that cannot be moved yet, or at all, says which', () => {
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 2, refused: 'not_at_rest' }), {
		kind: 'pause_first',
		version: 2
	});
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 4, refused: 'step_removed' }), {
		kind: 'step_removed',
		version: 4,
		current: 1
	});
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 4, refused: 'step_changed' }), {
		kind: 'step_changed',
		version: 4,
		current: 1
	});
	// A finished task, or one without a workflow, is offered nothing.
	assert.deepEqual(workflowUpdate({ from_version: 1, to_version: 2, refused: 'finished' }), { kind: 'none' });
	assert.deepEqual(workflowUpdate({ from_version: 0, to_version: 0, refused: 'no_workflow' }), { kind: 'none' });
});

test('only a live workflow task is asked about', () => {
	const task = { state: 'running', kind: 'implement', workflow_version_id: 'v1' };
	assert.equal(mayHaveWorkflowUpdate(task), true);
	assert.equal(mayHaveWorkflowUpdate({ ...task, workflow_version_id: null }), false);
	assert.equal(mayHaveWorkflowUpdate({ ...task, kind: 'review' }), false);
	for (const state of ['completed', 'failed', 'cancelled']) assert.equal(mayHaveWorkflowUpdate({ ...task, state }), false);
});
