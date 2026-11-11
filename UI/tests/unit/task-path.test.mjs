import assert from 'node:assert/strict';
import test from 'node:test';
import { attemptTimeline, workflowTimeline } from '../../src/lib/features/assistant/agent-timeline.ts';
import { compactPath, pathDuration, pathTime, taskPath } from '../../src/lib/features/assistant/task-path.ts';

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
		{ id: 'ci', type: 'check', label: 'CI', check: 'ci' },
		{ id: 'approve', type: 'approval', label: 'Approve', prompt: 'Ship it to production?' },
		{ id: 'merge', type: 'merge', label: 'Merge' },
		{ id: 'end', type: 'end', label: 'Done' }
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
const task = (extra = {}) => ({
	id: 'task-1',
	state: 'awaiting_review',
	reason: 'Waiting for CI on the pull request',
	run_id: 'run-impl-2',
	provider: 'opencode',
	model: 'kimi-k2',
	generation: 3,
	updated_at: at(30),
	...extra
});

// A task like KUA-122: planned, implemented, published, sent back by the
// review once, and now waiting for CI.
function view(extra = {}) {
	return {
		graph,
		profiles,
		status: 'waiting',
		steps: [
			step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
			step(2, 'plan', 'agent', 0, { from_node: 'start', condition: 'else', outcome: 'agent_finished', finished_at: at(4) }),
			step(3, 'implement', 'agent', 4, { from_node: 'plan', condition: 'agent_finished', outcome: 'agent_finished', finished_at: at(12) }),
			step(4, 'publish', 'publish', 12, { from_node: 'implement', condition: 'agent_finished', outcome: 'published', finished_at: at(13) }),
			step(5, 'review', 'agent', 13, { from_node: 'publish', condition: 'published', outcome: 'review_changes_requested', finished_at: at(18) }),
			step(6, 'implement', 'agent', 18, { visit: 2, from_node: 'review', condition: 'review_changes_requested', outcome: 'agent_finished', finished_at: at(24) }),
			step(7, 'publish', 'publish', 24, { visit: 2, from_node: 'implement', condition: 'agent_finished', outcome: 'published', finished_at: at(25) }),
			step(8, 'review', 'agent', 25, { visit: 2, from_node: 'publish', condition: 'published', outcome: 'review_approved', finished_at: at(29) }),
			step(9, 'ci', 'check', 29, { from_node: 'review', condition: 'review_approved' })
		],
		runs: [],
		...extra
	};
}
const path = (v, t, extra = {}) => taskPath(workflowTimeline(v, t), { task: t, pauses: v.pauses, status: v.status, ...extra });
const names = (entries) =>
	entries.map((entry) => (entry.kind === 'step' ? `${entry.step.label}${entry.step.visit > 1 ? ` ×${entry.step.visit}` : ''}` : entry.action));

test('the full path is every step in order; each says where the task went next and why', () => {
	const entries = path(view(), task());
	assert.deepEqual(names(entries), [
		'Start',
		'Plan',
		'Implement',
		'Publish',
		'Review',
		'Implement ×2',
		'Publish ×2',
		'Review ×2',
		'CI'
	]);
	assert.deepEqual(
		entries.map((entry) => (entry.next ? [entry.next.label, entry.next.condition] : null)),
		[
			['Plan', 'else'],
			['Implement', 'agent_finished'],
			['Publish', 'agent_finished'],
			['Review', 'published'],
			['Implement', 'review_changes_requested'],
			['Publish', 'agent_finished'],
			['Review', 'published'],
			['CI', 'review_approved'],
			null
		]
	);
	assert.deepEqual(
		entries.map((entry) => entry.step.status),
		['done', 'done', 'done', 'done', 'changes', 'done', 'done', 'done', 'waiting']
	);
	assert.equal(entries[5].key, 'step-6');
});

