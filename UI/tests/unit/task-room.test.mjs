import assert from 'node:assert/strict';
import test from 'node:test';
import { factLabel, foldText, mergeRoom, roomRead, roomRows, rowRoles } from '../../src/lib/features/assistant/task-room.ts';

// The room of a pipeline task as rows: what the owner, Kuayle and each
// role's turn posted, in the room's order.
const at = (seq) => `2026-11-24T10:${String(seq).padStart(2, '0')}:00Z`;
const post = (seq, author_kind, kind, body = '', extra = {}) => ({
	id: `message-${seq}`,
	task_id: 'task-1',
	seq,
	author_kind,
	author_role: null,
	author_user_id: author_kind === 'human' ? 'user-1' : null,
	author_member_id: null,
	run_id: null,
	to_roles: [],
	kind,
	body,
	data: {},
	reply_to: null,
	reject_reason: null,
	created_at: at(seq),
	...extra
});
const owner = (seq, body, extra) => post(seq, 'human', 'say', body, extra);
const kuayle = (seq, kind, data, body = '') => post(seq, 'kuayle', kind, body, { data });
const member = (seq, role, run, kind, body, extra = {}) =>
	post(seq, 'member', kind, body, { author_role: role, author_member_id: `member-${role}`, run_id: run, ...extra });
const shape = (rows) =>
	rows.map((row) =>
		row.type === 'member'
			? [row.type, row.role, row.runId, row.messages.map((m) => m.seq)]
			: row.type === 'facts'
				? [row.type, row.messages.map((m) => m.seq)]
				: [row.type, row.message.seq]
	);

test('each post of the owner and each phase move of Kuayle is a row of its own, in seq order', () => {
	const rows = roomRows([
		kuayle(2, 'phase', { phase: 'implement' }),
		owner(1, 'Keep the old route working.'),
		kuayle(3, 'fact', { fact: 'ci_green', value: { state: 'passing' } }, 'CI passed')
	]);
	assert.deepEqual(shape(rows), [
		['owner', 1],
		['kuayle', 2],
		['facts', [3]]
	]);
	assert.deepEqual(
		rows.map((row) => row.key),
		['seq-1', 'seq-2', 'facts-3']
	);
});

test("Kuayle's facts in a row are one row, until something else is said", () => {
	const rows = roomRows([
		kuayle(1, 'fact', { fact: 'pr_exists', value: true }),
		kuayle(2, 'fact', { fact: 'head_sha', value: 'abc1234def' }),
		kuayle(3, 'fact', { fact: 'ci_green', value: { state: 'pending', head: 'abc1234def', failing: [] } }, '2 of 3 check(s) are still running'),
		kuayle(4, 'phase', { phase: 'review' }),
		kuayle(5, 'fact', { fact: 'ci_green', value: { state: 'passing', head: 'abc1234def', failing: [] } }),
		member(6, 'reviewer', 'run-rev-1', 'say', 'Looking.'),
		kuayle(7, 'fact', { fact: 'conflict', value: true }, 'Pull request #4 conflicts with main.'),
		// What Kuayle tells the owner is not folded away, nor an approval.
		post(8, 'kuayle', 'fact', 'Check lint failed.', { data: { fact: 'script', value: { check: 'lint', exit: 1 } }, to_roles: ['human'] }),
		kuayle(9, 'fact', { fact: 'approved', value: true }),
		kuayle(10, 'fact', { fact: 'approved', value: false }),
		kuayle(11, 'summary', {}, 'Nobody is working, and the phase Review waits on: ...')
	]);
	assert.deepEqual(shape(rows), [
		['facts', [1, 2, 3]],
		['kuayle', 4],
		['facts', [5]],
		['member', 'reviewer', 'run-rev-1', [6]],
		['facts', [7]],
		['kuayle', 8],
		['kuayle', 9],
		['facts', [10]],
		['kuayle', 11]
	]);
	// A phase move and an approval are decisions; facts and summaries are not.
	assert.deepEqual(
		rows.map((row) => !!row.decision),
		[false, true, false, false, false, false, true, false, false]
	);
});

