import assert from 'node:assert/strict';
import test from 'node:test';
import { budgetPercent, budgetUse, pipelineMembers, pipelinePhases } from '../../src/lib/features/assistant/task-column.ts';

// The task column of the agent view (KUA-281): where a task is in its
// pipeline, who of its roster is working and how much of its budget it used,
// from what the task's workflow view already returns.
const pipeline = (extra = {}) => ({
	kind: 'pipeline',
	phases: [
		{ id: 'implement', label: 'Implement', owner: 'implementer' },
		{ id: 'review', label: 'Review', owner: 'implementer' },
		{ id: 'integrate', label: 'CI checks', owner: 'implementer' },
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'implementer', profile_id: 'p-impl', writer: true },
		{ role: 'reviewer', profile_id: 'p-rev' },
		{ role: 'security', profile_id: 'p-sec' }
	],
	...extra
});
const task = (extra = {}) => ({ id: 'task-1', state: 'awaiting_review', workflow_node: 'review', workflow_paused_at: null, ...extra });
const view = (extra = {}) => ({ node: 'review', status: 'waiting', ...extra });
const run = (id, role, status, minute, taskId = role === 'implementer' ? 'task-1' : `member-${role}`) => ({
	id,
	task_id: taskId,
	role,
	provider: role === 'implementer' ? 'claude-code' : 'codex',
	model: role === 'implementer' ? 'claude-opus-5-5' : 'gpt-5',
	status,
	created_at: `2026-11-25T10:${String(minute).padStart(2, '0')}:00Z`
});
const profiles = [
	{ id: 'p-impl', name: 'Implementer', harness: 'claude-code', model: 'claude-opus-5-5' },
	{ id: 'p-rev', name: 'Reviewer', harness: 'codex', model: 'gpt-5' }
];
const shape = (phases) => phases.map((phase) => [phase.id, phase.state, phase.status ?? null]);

test('the phases before the current one have passed, the current one says how it goes, the rest are ahead', () => {
	assert.deepEqual(shape(pipelinePhases(pipeline(), view(), task())), [
		['implement', 'passed', null],
		['review', 'current', 'waiting'],
		['integrate', 'ahead', null],
		['merge', 'ahead', null]
	]);
	// A turn running on the current phase.
	assert.deepEqual(
		shape(pipelinePhases(pipeline(), view({ status: 'running' }), task({ state: 'running', workflow_node: 'implement' }))).slice(0, 2),
		[
			['implement', 'current', 'running'],
			['review', 'ahead', null]
		]
	);
});

test('the task names its phase before its workflow view is read again', () => {
	const phases = pipelinePhases(pipeline(), view({ node: 'implement' }), task({ workflow_node: 'integrate' }));
	assert.equal(phases.find((phase) => phase.state === 'current')?.id, 'integrate');
	// Without the task's own, the view's phase.
	const fromView = pipelinePhases(pipeline(), view({ node: 'review' }), task({ workflow_node: null }));
	assert.equal(fromView.find((phase) => phase.state === 'current')?.id, 'review');
});

test('a finished pipeline has passed every phase; a failed or stopped one says so on its phase', () => {
	assert.deepEqual(
		shape(pipelinePhases(pipeline(), view({ status: 'done', node: 'merge' }), task({ state: 'completed', workflow_node: 'merge' }))),
		[
			['implement', 'passed', null],
			['review', 'passed', null],
			['integrate', 'passed', null],
			['merge', 'passed', null]
		]
	);
	const failed = pipelinePhases(pipeline(), view({ status: 'failed', node: 'integrate' }), task({ state: 'failed', workflow_node: 'integrate' }));
	assert.deepEqual(shape(failed)[2], ['integrate', 'current', 'failed']);
	const stopped = pipelinePhases(pipeline(), view(), task({ state: 'cancelled' }));
	assert.deepEqual(shape(stopped)[1], ['review', 'current', 'stopped']);
	// A person's pause, or the pipeline paused at a limit, waits.
	assert.equal(pipelinePhases(pipeline(), view({ status: 'running' }), task({ state: 'running', workflow_paused_at: '2026-11-25T10:00:00Z' }))[1].status, 'waiting');
	assert.equal(pipelinePhases(pipeline(), view({ status: 'paused' }), task())[1].status, 'waiting');
});

test('with a lead the earlier phases are not claimed as passed: it may have skipped or reordered them', () => {
	const phases = pipelinePhases(pipeline({ lead: { profile_id: 'p-lead' } }), view(), task({ workflow_node: 'integrate' }));
	assert.deepEqual(shape(phases).map(([id, state]) => [id, state]), [
		['implement', 'ahead'],
		['review', 'ahead'],
		['integrate', 'current'],
		['merge', 'ahead']
	]);
});

test('a phase the pipeline does not name has no current phase, and nothing is claimed as passed', () => {
	assert.deepEqual(
		pipelinePhases(pipeline(), view({ node: '' }), task({ workflow_node: 'gone' })).map((phase) => phase.state),
		['ahead', 'ahead', 'ahead', 'ahead']
	);
});

