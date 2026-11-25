import assert from 'node:assert/strict';
import test from 'node:test';
import {
	firstLine,
	shownRows,
	stepThread,
	threadRoles,
	turnSummary,
	workTokens
} from '../../src/lib/features/assistant/thread-rows.ts';

// One line per event (KUA-286): what a turn's line says, how much it
// worked through, which rows a filter keeps and how a graph workflow's
// steps map to the same rows.
const at = (minute) => `2026-11-25T10:${String(minute).padStart(2, '0')}:00Z`;
const post = (seq, kind, body, extra = {}) => ({
	id: `message-${seq}`,
	task_id: 'task-1',
	seq,
	author_kind: 'member',
	author_role: 'implementer',
	author_user_id: null,
	author_member_id: 'member-implementer',
	run_id: 'run-1',
	to_roles: [],
	kind,
	body,
	data: {},
	reply_to: null,
	reject_reason: null,
	created_at: at(seq),
	...extra
});

test('a first line is the first line with text, trimmed and capped', () => {
	assert.equal(firstLine('\n\n  Search is added.  \nAnd covered.'), 'Search is added.');
	assert.equal(firstLine('**Done:** the `query` param'), 'Done: the query param');
	assert.equal(firstLine('# Summary\nAll good'), 'Summary');
	assert.equal(firstLine(''), '');
	assert.equal(firstLine(null), '');
	assert.equal(firstLine(undefined), '');
	assert.equal(firstLine('x'.repeat(300), 10), `${'x'.repeat(9)}…`);
});

test("a turn's summary is its run's final message, else what the turn last posted", () => {
	assert.equal(turnSummary({ summary: 'Renamed q to query.\n\nTests pass.' }, [post(1, 'say', 'Working on it')]), 'Renamed q to query.');
	// Running, the final message is not there yet: the newest post says it.
	assert.equal(turnSummary({ summary: null }, [post(1, 'say', 'First'), post(2, 'ask', 'Case-insensitive?\nOr not')]), 'Case-insensitive?');
	// A refused post is not what the turn said.
	assert.equal(
		turnSummary(undefined, [post(1, 'say', 'Pushed.'), post(2, 'verdict', 'I approve', { reject_reason: 'no' })]),
		'Pushed.'
	);
	assert.equal(turnSummary(undefined, []), '');
});

test('tokens are what the runs worked through: input and output, when reported', () => {
	assert.equal(workTokens([{ input_tokens: 315, output_tokens: 122, cache_read_tokens: 24448 }]), 437);
	assert.equal(workTokens([{ input_tokens: 10 }, { output_tokens: 5 }, {}]), 15);
	// Nothing reported is nothing to show, not zero.
	assert.equal(workTokens([{}, { cost_usd: 0.4 }]), undefined);
	assert.equal(workTokens([]), undefined);
	assert.equal(workTokens([{ input_tokens: 0, output_tokens: 0 }]), 0);
});

// Rows as the room and the timeline give them: who they concern and
// whether they are a decision.
const rows = [
	{ key: 'a', roles: ['implementer'], decision: false },
	{ key: 'b', roles: [], decision: false },
	{ key: 'c', roles: ['reviewer'], decision: true },
	{ key: 'd', roles: ['implementer', 'reviewer'], decision: true },
	{ key: 'e', roles: [], decision: true }
];
const keys = (list) => list.map((row) => row.key);
const roles = (row) => row.roles;
const decision = (row) => row.decision;

test('a filter keeps the rows of a role, the decisions, or both', () => {
	assert.deepEqual(keys(shownRows(rows, { role: '', decisions: false }, roles, decision)), ['a', 'b', 'c', 'd', 'e']);
	assert.deepEqual(keys(shownRows(rows, { role: 'implementer', decisions: false }, roles, decision)), ['a', 'd']);
	assert.deepEqual(keys(shownRows(rows, { role: '', decisions: true }, roles, decision)), ['c', 'd', 'e']);
	assert.deepEqual(keys(shownRows(rows, { role: 'reviewer', decisions: true }, roles, decision)), ['c', 'd']);
	assert.deepEqual(keys(shownRows(rows, { role: 'lead', decisions: false }, roles, decision)), []);
});

test('the roles to filter by are the ones that appear, in order, once each', () => {
	assert.deepEqual(threadRoles(rows, roles), ['implementer', 'reviewer']);
	assert.deepEqual(threadRoles([], roles), []);
});

// A graph workflow's steps on the same rows.
const step = (anchor, kind, nodeType, extra = {}) => ({
	anchor,
	kind,
	nodeType,
	label: '',
	status: 'done',
	visit: 1,
	current: false,
	startedAt: at(1),
	runs: [],
	detail: '',
	reason: '',
	...extra
});
const roleOf = (s) => s.profile ?? s.label;

test("a graph workflow's agent steps are turns of their role; Kuayle's steps in a row fold into one", () => {
	const steps = [
		step('step-1', 'row', 'start'),
		step('step-2', 'agent', 'agent', { label: 'Implement', profile: 'Implementer' }),
		step('step-3', 'row', 'publish'),
		step('step-4', 'row', 'check'),
		step('step-5', 'agent', 'agent', { label: 'Review', profile: 'Reviewer', reviewTaskId: 'review-1', status: 'changes' }),
		step('step-6', 'row', 'approval'),
		step('step-7', 'row', 'merge'),
		step('step-8', 'row', 'end', { current: true, status: 'running' })
	];
	const thread = stepThread(steps, roleOf);
	assert.deepEqual(
		thread.map((row) => [row.type, row.key, row.type === 'fold' ? row.steps.map((s) => s.anchor) : row.step.anchor, row.role, row.decision]),
		[
			['step', 'step-1', 'step-1', '', false],
			['step', 'step-2', 'step-2', 'Implementer', false],
			// Publish and the CI check are one row.
			['fold', 'fold-step-3', ['step-3', 'step-4'], '', false],
			// A review's verdict and an approval are decisions; they never fold.
			['step', 'step-5', 'step-5', 'Reviewer', true],
			['step', 'step-6', 'step-6', '', true],
			// The step the task is on stays its own row.
			['step', 'step-7', 'step-7', '', false],
			['step', 'step-8', 'step-8', '', false]
		]
	);
	const filter = (f) => keys(shownRows(thread, f, (row) => (row.role ? [row.role] : []), (row) => row.decision));
	assert.deepEqual(filter({ role: 'Reviewer', decisions: false }), ['step-5']);
	assert.deepEqual(filter({ role: '', decisions: true }), ['step-5', 'step-6']);
	assert.deepEqual(threadRoles(thread, (row) => (row.role ? [row.role] : [])), ['Implementer', 'Reviewer']);
});
