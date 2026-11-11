import assert from 'node:assert/strict';
import test from 'node:test';
import { answerTo, interleave, placeConversation, sendOptions } from '../../src/lib/features/assistant/agent-conversation.ts';

const at = (minute, second = 0) =>
	`2026-11-11T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`;
const toAgent = (seq, body, extra = {}) => ({
	id: `to-${seq}`,
	task_id: 'task-1',
	direction: 'to_agent',
	seq,
	sender: 'user',
	author_id: 'user-1',
	kind: 'message',
	body,
	state: 'queued',
	delivered_via: null,
	run_id: null,
	reject_reason: null,
	created_at: at(seq),
	delivered_at: null,
	deliveries: 0,
	...extra
});
const delivered = (seq, body, run, minute, extra = {}) =>
	toAgent(seq, body, {
		state: 'delivered',
		delivered_via: 'inbox',
		run_id: run,
		delivered_at: at(minute),
		deliveries: 1,
		...extra
	});
const fromAgent = (id, body, run, minute, extra = {}) => ({
	id,
	task_id: 'task-1',
	direction: 'from_agent',
	seq: null,
	sender: 'agent',
	author_id: null,
	kind: 'message',
	body,
	state: 'received',
	delivered_via: null,
	run_id: run,
	reject_reason: null,
	created_at: at(minute),
	delivered_at: null,
	...extra
});
const steps = (current = 'running', last = 'running', first = 'failed') => [
	{ current: false, status: 'done', runs: [{ id: 'run-plan', status: 'succeeded' }] },
	{
		current: true,
		status: current,
		runs: [
			{ id: 'run-1', status: first },
			{ id: 'run-2', status: last }
		]
	}
];
const running = { state: 'running', run_id: 'run-2' };
const states = (list) => list.map((placed) => [placed.message.id, placed.state]);

test('a message sits in the run that received it, and its reply is the next item', () => {
	const conversation = placeConversation(
		[
			delivered(1, 'Keep the v1 route', 'run-2', 5),
			fromAgent('reply-1', 'Understood: v1 stays.', 'run-2', 6, { reply_to: 1 }),
			fromAgent('note', 'Tests added', 'run-2', 4),
			delivered(2, 'Use the shared client', 'run-2', 9)
		],
		steps(),
		running
	);
	assert.deepEqual([...conversation.byRun.keys()], ['run-2']);
	const placed = conversation.byRun.get('run-2');
	assert.deepEqual(states(placed), [
		['note', 'received'],
		['to-1', 'replied'],
		['to-2', 'delivered']
	]);
	assert.deepEqual(
		placed[1].replies.map((reply) => reply.id),
		['reply-1'],
		'the reply reads under its message, not on its own'
	);
	assert.equal(placed[1].at, at(5), 'a message is placed when it was delivered, not when it was written');
	assert.deepEqual(conversation.pending, []);
	assert.equal(conversation.unanswered.size, 0, 'the attempt can still answer');
});

test('a waiting message is shown where it will be picked up', () => {
	const queued = [toAgent(1, 'Also add tests')];
	const live = placeConversation(queued, steps(), running);
	assert.deepEqual(states(live.byRun.get('run-2')), [['to-1', 'queued']], 'the run that is going takes it');
	assert.deepEqual(live.pending, []);

	for (const [status, task] of [
		['waiting', { state: 'awaiting_review', run_id: 'run-2' }],
		['failed', { state: 'failed', run_id: 'run-2' }]
	]) {
		const idle = placeConversation(queued, steps(status), task);
		assert.equal(idle.byRun.size, 0, status);
		assert.deepEqual(states(idle.pending), [['to-1', 'queued']], 'no step runs: it waits after the last one');
	}
});

test('a message without a reply is flagged once its attempt can no longer answer', () => {
	const messages = [
		delivered(1, 'Keep the v1 route', 'run-1', 3),
		delivered(2, 'Old message', 'run-1', 2, { deliveries: 0 }),
		delivered(3, 'Use the shared client', 'run-2', 8)
	];
	const later = placeConversation(messages, steps(), running);
	assert.deepEqual(states(later.byRun.get('run-1')), [
		['to-2', 'delivered'],
		['to-1', 'unanswered']
	]);
	assert.deepEqual([...later.unanswered], ['run-1'], 'the earlier attempt ended owing a reply');
	assert.deepEqual(states(later.byRun.get('run-2')), [['to-3', 'delivered']]);

	const failed = placeConversation(messages, steps('failed'), { state: 'failed', run_id: 'run-2' });
	assert.deepEqual(states(failed.byRun.get('run-2')), [['to-3', 'unanswered']]);
	assert.deepEqual([...failed.unanswered].sort(), ['run-1', 'run-2']);

	// A paused or interrupted attempt continues: its message is not flagged.
	const paused = placeConversation(messages, steps('waiting', 'cancelled'), { state: 'blocked', run_id: 'run-2' });
	assert.deepEqual(states(paused.byRun.get('run-2')), [['to-3', 'delivered']]);

	// A missing reply never fails the attempt: it succeeds, the task goes on
	// to its check on the same run, and the message is flagged all the same.
	for (const state of ['validating', 'publishing', 'awaiting_review']) {
		const succeeded = placeConversation(messages, steps('running', 'succeeded'), { state, run_id: 'run-2' });
		assert.deepEqual(states(succeeded.byRun.get('run-2')), [['to-3', 'unanswered']], state);
		assert.ok(succeeded.unanswered.has('run-2'), state);
	}
});