// English words for the labels, as the view says them.
const words = (label) =>
	({
		pr_opened: 'Pull request opened',
		pr_open: 'Pull request open',
		head: `Head ${label.commit}`,
		base: `Base ${label.branch}`,
		ci_running: 'CI running',
		ci_passed: 'CI passed',
		ci_failed: 'CI failed',
		conflict: 'Conflicts with the base',
		no_conflict: 'No conflicts',
		overlap: `${label.issues} merge first`,
		script_failed: `Check ${label.check} failed`,
		text: label.text
	})[label.kind] ?? label.kind;
const then = (first, next) => `${first}, then ${next}`;

test('a fact says what it means in a few words; one Kuayle does not know keeps its text', () => {
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'head_sha', value: 'abc1234def5678' })), { kind: 'head', commit: 'abc1234' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'ci_green', value: { state: 'failing' } })), { kind: 'ci_failed' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'ci_green', value: { state: 'none' } })), { kind: 'ci_waiting' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'ci_green', value: { state: 'incomplete' } })), { kind: 'ci_unknown' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'overlap', value: ['KUA-1', 'KUA-2'] })), { kind: 'overlap', issues: 'KUA-1, KUA-2' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'overlap', value: [] })), { kind: 'no_overlap' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'merged', value: false })), { kind: 'pr_open' });
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'script', value: { check: 'lint', exit: 0 } })), { kind: 'script_passed', check: 'lint' });
	assert.deepEqual(
		factLabel(kuayle(1, 'fact', { fact: 'turn', value: { role: 'reviewer' } }, "The @reviewer's turn failed: out of memory\nmore")),
		{ kind: 'text', text: "The @reviewer's turn failed: out of memory" }
	);
	assert.deepEqual(factLabel(kuayle(1, 'fact', { fact: 'mystery', value: 1 })), { kind: 'text', text: 'mystery' });
});

test('a folded row reads as one line: the same fact running on, then where it ended', () => {
	const ci = (seq, state) => kuayle(seq, 'fact', { fact: 'ci_green', value: { state } });
	assert.equal(foldText([ci(1, 'pending'), ci(2, 'passing')], words, then), 'CI running, then CI passed');
	// Only where a fact started and ended within the fold.
	assert.equal(foldText([ci(1, 'pending'), ci(2, 'failing'), ci(3, 'pending'), ci(4, 'passing')], words, then), 'CI running, then CI passed');
	assert.equal(
		foldText(
			[
				kuayle(1, 'fact', { fact: 'pr_exists', value: true }),
				kuayle(2, 'fact', { fact: 'head_sha', value: 'abc1234def' }),
				ci(3, 'pending'),
				kuayle(4, 'fact', { fact: 'conflict', value: false }),
				ci(5, 'passing'),
				kuayle(6, 'fact', { fact: 'merged', value: false }),
				kuayle(7, 'fact', { fact: 'closed', value: false })
			],
			words,
			then
		),
		'Pull request opened · Head abc1234 · CI running · No conflicts · CI passed · Pull request open'
	);
});

test('a row concerns the role that wrote it or the roles it is addressed to', () => {
	const rows = roomRows([
		owner(1, 'All of you.'),
		owner(2, 'Reviewer, be strict.', { to_roles: ['reviewer'] }),
		member(3, 'implementer', 'run-1', 'say', 'Done.'),
		post(4, 'kuayle', 'fact', 'CI failed.', { data: { fact: 'ci_green', value: { state: 'failing' } }, to_roles: ['implementer'] })
	]);
	assert.deepEqual(rows.map(rowRoles), [[], ['reviewer'], ['implementer'], ['implementer']]);
});

test("an owner's waiver or grant is a decision; a turn's verdict or phase move is, and so is its question flagged", () => {
	const rows = roomRows([
		owner(1, 'Go on.'),
		post(2, 'human', 'waiver', '', { data: { check: 'ci' } }),
		post(3, 'human', 'grant', '', { data: { check: 'reviewer' } }),
		member(4, 'implementer', 'run-1', 'ask', 'Which route?', { to_roles: ['human'] }),
		member(5, 'reviewer', 'run-2', 'verdict', 'Rename q.', { data: { verdict: 'request_changes', commit: 'abc1234' } }),
		member(6, 'lead', 'run-3', 'phase', 'Back to implement.', { data: { phase: 'implement' } }),
		member(7, 'implementer', 'run-4', 'verdict', 'Mine.', { reject_reason: 'the implementer may not post verdict' })
	]);
	assert.deepEqual(
		rows.map((row) => !!row.decision),
		[false, true, true, false, true, true, false]
	);
	assert.deepEqual([rows[3].asks, rows[4].verdict, rows[6].verdict], [true, 'request_changes', undefined]);
});

