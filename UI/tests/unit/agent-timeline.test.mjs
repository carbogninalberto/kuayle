import assert from 'node:assert/strict';
import test from 'node:test';
import {
	attemptTimeline,
	openStep,
	stepDuration,
	workflowTimeline
} from '../../src/lib/features/assistant/agent-timeline.ts';
import { finalMessage, groupSession, parseAgentSession } from '../../src/lib/features/assistant/agent-transcript.ts';

const at = (minute) => `2026-11-01T10:${String(minute).padStart(2, '0')}:00Z`;
const profiles = [
	{ id: 'p-orch', name: 'Orchestrator', harness: 'claude-code', model: 'opus', behaviour: 'plans' },
	{ id: 'p-impl', name: 'Cheap Implementer', harness: 'opencode', model: 'kimi-k2', behaviour: 'changes_code' },
	{ id: 'p-rev', name: 'Reviewer', harness: 'codex', model: 'gpt-5', behaviour: 'reviews' }
];
const graph = {
	nodes: [
		{ id: 'start', type: 'start', label: 'Start' },
		{ id: 'plan', type: 'agent', label: 'Plan', profile_id: 'p-orch' },
		{ id: 'implement', type: 'agent', label: 'Implement', profile_id: 'p-impl' },
		{ id: 'publish', type: 'publish', label: 'Publish' },
		{ id: 'review', type: 'agent', label: 'Review', profile_id: 'p-rev' },
		{ id: 'ci', type: 'check', label: '', check: 'ci' }
	],
	edges: [],
	budget: {}
};
const step = (sequence, node_id, node_type, entered, extra = {}) => ({
	task_id: 'task-1',
	sequence,
	node_id,
	node_type,
	visit: 1,
	reason: '',
	detail: '',
	entered_at: at(entered),
	...extra
});
const run = (id, sequence, provider, model, status, extra = {}) => ({
	id,
	task_id: 'task-1',
	step: sequence,
	provider,
	model,
	status,
	created_at: at(0),
	...extra
});
const task = (extra = {}) => ({
	id: 'task-1',
	state: 'running',
	run_id: 'run-impl-2',
	provider: 'opencode',
	model: 'kimi-k2',
	generation: 3,
	updated_at: at(30),
	...extra
});

function view(extra = {}) {
	return {
		graph,
		profiles,
		status: 'running',
		steps: [
			step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
			step(2, 'plan', 'agent', 0, { generation: 1, outcome: 'agent_finished', finished_at: at(4), plan: '1. Add it' }),
			step(3, 'implement', 'agent', 4, { generation: 2, outcome: 'agent_finished', finished_at: at(12) }),
			step(4, 'publish', 'publish', 12, { outcome: 'published', finished_at: at(13) }),
			step(5, 'review', 'agent', 13, {
				review_task_id: 'review-1',
				outcome: 'review_changes_requested',
				finished_at: at(18),
				detail: 'Two findings'
			}),
			step(6, 'implement', 'agent', 18, { generation: 3, visit: 2, reason: 'Changes requested' })
		],
		runs: [
			run('run-plan', 2, 'claude-code', 'opus', 'succeeded', { summary: 'The plan' }),
			run('run-impl-1', 3, 'opencode', 'kimi-k2', 'succeeded'),
			run('run-review', 5, 'codex', 'gpt-5', 'succeeded', { task_id: 'review-1' }),
			run('run-impl-2', 6, 'opencode', 'kimi-k2', 'running')
		],
		...extra
	};
}

