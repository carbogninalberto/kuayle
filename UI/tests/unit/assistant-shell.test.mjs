import assert from 'node:assert/strict';
import test from 'node:test';
import {
	conversationIndicator,
	contextUsage,
	dropMove,
	humanizeToolName,
	isConversationDrag,
	iterationReasons,
	placeByTime,
	splitBackgroundTasks,
	splitSteps,
	visibleHistory,
	CONVERSATION_DRAG_TYPE
} from '../../src/lib/features/assistant/shell-rules.ts';

const at = (minute) => `2026-10-26T10:${String(minute).padStart(2, '0')}:00Z`;

test('a failed task stays listed; only completed or cancelled ones are folded as finished', () => {
	const items = [
		{ id: 'a', state: 'running', updated_at: at(1) },
		{ id: 'b', state: 'failed', updated_at: at(2) },
		{ id: 'c', state: 'completed', updated_at: at(3) },
		{ id: 'd', state: 'cancelled', updated_at: at(4) },
		{ id: 'e', state: 'awaiting_review', updated_at: at(5) }
	];
	const { open, finished } = splitBackgroundTasks(items);
	// Failures first, then what waits on the person, then running work.
	assert.deepEqual(
		open.map((i) => i.id),
		['b', 'e', 'a']
	);
	assert.deepEqual(
		finished.map((i) => i.id),
		['c', 'd']
	);
	// A live state from the open conversation overrides the polled one.
	const live = splitBackgroundTasks(items, { a: { state: 'failed' } });
	assert.deepEqual(
		live.open.map((i) => i.id),
		['b', 'a', 'e']
	);
});

test('a conversation row shows the strongest indicator of its tasks', () => {
	const tasks = [
		{ conversation_id: 'x', state: 'running' },
		{ conversation_id: 'x', state: 'awaiting_review' },
		{ conversation_id: 'y', state: 'completed' },
		{ conversation_id: 'z', state: 'failed' },
		{ conversation_id: 'z', state: 'running' }
	];
	assert.equal(conversationIndicator('x', tasks), 'waiting');
	assert.equal(conversationIndicator('y', tasks), undefined);
	assert.equal(conversationIndicator('z', tasks), 'failed');
});

test('collapsing history keeps the latest messages and counts the folded ones', () => {
	const messages = Array.from({ length: 10 }, (_, i) => i);
	assert.deepEqual(visibleHistory(messages, false, 6), { shown: messages, hidden: 0 });
	assert.deepEqual(visibleHistory(messages, true, 6), { shown: [4, 5, 6, 7, 8, 9], hidden: 4 });
	assert.deepEqual(visibleHistory(messages.slice(0, 5), true, 6).hidden, 0);
});

test('context usage is the share of the 50-message model window', () => {
	const done = (n) => Array.from({ length: n }, () => ({ status: 'completed' }));
	assert.deepEqual(contextUsage([...done(10), { status: 'streaming' }], false), { used: 10, limit: 50, percent: 20 });
	assert.equal(contextUsage(done(80), false).percent, 100);
	assert.equal(contextUsage(done(3), true).percent, 100);
});

test('timed items sit before the first message created after them', () => {
	const messages = [
		{ id: 'm1', created_at: at(1) },
		{ id: 'm2', created_at: at(5) }
	];
	const items = [
		{ id: 't-late', created_at: at(9) },
		{ id: 't-early', created_at: at(0) },
		{ id: 't-mid', created_at: at(3) }
	];
	const placed = placeByTime(messages, items);
	assert.deepEqual(
		placed.before.m1.map((i) => i.id),
		['t-early']
	);
	assert.deepEqual(
		placed.before.m2.map((i) => i.id),
		['t-mid']
	);
	assert.deepEqual(
		placed.after.map((i) => i.id),
		['t-late']
	);
});

test('only a conversation drag moves, and only when the folder changes', () => {
	assert.equal(isConversationDrag([CONVERSATION_DRAG_TYPE, 'text/uri-list']), true);
	assert.equal(isConversationDrag(['text/uri-list', 'Files']), false);
	assert.equal(isConversationDrag(undefined), false);
	assert.deepEqual(dropMove({ id: 'c', folder_id: null }, 'f'), { id: 'c', folder: 'f' });
	assert.deepEqual(dropMove({ id: 'c', folder_id: 'f' }, null), { id: 'c', folder: null });
	assert.equal(dropMove({ id: 'c', folder_id: 'f' }, 'f'), null);
	assert.equal(dropMove({ id: 'c' }, null), null);
	assert.equal(dropMove(undefined, 'f'), null);
});

test('unknown tools get a readable label; long tool work folds behind +N', () => {
	assert.equal(humanizeToolName('web_search'), 'Web search');
	assert.equal(humanizeToolName('scan-repository'), 'Scan repository');
	assert.deepEqual(splitSteps([1, 2, 3, 4]), { inline: [1, 2, 3, 4], overflow: [] });
	assert.deepEqual(splitSteps([1, 2, 3, 4, 5, 6]), { inline: [1, 2, 3], overflow: [4, 5, 6] });
});

test('each iteration says why it started: a retry after failure differs from review changes', () => {
	const transitions = [
		{ sequence: 1, from_state: '', to_state: 'queued' },
		{ sequence: 2, from_state: 'queued', to_state: 'running' },
		{ sequence: 3, from_state: 'running', to_state: 'failed' },
		{ sequence: 4, from_state: 'failed', to_state: 'queued' },
		{ sequence: 5, from_state: 'running', to_state: 'awaiting_review' },
		{ sequence: 6, from_state: 'awaiting_review', to_state: 'queued' },
		{ sequence: 7, from_state: 'awaiting_review', to_state: 'queued' },
		{ sequence: 8, from_state: 'blocked', to_state: 'queued' }
	];
	const signals = [
		{ kind: 'review', action: 'follow_up', follow_up_generation: 3 },
		{ kind: 'ci', action: 'recorded', follow_up_generation: null }
	];
	assert.deepEqual(iterationReasons(transitions, signals), {
		1: 'first',
		2: 'retry_failed',
		3: 'review_changes',
		4: 'follow_up',
		5: 'retry_blocked'
	});
	assert.equal(
		iterationReasons(transitions.slice(0, 4), [{ kind: 'ci', action: 'follow_up', follow_up_generation: 2 }])[2],
		'ci_failed'
	);
});