test('a reply that may exist unseen is not called missing', () => {
	// Kuayle could not collect the agent's last messages when the attempt
	// ended: the message stays "delivered" until more is known.
	const messages = [delivered(3, 'Use the shared client', 'run-2', 8)];
	const unknown = [
		{ current: true, status: 'running', runs: [{ id: 'run-2', status: 'succeeded', replies_unknown: true }] }
	];
	for (const task of [
		{ state: 'validating', run_id: 'run-2' },
		{ state: 'failed', run_id: 'run-2' },
		{ state: 'running', run_id: 'run-3' }
	]) {
		const conversation = placeConversation(messages, unknown, task);
		assert.deepEqual(states(conversation.byRun.get('run-2')), [['to-3', 'delivered']], task.state);
		assert.equal(conversation.unanswered.size, 0, task.state);
	}
});

test('messages of a run the timeline does not list, refusals and stray replies are kept', () => {
	const conversation = placeConversation(
		[
			delivered(1, 'Before the list', 'run-gone', 1),
			fromAgent('stray', 'Reply to nothing', 'run-2', 5, { reply_to: 9 }),
			fromAgent('refused', '{"to":"orchestrator"', 'run-2', 6, { state: 'rejected', reject_reason: 'invalid JSON message' }),
			fromAgent('late', 'Reply to a refusal', 'run-2', 7, { reply_to: 1, state: 'rejected', reject_reason: 'x' })
		],
		steps(),
		running
	);
	assert.deepEqual(states(conversation.pending), [['to-1', 'unanswered']]);
	assert.deepEqual(states(conversation.byRun.get('run-2')), [
		['stray', 'received'],
		['refused', 'rejected'],
		['late', 'rejected']
	]);
});

test('messages are placed in the session by time', () => {
	const blocks = [
		{ kind: 'header', key: 'h' },
		{ kind: 'text', key: 't1', text: 'Reading the router.', at: at(1) },
		{ kind: 'tool', key: 'c1', tool: { id: 'c1', started: at(2) } },
		{ kind: 'thinking', key: 'th' },
		{ kind: 'steps', key: 's1', tools: [{ id: 'c2', started: at(6) }], entries: [] },
		{ kind: 'result', key: 'r', failed: false }
	];
	const placed = (id, minute) => ({ message: { id }, state: 'delivered', at: at(minute), replies: [] });
	assert.deepEqual(
		interleave(blocks, [placed('a', 0), placed('b', 4), placed('c', 9)]).map((item) => item.key),
		['h', 'message-a', 't1', 'c1', 'th', 'message-b', 's1', 'r', 'message-c'],
		'each before the first thing the agent did after it; the last at the end'
	);
	assert.deepEqual(
		interleave([], [placed('a', 0)]).map((item) => item.key),
		['message-a'],
		'a run without output yet still shows what was sent'
	);
	assert.deepEqual(
		interleave(blocks, []).map((item) => item.kind),
		blocks.map(() => 'block')
	);
});

test('the composer offers what the task can take', () => {
	const task = (extra = {}) => ({
		kind: 'implement',
		state: 'running',
		creator_id: 'user-1',
		provider: 'claude-code',
		workflow_version_id: 'wf-1',
		...extra
	});
	const options = (now, confirm = false) => ({ open: true, now, confirm });
	const closed = { open: false, now: null, confirm: false };
	assert.deepEqual(sendOptions(task(), 'user-1'), options('interrupt'));
	assert.deepEqual(sendOptions(task({ state: 'waiting_input' }), 'user-1'), options('interrupt'));
	assert.deepEqual(
		sendOptions(task({ workflow_version_id: null }), 'user-1'),
		options('interrupt'),
		'a task without a workflow is interrupted like any other'
	);
	// Codex and OpenCode are interrupted by any message: one action is enough.
	for (const provider of ['codex', 'opencode'])
		assert.deepEqual(sendOptions(task({ provider }), 'user-1'), options(null), provider);
	for (const state of ['failed', 'blocked'])
		assert.deepEqual(sendOptions(task({ state }), 'user-1'), options('start'), state);
	assert.deepEqual(
		sendOptions(task({ state: 'awaiting_review' }), 'user-1'),
		options('start', true),
		'starting now abandons what the task waits for: the person is asked'
	);
	assert.deepEqual(
		sendOptions(task({ state: 'blocked', usage_limit_message: 'Limit reached' }), 'user-1'),
		options(null),
		'a usage limit is not lifted by a message'
	);
	for (const state of ['queued', 'preparing', 'validating', 'publishing'])
		assert.deepEqual(sendOptions(task({ state }), 'user-1'), options(null), state);
	for (const extra of [{ workflow_paused_at: at(3) }, { interrupting: true }])
		assert.deepEqual(
			sendOptions(task({ state: 'cancelling', ...extra }), 'user-1'),
			options(null),
			'an agent being interrupted or paused still takes what is said meanwhile'
		);
	for (const state of ['completed', 'cancelled', 'cancelling'])
		assert.deepEqual(sendOptions(task({ state }), 'user-1'), closed, state);
	assert.deepEqual(sendOptions(task(), 'user-2'), closed, 'only the creator writes to the agent');
	assert.deepEqual(sendOptions(task(), undefined), closed);
	assert.deepEqual(sendOptions(task({ kind: 'review' }), 'user-1'), closed);
});

test('an answer names the question it answers', () => {
	assert.equal(
		answerTo('Keep the v1 route?\n  It is still used.', 'Yes, keep it.'),
		'In answer to your question "Keep the v1 route? It is still used.":\nYes, keep it.'
	);
	const long = answerTo('x'.repeat(400), 'No.');
	assert.ok(long.includes('x'.repeat(279) + '…"'));
	assert.ok(!long.includes('x'.repeat(280)));
});
