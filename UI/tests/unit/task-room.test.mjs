import assert from 'node:assert/strict';
import test from 'node:test';
import { mergeRoom, readRoomPages, roomRead, roomRows } from '../../src/lib/features/assistant/task-room.ts';

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
		row.type === 'member' ? [row.type, row.role, row.runId, row.messages.map((m) => m.seq)] : [row.type, row.message.seq]
	);

test('each post of the owner and of Kuayle is a row of its own, in seq order', () => {
	const rows = roomRows([
		kuayle(2, 'phase', { phase: 'implement' }),
		owner(1, 'Keep the old route working.'),
		kuayle(3, 'fact', { fact: 'ci_green', value: true }, 'CI passed')
	]);
	assert.deepEqual(shape(rows), [
		['owner', 1],
		['kuayle', 2],
		['kuayle', 3]
	]);
	assert.deepEqual(
		rows.map((row) => row.key),
		['seq-1', 'seq-2', 'seq-3']
	);
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
		['kuayle', 3],
		['member', 'reviewer', 'run-rev-1', [5, 6]]
	]);
	assert.deepEqual(
		rows.map((row) => row.key),
		['seq-1', 'run-run-impl-1', 'seq-3', 'run-run-rev-1']
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

test('a pipeline run reads every room page after the last contiguous post', async () => {
	let room = [owner(1, 'a'), owner(3, 'arrived live')];
	const afters = [];
	const messages = Array.from({ length: 201 }, (_, i) => owner(i + 2, String(i + 2)));
	await readRoomPages(roomRead(room), async (after) => {
		afters.push(after);
		return messages.filter((message) => message.seq > after).slice(0, 200);
	}, (page) => { room = mergeRoom(room, page); });
	assert.deepEqual(afters, [1, 201]);
	assert.equal(room.length, 202);
	assert.equal(room[2].body, 'arrived live');
});