test('compact is each step once, as it last went, without the start', () => {
	const entries = path(view(), task());
	const compact = compactPath(entries);
	assert.deepEqual(names(compact), ['Plan', 'Implement ×2', 'Publish ×2', 'Review ×2', 'CI']);
	assert.deepEqual(
		compact.map((entry) => entry.step.status),
		['done', 'done', 'done', 'done', 'waiting']
	);
	// A loop that is under way shows the last outcome and the step it led to.
	const looping = view();
	looping.steps = looping.steps.slice(0, 6).map((s) => (s.sequence === 6 ? { ...s, outcome: undefined, finished_at: undefined } : s));
	const live = compactPath(path({ ...looping, status: 'running' }, task({ state: 'running' })));
	assert.deepEqual(names(live), ['Plan', 'Publish', 'Review', 'Implement ×2']);
	assert.deepEqual(
		live.slice(-2).map((entry) => entry.step.status),
		['changes', 'running']
	);
	// A path of one step, even the start, is never empty.
	const fresh = view({ steps: [step(1, 'start', 'start', 0)], status: 'running' });
	assert.deepEqual(names(compactPath(path(fresh, task({ state: 'queued' })))), ['Start']);
});

test('a waiting step says what it waits for', () => {
	const waits = (node, type, t = task(), extra = {}) =>
		path(view({ steps: [step(1, node, type, 0)], ...extra }), t).at(-1).wait;
	assert.deepEqual(waits('ci', 'check'), { kind: 'ci' });
	assert.deepEqual(waits('approve', 'approval'), { kind: 'approval', question: 'Ship it to production?' });
	assert.deepEqual(waits('merge', 'merge'), { kind: 'merge' });
	// A limit the workflow stopped at waits for a person's decision.
	assert.deepEqual(waits('implement', 'agent', task({ state: 'blocked', reason: 'Visit limit reached' }), { status: 'paused' }), {
		kind: 'limit',
		text: 'Visit limit reached'
	});
	// Paused by its creator, whatever it was doing.
	assert.deepEqual(waits('ci', 'check', task({ workflow_paused_at: at(31) })), { kind: 'paused' });
	// Anything else says what the task itself says.
	assert.deepEqual(waits('implement', 'agent', task({ state: 'waiting_input', reason: 'Needs an answer' })), {
		kind: 'other',
		text: 'Needs an answer'
	});
	// A step that works, or is over, waits for nothing.
	assert.equal(waits('implement', 'agent', task({ state: 'running' })), undefined);
	assert.equal(path(view(), task())[2].wait, undefined);
});

test('a failed task says the step and the reason it stopped at', () => {
	const failed = view({ status: 'failed' });
	failed.steps = failed.steps.slice(0, 3).map((s) => (s.sequence === 3 ? { ...s, outcome: 'agent_failed', finished_at: at(9) } : s));
	const entries = compactPath(path(failed, task({ state: 'failed', reason: 'The agent exited with status 1' })));
	assert.deepEqual(names(entries), ['Plan', 'Implement']);
	assert.equal(entries.at(-1).step.status, 'failed');
	assert.equal(entries.at(-1).stopped, 'The agent exited with status 1');
	assert.equal(entries[0].stopped, undefined);
	// A task that goes on has stopped nowhere.
	assert.equal(path(view(), task()).at(-1).stopped, undefined);
});

test('pauses, resumes and a move to a newer version sit where they happened', () => {
	const paused = view({
		pauses: [
			{ action: 'pause_requested', sequence: 3, mode: 'after_step', at: '2026-11-01T10:05:00Z' },
			{ action: 'paused', sequence: 3, mode: 'now', at: '2026-11-01T10:06:00Z' },
			{ action: 'resumed', sequence: 3, at: '2026-11-01T10:08:00Z' },
			{ action: 'pause_withdrawn', sequence: 5, at: '2026-11-01T10:14:00Z' },
			{ action: 'paused', sequence: 9, at: '2026-11-01T10:30:00Z' },
			{ action: 'version_switched', sequence: 9, from_version: 2, to_version: 3, at: '2026-11-01T10:31:00Z' }
		]
	});
	const entries = path(paused, task({ workflow_paused_at: '2026-11-01T10:30:00Z' }));
	assert.deepEqual(names(entries), [
		'Start',
		'Plan',
		'Implement',
		'paused',
		'resumed',
		'Publish',
		'Review',
		'Implement ×2',
		'Publish ×2',
		'Review ×2',
		'CI',
		'paused',
		'moved'
	]);
	const moved = entries.at(-1);
	assert.deepEqual([moved.from, moved.to, moved.at], [2, 3, '2026-11-01T10:31:00Z']);
	assert.equal(new Set(entries.map((entry) => entry.key)).size, entries.length);
	// Compact keeps the steps; that it is paused is said on the step it is on.
	const compact = compactPath(entries);
	assert.deepEqual(names(compact), ['Plan', 'Implement ×2', 'Publish ×2', 'Review ×2', 'CI']);
	assert.deepEqual(compact.at(-1).wait, { kind: 'paused' });
});

