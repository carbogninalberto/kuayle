import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_WORKFLOW,
	customWorkflows,
	defaultMayBeCustom,
	isCustomChoice,
	workflowEdit,
	workflowOptions
} from '../../src/lib/features/assistant/workflow-choice.ts';

const summary = (id, name, extra = {}) => ({ id, name, builtin: false, archived: false, ...extra });
const list = {
	workflows: [
		summary('standard', 'Standard', { builtin: true }),
		summary('loop', 'Review loop'),
		summary('old', 'Old flow', { archived: true }),
		summary('team', 'Team', { graph: { kind: 'pipeline', phases: [], roster: [] } })
	],
	defaults: [],
	can_manage: true
};
const labels = { default: 'Issue default', standard: 'Standard' };

test('only workflows a task can start on are offered, pipelines among them', () => {
	assert.deepEqual(
		customWorkflows(list).map((w) => w.id),
		['loop', 'team']
	);
	assert.deepEqual(workflowOptions(list, labels), [
		{ value: 'default', label: 'Issue default' },
		{ value: 'standard', label: 'Standard' },
		{ value: 'loop', label: 'Review loop' },
		{ value: 'team', label: 'Team' }
	]);
	assert.deepEqual(customWorkflows(null), []);
});

test('a proposed workflow the list no longer offers keeps its own option', () => {
	const options = workflowOptions(list, labels, { id: 'old', name: 'Old flow' });
	assert.deepEqual(options.at(-1), { value: 'old', label: 'Old flow' });
	assert.equal(workflowOptions(list, labels, { id: 'loop', name: 'Review loop' }).length, 4);
	assert.equal(workflowOptions(list, labels, { id: 'standard', name: 'Standard' }).length, 4);
});

test('a custom choice and a custom default are told apart from Standard', () => {
	assert.equal(isCustomChoice('loop'), true);
	assert.equal(isCustomChoice('standard'), false);
	assert.equal(isCustomChoice(DEFAULT_WORKFLOW), false);
	assert.equal(defaultMayBeCustom(list), false);
	assert.equal(
		defaultMayBeCustom({ ...list, defaults: [{ scope: 'team', scope_id: 't', workflow_id: 'loop' }] }),
		true
	);
});

test('a decision carries the workflow only when the approver changed it', () => {
	assert.equal(workflowEdit(DEFAULT_WORKFLOW, undefined), undefined);
	assert.equal(workflowEdit(DEFAULT_WORKFLOW, ''), undefined);
	assert.equal(workflowEdit('loop', 'loop'), undefined, 'the proposal, untouched');
	assert.deepEqual(workflowEdit('loop', undefined), { workflow_id: 'loop' });
	assert.deepEqual(workflowEdit('standard', 'loop'), { workflow_id: 'standard' });
	assert.deepEqual(workflowEdit(DEFAULT_WORKFLOW, 'loop'), { workflow_id: '' }, 'back to the issue default');
});