test('a workflow task is one section per step, each with who ran it and its own runs', () => {
	const steps = workflowTimeline(view(), task());
	assert.deepEqual(
		steps.map((s) => [s.anchor, s.kind, s.label, s.profile ?? '', s.harness ?? '', s.model ?? '', s.status, s.visit]),
		[
			['step-1', 'row', 'Start', '', '', '', 'done', 1],
			['step-2', 'agent', 'Plan', 'Orchestrator', 'claude-code', 'opus', 'done', 1],
			['step-3', 'agent', 'Implement', 'Cheap Implementer', 'opencode', 'kimi-k2', 'done', 1],
			['step-4', 'row', 'Publish', '', '', '', 'done', 1],
			['step-5', 'agent', 'Review', 'Reviewer', 'codex', 'gpt-5', 'changes', 1],
			['step-6', 'agent', 'Implement', 'Cheap Implementer', 'opencode', 'kimi-k2', 'running', 2]
		]
	);
	assert.deepEqual(
		steps.map((s) => s.runs.map((r) => r.id)),
		[[], ['run-plan'], ['run-impl-1'], [], ['run-review'], ['run-impl-2']]
	);
	// A planning step carries its plan; a reviewer step its review task, whose
	// run is read under that task.
	assert.equal(steps[1].plan, '1. Add it');
	assert.equal(steps[4].reviewTaskId, 'review-1');
	assert.equal(steps[4].runs[0].task_id, 'review-1');
	assert.equal(steps[4].detail, 'Two findings');
	assert.equal(steps[5].current, true);
	assert.equal(openStep(steps), 'step-6');
});

test('the step that ran shows the harness that really ran, not what its profile says now', () => {
	const edited = view({
		profiles: [{ ...profiles[0], harness: 'codex', model: 'gpt-5' }, ...profiles.slice(1)]
	});
	const plan = workflowTimeline(edited, task())[1];
	assert.equal(plan.harness, 'claude-code');
	assert.equal(plan.model, 'opus');
	// A profile deleted since still leaves a labelled step.
	const gone = workflowTimeline(view({ profiles: [] }), task())[2];
	assert.equal(gone.label, 'Implement');
	assert.equal(gone.profile, undefined);
	assert.equal(gone.harness, 'opencode');
});

test('an agent step whose run was not created yet waits on the task’s run', () => {
	const queued = view();
	queued.runs = queued.runs.slice(0, 3);
	const steps = workflowTimeline(queued, task({ state: 'queued' }));
	assert.deepEqual(steps[5].runs, [
		{ id: 'run-impl-2', task_id: 'task-1', step: 6, provider: 'opencode', model: 'kimi-k2', status: 'queued', created_at: at(18) }
	]);
	assert.equal(steps[5].status, 'running');
});

test('step status follows the outcome, and the open step follows the task', () => {
	const outcome = (value, type = 'agent') =>
		workflowTimeline(
			view({ steps: [step(1, 'implement', type, 0, { generation: 1, outcome: value, finished_at: at(1) })], runs: [] }),
			task({ state: 'failed' })
		)[0].status;
	assert.equal(outcome('agent_finished'), 'done');
	assert.equal(outcome('agent_failed'), 'failed');
	assert.equal(outcome('checks_failed', 'check'), 'failed');
	assert.equal(outcome('timeout', 'check'), 'failed');
	assert.equal(outcome('task_cancelled'), 'stopped');
	assert.equal(outcome('retried'), 'stopped');
	assert.equal(outcome('review_approved'), 'done');

	const open = (state, type = 'agent', extra = {}) =>
		workflowTimeline(view({ steps: [step(1, type === 'agent' ? 'implement' : 'ci', type, 0, extra)], runs: [] }), task({ state }))[0]
			.status;
	assert.equal(open('running'), 'running');
	assert.equal(open('waiting_input'), 'waiting');
	assert.equal(open('blocked'), 'waiting');
	// A reviewer works while the task itself waits for the review.
	assert.equal(open('awaiting_review', 'agent', { review_task_id: 'review-1' }), 'running');
	assert.equal(open('awaiting_review', 'check'), 'waiting');
	assert.equal(open('validating', 'check'), 'running');
	// A wait row without a label is named by its kind in the view.
	assert.equal(workflowTimeline(view({ steps: [step(1, 'ci', 'check', 0)], runs: [] }), task())[0].label, '');
});

test('with nothing running the last agent step is the open one', () => {
	const done = view({ status: 'done' });
	done.steps = done.steps.slice(0, 4);
	done.runs = done.runs.slice(0, 2);
	const steps = workflowTimeline(done, task({ state: 'awaiting_review', run_id: 'run-impl-1' }));
	assert.equal(openStep(steps), 'step-3');
	assert.equal(openStep([]), undefined);
	// A waiting row (an approval) keeps the last agent's work in view.
	const waiting = view({ steps: [...done.steps, step(5, 'ci', 'check', 13)], runs: done.runs });
	assert.equal(openStep(workflowTimeline(waiting, task({ state: 'awaiting_review' }))), 'step-3');
});