test('a task without a workflow is its attempts, each with its state changes', () => {
	const runs = [
		{ id: 'run-1', provider_id: 'claude-code', status: 'failed', created_at: at(1), started_at: at(2), completed_at: at(5) },
		{ id: 'run-2', provider_id: 'codex', status: 'failed', created_at: at(6), started_at: at(7), completed_at: at(8) },
		{ id: 'run-3', provider_id: 'codex', status: 'succeeded', created_at: at(9), started_at: at(10), completed_at: at(13) }
	];
	const change = (sequence, from_state, to_state, minute, reason = '') => ({
		sequence,
		task_id: 'task-1',
		from_state,
		to_state,
		reason,
		created_at: at(minute)
	});
	const transitions = [
		change(1, '', 'queued', 1, 'Created'),
		change(2, 'queued', 'running', 2),
		change(3, 'running', 'failed', 5, 'The build is broken'),
		change(4, 'failed', 'queued', 6, 'Retried'),
		change(5, 'queued', 'failed', 8, 'No machine'),
		change(6, 'failed', 'queued', 9, 'Retried'),
		change(7, 'queued', 'running', 10),
		change(8, 'running', 'awaiting_review', 13, 'Ready for review')
	];
	const t = task({ run_id: 'run-3', provider: 'codex', reason: 'Ready for review' });
	const entries = taskPath(attemptTimeline(t, runs), { task: t, transitions });
	assert.deepEqual(
		entries.map((entry) => [entry.step.harness, entry.step.visit, entry.step.status, entry.changes.map((c) => c.state)]),
		[
			['claude-code', 1, 'failed', ['queued', 'running', 'failed']],
			['codex', 2, 'failed', ['queued', 'failed']],
			['codex', 3, 'done', ['queued', 'running', 'awaiting_review']]
		]
	);
	assert.deepEqual(entries[0].changes[2], { state: 'failed', reason: 'The build is broken', at: at(5) });
	// The attempt is over and its task waits: it says what the task says.
	assert.deepEqual(entries.at(-1).wait, { kind: 'other', text: 'Ready for review' });
	assert.equal(entries[0].wait, undefined);
	assert.equal(entries.at(-1).next, undefined);
	// Compact is the last outcome and the attempt the task is on.
	assert.deepEqual(
		compactPath(entries).map((entry) => entry.key),
		['run-run-2', 'run-run-3']
	);
	// Older attempts that are not loaded leave the newest ones their changes.
	const recent = taskPath(attemptTimeline(t, runs.slice(1)), { task: t, transitions });
	assert.deepEqual(
		recent.map((entry) => [entry.step.visit, entry.changes.length]),
		[
			[2, 2],
			[3, 3]
		]
	);
	// A failed task stops at its last attempt, with the task's reason.
	const failed = task({ state: 'failed', run_id: 'run-2', reason: 'No machine' });
	const stopped = taskPath(attemptTimeline(failed, runs.slice(0, 2)), { task: failed, transitions: transitions.slice(0, 5) });
	assert.equal(stopped.at(-1).stopped, 'No machine');
});

test('durations and times are short enough to scan', () => {
	assert.deepEqual(
		[0, 42_000, 59_600, 60_000, 6 * 60_000 + 29_000, 3_600_000, 3_900_000, 26 * 3_600_000, 48 * 3_600_000].map(pathDuration),
		['0s', '42s', '1m', '1m', '6m', '1h', '1h 5m', '1d 2h', '2d']
	);
	// Today is a time; another day says which.
	const now = new Date(2026, 10, 1, 18, 0).getTime();
	const today = pathTime(new Date(2026, 10, 1, 9, 5).toISOString(), now, 'en-GB');
	const before = pathTime(new Date(2026, 9, 30, 9, 5).toISOString(), now, 'en-GB');
	assert.equal(today, '09:05');
	assert.match(before, /30 Oct/);
	assert.match(before, /09:05/);
});
