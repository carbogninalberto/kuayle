import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_WORKFLOW,
	customWorkflows,
	defaultMayBeCustom,
	isCustomChoice,
	startable,
	workflowEdit,
	workflowOptions
} from '../../src/lib/features/assistant/workflow-choice.ts';

const pipeline = { kind: 'pipeline', phases: [], roster: [] };
const summary = (id, name, extra = {}) => ({ id, name, builtin: false, archived: false, graph: pipeline, ...extra });
const list = {
	workflows: [
		summary('standard', 'Standard', { builtin: true, graph: { nodes: [], edges: [], budget: {} } }),
		summary('loop', 'Review loop'),
		summary('old', 'Old flow', { archived: true }),
		summary('team', 'Team')
	],
	defaults: [],
	can_manage: true,
	pipelines_enabled: true
};
const labels = { default: 'Issue default', standard: 'Standard' };

test('only pipelines a task can start on are offered besides Standard', () => {
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
	assert.deepEqual(
		customWorkflows({ ...list, pipelines_enabled: false }).map((w) => w.id),
		[],
		'a server with pipelines turned off offers only Standard'
	);
	assert.deepEqual(
		workflowOptions({ ...list, pipelines_enabled: false }, labels).map((o) => o.value),
		['default', 'standard']
	);
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

// KUA-291: wherever a workflow is chosen (the composer, the approval card, the
// defaults and the automations), a pipeline is offered only while the server
// has pipelines on, and an archived workflow never.
test('a pipeline is offered only while pipelines are on', () => {
	const off = { ...list, pipelines_enabled: false };
	const byId = (id) => list.workflows.find((w) => w.id === id);
	assert.equal(startable(list, byId('team')), true);
	assert.equal(startable(off, byId('team')), false);
	assert.equal(startable(list, byId('old')), false);
	assert.equal(startable(null, byId('team')), false);
});

// Graph workflows were removed: the server refuses a task on one, so a graph
// that is still listed (an older server) is never offered.
test('a graph workflow is never offered', () => {
	const graph = summary('legacy', 'Legacy', { graph: { nodes: [], edges: [], budget: {} } });
	assert.equal(startable(list, graph), false);
	assert.deepEqual(
		customWorkflows({ ...list, workflows: [...list.workflows, graph] }).map((w) => w.id),
		['loop', 'team']
	);
});