test("a role's turn is one row, where its first post is, with every post of the turn", () => {
	const rows = roomRows([
		owner(1, 'Add search.'),
		member(2, 'implementer', 'run-impl-1', 'ask', 'Case-insensitive?', { to_roles: ['reviewer'] }),
		kuayle(3, 'fact', { fact: 'pr_exists', value: true }),
		member(4, 'implementer', 'run-impl-1', 'say', 'Pushed the first version.'),
		member(5, 'reviewer', 'run-rev-1', 'say', 'Yes, case-insensitive.'),
		member(6, 'reviewer', 'run-rev-1', 'verdict', 'Looks good.', {
			data: { verdict: 'approve', commit: 'abc1234', role: 'reviewer' }
		})
	]);
	assert.deepEqual(shape(rows), [
		['owner', 1],
		['member', 'implementer', 'run-impl-1', [2, 4]],
		['facts', [3]],
		['member', 'reviewer', 'run-rev-1', [5, 6]]
	]);
	assert.deepEqual(
		rows.map((row) => row.key),
		['seq-1', 'run-run-impl-1', 'facts-3', 'run-run-rev-1']
	);
});

test('two turns of a role are two rows; a post of a role outside a turn stands alone', () => {
	const rows = roomRows([
		member(1, 'implementer', 'run-impl-1', 'say', 'First pass done.'),
		member(2, 'reviewer', 'run-rev-1', 'verdict', 'Rename q.', {
			data: { verdict: 'request_changes', commit: 'abc1234', role: 'reviewer' }
		}),
		member(3, 'implementer', 'run-impl-2', 'say', 'Renamed.'),
		member(4, 'lead', null, 'say', 'Moving on.')
	]);
	assert.deepEqual(shape(rows), [
		['member', 'implementer', 'run-impl-1', [1]],
		['member', 'reviewer', 'run-rev-1', [2]],
		['member', 'implementer', 'run-impl-2', [3]],
		['member', 'lead', null, [4]]
	]);
	assert.equal(rows[3].key, 'seq-4');
});

test('a refused post keeps its place and its reason', () => {
	const refused = member(2, 'implementer', 'run-impl-1', 'verdict', 'I approve myself.', {
		reject_reason: 'the implementer may not post verdict',
		data: {}
	});
	const rows = roomRows([owner(1, 'Go.'), refused, member(3, 'implementer', 'run-impl-1', 'say', 'Done.')]);
	assert.deepEqual(shape(rows), [
		['owner', 1],
		['member', 'implementer', 'run-impl-1', [2, 3]]
	]);
	assert.equal(rows[1].messages[0].reject_reason, 'the implementer may not post verdict');
});

test('the room merges what was read and what arrived live once per seq, oldest first', () => {
	const read = [owner(1, 'Add search.'), kuayle(2, 'phase', { phase: 'implement' })];
	const live = [kuayle(2, 'phase', { phase: 'implement' }), owner(4, 'Also the docs.'), owner(3, 'Keep the route.')];
	const merged = mergeRoom(read, live);
	assert.deepEqual(
		merged.map((message) => message.seq),
		[1, 2, 3, 4]
	);
	// The copy already held is kept.
	assert.equal(merged[1], read[1]);
	// Nothing new is the same room.
	assert.equal(mergeRoom(merged, [owner(3, 'Keep the route.')]), merged);
	assert.deepEqual(mergeRoom(undefined, [owner(1, 'Hi')]).map((message) => message.seq), [1]);
});

test('a read continues after the unbroken run of posts from the first', () => {
	assert.equal(roomRead([]), 0);
	assert.equal(roomRead([owner(1, 'a'), owner(2, 'b'), owner(3, 'c')]), 3);
	// Seq 3 was missed (an event lost): the next read starts after 2.
	assert.equal(roomRead([owner(1, 'a'), owner(2, 'b'), owner(4, 'd')]), 2);
	assert.equal(roomRead([owner(2, 'b')]), 0);
});