test('a task without a workflow is one step per attempt, oldest first', () => {
	const runs = [
		{ id: 'run-2', provider_id: 'codex', model: 'gpt-5', status: 'running', created_at: at(9), started_at: at(10) },
		{
			id: 'run-1',
			provider_id: 'claude-code',
			model: '',
			status: 'failed',
			created_at: at(1),
			started_at: at(2),
			completed_at: at(5)
		}
	];
	const steps = attemptTimeline(task({ run_id: 'run-2', provider: 'codex', model: 'gpt-5' }), runs);
	assert.deepEqual(
		steps.map((s) => [s.anchor, s.kind, s.harness, s.model ?? '', s.status, s.current, s.runs.map((r) => r.id)]),
		[
			['run-run-1', 'agent', 'claude-code', '', 'failed', false, ['run-1']],
			['run-run-2', 'agent', 'codex', 'gpt-5', 'running', true, ['run-2']]
		]
	);
	assert.equal(steps[0].startedAt, at(2));
	assert.equal(steps[0].finishedAt, at(5));
	assert.equal(openStep(steps), 'run-run-2');

	// Before its run exists the task is still one step, never an empty timeline.
	const queued = attemptTimeline(task({ state: 'queued', run_id: 'run-9', provider: 'codex', model: '' }), []);
	assert.equal(queued.length, 1);
	assert.deepEqual([queued[0].anchor, queued[0].harness, queued[0].status, queued[0].runs[0].id], [
		'run-run-9',
		'codex',
		'running',
		'run-9'
	]);
	// A finished task's last attempt takes the task's end, an older one left
	// unfinished reads as stopped.
	const finished = attemptTimeline(task({ state: 'awaiting_review', run_id: 'run-2' }), [
		{ ...runs[0], status: 'succeeded', completed_at: at(20) },
		{ ...runs[1], status: 'running', completed_at: undefined }
	]);
	assert.deepEqual(
		finished.map((s) => s.status),
		['stopped', 'done']
	);
});

test('a step’s duration runs to its end, or to now while it is going', () => {
	const [, plan, , , , current] = workflowTimeline(view(), task());
	assert.equal(stepDuration(plan, Date.parse(at(50))), 4 * 60_000);
	assert.equal(stepDuration(current, Date.parse(at(20))), 2 * 60_000);
	const waiting = { ...current, status: 'waiting' };
	assert.equal(stepDuration(waiting, Date.parse(at(20))), undefined);
	// Time a person paused the step is not time it worked.
	const paused = view();
	paused.steps[1] = { ...paused.steps[1], paused_seconds: 90 };
	paused.steps[5] = { ...paused.steps[5], paused_seconds: 60 };
	const steps = workflowTimeline(paused, task());
	assert.equal(stepDuration(steps[1], Date.parse(at(50))), 4 * 60_000 - 90_000);
	assert.equal(stepDuration(steps[5], Date.parse(at(20))), 60_000);
	assert.equal(stepDuration({ ...steps[1], pausedSeconds: 3600 }, Date.parse(at(50))), 0);
});

test('the final message of a finished run is told apart from its trace', () => {
	let id = 0;
	const chunk = (value) => ({ id: ++id, stream: 'stdout', content: JSON.stringify(value) + '\n', created_at: at(0) });
	const text = (value) => ({ type: 'assistant', message: { content: [{ type: 'text', text: value }] } });
	const tool = {
		type: 'assistant',
		message: { content: [{ type: 'tool_use', id: 't1', name: 'Bash', input: { command: 'ls' } }] }
	};
	const blocks = (...lines) => groupSession(parseAgentSession(lines.map(chunk)));
	const done = blocks(text('Looking.'), tool, text('**Done.**'), { type: 'result', subtype: 'success', duration_ms: 9 });
	const final = finalMessage(done);
	assert.equal(final?.text, '**Done.**');
	assert.ok(done.some((block) => block.key === final.key));
	// Work after the last message means there is no final one yet.
	assert.equal(finalMessage(blocks(text('Looking.'), tool)), undefined);
	assert.equal(finalMessage([]), undefined);
});