test('each role of the roster is a member, with its profile, its turns and whether it works now', () => {
	const members = pipelineMembers(
		pipeline(),
		[run('r1', 'implementer', 'succeeded', 1), run('r2', 'reviewer', 'succeeded', 3), run('r3', 'reviewer', 'running', 5)],
		profiles,
		task()
	);
	assert.deepEqual(
		members.map((member) => [member.role, member.profile ?? null, member.harness ?? null, member.writer, member.turns, member.working]),
		[
			['implementer', 'Implementer', 'claude-code', true, 1, false],
			['reviewer', 'Reviewer', 'codex', false, 2, true],
			// A role not called yet, with a profile the workspace no longer lists.
			['security', null, null, false, 0, false]
		]
	);
});

test('a member works while its newest turn is queued, starting, running or waiting for input', () => {
	for (const status of ['queued', 'starting', 'running', 'waiting_input']) {
		const [, reviewer] = pipelineMembers(pipeline(), [run('r1', 'reviewer', status, 1)], profiles, task());
		assert.equal(reviewer.working, true, status);
	}
	for (const status of ['succeeded', 'failed', 'cancelled', 'timeout']) {
		const [, reviewer] = pipelineMembers(pipeline(), [run('r1', 'reviewer', 'running', 1), run('r2', 'reviewer', status, 2)], profiles, task());
		assert.equal(reviewer.working, false, status);
	}
});

test('the writer works while the task itself runs, also before its turn has a run', () => {
	const running = pipelineMembers(pipeline(), [run('r1', 'implementer', 'succeeded', 1)], profiles, task({ state: 'queued' }));
	assert.equal(running[0].working, true);
	const fresh = pipelineMembers(pipeline(), [], profiles, task({ state: 'running' }));
	assert.equal(fresh[0].working, true);
	assert.equal(fresh[1].working, false);
	// Waiting between turns, nobody works.
	assert.equal(pipelineMembers(pipeline(), [], profiles, task({ state: 'awaiting_review' }))[0].working, false);
	// The task's own runs name its role, whichever role writes.
	const roles = pipelineMembers(pipeline(), [run('r1', 'reviewer', 'succeeded', 1, 'task-1')], profiles, task({ state: 'running' }));
	assert.deepEqual(roles.map((member) => member.working), [false, true, false]);
});

test('the harness and model come from the last turn when the profile is not listed', () => {
	const [, reviewer] = pipelineMembers(pipeline(), [run('r1', 'reviewer', 'succeeded', 1)], [], task());
	assert.equal(reviewer.profile, undefined);
	assert.equal(reviewer.harness, 'codex');
	assert.equal(reviewer.model, 'gpt-5');
});

test('a lead is a member after the roster', () => {
	const members = pipelineMembers(pipeline({ lead: { profile_id: 'p-rev' } }), [run('r1', 'lead', 'running', 1, 'member-lead')], profiles, task());
	assert.deepEqual(members.at(-1), {
		role: 'lead',
		profile: 'Reviewer',
		harness: 'codex',
		model: 'gpt-5',
		writer: false,
		lead: true,
		turns: 1,
		working: true
	});
});

test('a pipeline counts its turns against its turn budget, extensions included', () => {
	assert.deepEqual(budgetUse({ iterations: 7, graph: { budget: {} } }, pipeline({ budget: { max_turns: 40 } })), {
		unit: 'turns',
		used: 7,
		max: 40
	});
	assert.deepEqual(budgetUse({ iterations: 45, graph: { budget: {} } }, pipeline({ budget: { max_turns: 40 } }), 1), {
		unit: 'turns',
		used: 45,
		max: 80
	});
	// No limit: only what was used.
	assert.deepEqual(budgetUse({ iterations: 3, graph: { budget: {} } }, pipeline()), { unit: 'turns', used: 3 });
});

test('a graph workflow counts its iterations against its own budget', () => {
	assert.deepEqual(budgetUse({ iterations: 2, graph: { nodes: [], edges: [], budget: { max_iterations: 5 } } }, null), {
		unit: 'iterations',
		used: 2,
		max: 5
	});
	assert.deepEqual(budgetUse({ iterations: 1, graph: { nodes: [], edges: [], budget: {} } }, null), { unit: 'iterations', used: 1 });
	// Nothing used and no limit, or no view: nothing to say.
	assert.equal(budgetUse({ iterations: 0, graph: { nodes: [], edges: [], budget: {} } }, null), null);
	assert.equal(budgetUse(null, null), null);
});

test('the share of a budget used is capped at the whole', () => {
	assert.equal(budgetPercent({ unit: 'turns', used: 10, max: 40 }), 25);
	assert.equal(budgetPercent({ unit: 'turns', used: 50, max: 40 }), 100);
	assert.equal(budgetPercent({ unit: 'turns', used: 3 }), null);
});
