import assert from 'node:assert/strict';
import test from 'node:test';
import { pipelineChecks } from '../../src/lib/features/assistant/task-checks.ts';

// The Checks tab of a pipeline task (KUA-286): each check of each phase,
// with its state, Kuayle's reason and since when, read from what the room
// says (Kuayle posts every fact when it changes, the roles their verdicts,
// the owner their waivers). Nothing is guessed: what the room does not say
// is not known yet.
const at = (seq) => `2026-11-25T10:${String(seq).padStart(2, '0')}:00Z`;
const HEAD = 'abc1234def5678abc1234def5678abc1234def56';
const OLD = '0011223344556677889900112233445566778899';
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
const fact = (seq, name, value, body = '') => post(seq, 'kuayle', 'fact', body, { data: { fact: name, value } });
const verdict = (seq, role, value, commit, extra = {}) =>
	post(seq, 'member', 'verdict', '', { author_role: role, run_id: `run-${seq}`, data: { verdict: value, commit, role }, ...extra });
const waiver = (seq, check, extra = {}) => post(seq, 'human', 'waiver', '', { data: { check }, ...extra });

const pipeline = {
	kind: 'pipeline',
	phases: [
		{ id: 'implement', label: 'Implement', owner: 'implementer', checks: [{ id: 'pr', fact: 'pr_exists', required: true }] },
		{
			id: 'integrate',
			label: 'Integrate',
			owner: 'kuayle',
			checks: [
				{ id: 'up-to-date', fact: 'head_contains_base', required: true },
				{ id: 'ci', fact: 'ci_green', required: true },
				{ id: 'migrations', fact: 'migration_collision' }
			]
		},
		{
			id: 'review',
			label: 'Review',
			owner: 'implementer',
			checks: [
				{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true },
				{ id: 'security', fact: 'verdict', role: 'security', required: true },
				{ id: 'lint', fact: 'script', script: 'make lint', required: true },
				{ id: 'others', fact: 'overlap' }
			]
		},
		{ id: 'approval', label: 'Approval', owner: 'human', checks: [{ id: 'owner', fact: 'approved', required: true }] },
		{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
	],
	roster: [
		{ role: 'implementer', profile_id: 'p-impl', writer: true },
		{ role: 'reviewer', profile_id: 'p-rev' },
		{ role: 'security', profile_id: 'p-sec' }
	]
};
const byId = (result) => Object.fromEntries(result.rows.map((row) => [row.check.id, row]));
const brief = (row) => [row.state, row.reason, row.note ?? null, row.since ?? null];

test('before Kuayle read anything, every check is not known yet, in phase order', () => {
	const result = pipelineChecks(pipeline, [], 'implement', false);
	assert.equal(result.head, '');
	assert.deepEqual(
		result.rows.map((row) => [row.phase.id, row.check.id, row.state]),
		[
			['implement', 'pr', 'unknown'],
			['integrate', 'up-to-date', 'unknown'],
			['integrate', 'ci', 'unknown'],
			['integrate', 'migrations', 'unknown'],
			['review', 'reviewer', 'unknown'],
			['review', 'security', 'unknown'],
			['review', 'lint', 'unknown'],
			['review', 'others', 'unknown'],
			['approval', 'owner', 'unknown']
		]
	);
});

test('without a pull request every check waits on it, with what Kuayle said and since when', () => {
	const why = 'No pull request is published for this task yet. Next step: commit the work so Kuayle publishes it.';
	const checks = byId(pipelineChecks(pipeline, [fact(1, 'pr_exists', false, why)], 'implement', false));
	assert.deepEqual(brief(checks.pr), ['unmet', why, null, at(1)]);
	assert.deepEqual(brief(checks.ci), ['unmet', why, null, at(1)]);
	assert.deepEqual(brief(checks.reviewer), ['unmet', why, null, at(1)]);
});

test('facts, CI, verdicts, scripts and overlap each say how they stand for the head', () => {
	const room = [
		fact(1, 'pr_exists', true),
		fact(2, 'head_sha', OLD),
		fact(3, 'head_contains_base', false, 'Pull request #4 is 2 commit(s) behind main.'),
		fact(4, 'ci_green', { state: 'pending', head: OLD, failing: [] }, '1 of 3 check(s) are still running.'),
		verdict(5, 'reviewer', 'approve', OLD.slice(0, 7)),
		fact(6, 'head_sha', HEAD),
		fact(7, 'head_contains_base', true),
		fact(8, 'ci_green', { state: 'failing', head: HEAD, failing: ['lint'] }, '1 check(s) failed on abc1234: lint.'),
		verdict(9, 'security', 'request_changes', HEAD.slice(0, 12)),
		fact(10, 'migration_collision', false),
		post(11, 'kuayle', 'fact', 'Check lint passed on 0011223.', { data: { fact: 'script', value: { check: 'lint', commit: OLD, exit: 0 } } }),
		fact(12, 'overlap', ['KUA-7']),
		// A refused verdict says nothing.
		verdict(13, 'security', 'approve', HEAD, { reject_reason: 'the commit is not on the task branch' })
	];
	const result = pipelineChecks(pipeline, room, 'integrate', false);
	assert.equal(result.head, HEAD);
	const checks = byId(result);
	// The phase before the current one passed.
	assert.deepEqual(brief(checks.pr), ['met', '', null, at(1)]);
	assert.deepEqual(brief(checks['up-to-date']), ['met', '', null, at(7)]);
	assert.deepEqual(brief(checks.ci), ['unmet', '1 check(s) failed on abc1234: lint.', null, at(8)]);
	assert.deepEqual(brief(checks.migrations), ['met', '', null, at(10)]);
	// The reviewer approved the earlier head, the security role asked for changes on this one.
	assert.deepEqual(brief(checks.reviewer), ['unmet', '', { kind: 'old_verdict', role: 'reviewer', commit: '0011223' }, at(6)]);
	assert.deepEqual(brief(checks.security), ['unmet', '', { kind: 'not_approved', role: 'security', verdict: 'request_changes' }, at(9)]);
	// The script ran on the earlier head: it runs again on this one.
	assert.deepEqual(brief(checks.lint), ['waiting', '', null, at(6)]);
	assert.deepEqual(brief(checks.others), ['waiting', '', { kind: 'merges_first', issues: 'KUA-7' }, at(12)]);
	assert.deepEqual(brief(checks.owner), ['unknown', '', null, null]);

	// CI running on the head waits; CI read for another head is not known.
	const running = byId(pipelineChecks(pipeline, [...room, fact(14, 'ci_green', { state: 'pending', head: HEAD, failing: [] }, 'running')], 'integrate', false));
	assert.deepEqual(brief(running.ci), ['waiting', 'running', null, at(14)]);
	const stale = byId(pipelineChecks(pipeline, [...room, fact(14, 'ci_green', { state: 'passing', head: OLD, failing: [] })], 'integrate', false));
	assert.equal(stale.ci.state, 'unknown');
	const missing = byId(pipelineChecks(pipeline, room.slice(0, 8), 'integrate', false));
	assert.deepEqual(brief(missing.security), ['unmet', '', { kind: 'no_verdict', role: 'security' }, at(6)]);
});

test("a waiver holds for the head it was given on, and only the owner's", () => {
	const room = [
		fact(1, 'pr_exists', true),
		fact(2, 'head_sha', OLD),
		waiver(3, 'ci'),
		fact(4, 'ci_green', { state: 'failing', head: OLD, failing: ['flaky'] }, 'flaky failed'),
		post(5, 'member', 'waiver', '', { author_role: 'lead', data: { check: 'reviewer' } }),
		waiver(6, 'migrations'),
		waiver(7, 'up-to-date', { reject_reason: 'nope' })
	];
	const checks = byId(pipelineChecks(pipeline, room, 'integrate', false));
	assert.deepEqual(brief(checks.ci), ['waived', 'flaky failed', { kind: 'waived', commit: '0011223' }, at(3)]);
	// An advisory check is never waived, and a lead's waiver is not the owner's.
	assert.notEqual(checks.migrations.state, 'waived');
	assert.notEqual(checks.reviewer.state, 'waived');
	assert.notEqual(checks['up-to-date'].state, 'waived');
	// A new head needs the check again.
	const moved = byId(pipelineChecks(pipeline, [...room, fact(8, 'head_sha', HEAD)], 'integrate', false));
	assert.notEqual(moved.ci.state, 'waived');
});

test('the approval and a passing script on the head are met; a done task met every required check', () => {
	const room = [
		fact(1, 'pr_exists', true),
		fact(2, 'head_sha', HEAD),
		post(3, 'kuayle', 'fact', 'Check lint exited 2 on abc1234.', { data: { fact: 'script', value: { check: 'lint', commit: HEAD, exit: 2 } } }),
		fact(4, 'approved', false, 'Nobody has approved merging abc1234 yet.')
	];
	let checks = byId(pipelineChecks(pipeline, room, 'review', false));
	assert.deepEqual(brief(checks.lint), ['unmet', 'Check lint exited 2 on abc1234.', null, at(3)]);
	assert.deepEqual(brief(checks.owner), ['unmet', 'Nobody has approved merging abc1234 yet.', null, at(4)]);
	const passed = [
		...room,
		post(5, 'kuayle', 'fact', 'Check lint passed on abc1234.', { data: { fact: 'script', value: { check: 'lint', commit: HEAD, exit: 0 } } }),
		fact(6, 'approved', true)
	];
	checks = byId(pipelineChecks(pipeline, passed, 'approval', false));
	assert.deepEqual(brief(checks.lint), ['met', '', null, at(5)]);
	assert.deepEqual(brief(checks.owner), ['met', '', null, at(6)]);
	// Merged: every phase passed, whatever the room last said.
	const done = byId(pipelineChecks(pipeline, room, 'merge', true));
	assert.equal(done.lint.state, 'met');
	assert.equal(done.security.state, 'met');
	// An advisory check is not claimed met by a passed phase.
	assert.equal(done.others.state, 'unknown');
});

test('with a lead, a phase before the current one is not claimed passed', () => {
	const led = { ...pipeline, lead: { profile_id: 'p-lead' } };
	const checks = byId(pipelineChecks(led, [], 'review', false));
	assert.equal(checks.pr.state, 'unknown');
});

test('new heads invalidate earlier results at the head change, and later verdicts keep their own date', () => {
	const room = [
		fact(1, 'pr_exists', true),
		fact(2, 'head_sha', OLD),
		fact(3, 'ci_green', { state: 'passing', head: OLD }),
		verdict(4, 'reviewer', 'approve', OLD),
		fact(5, 'head_sha', HEAD),
		verdict(6, 'security', 'approve', OLD)
	];
	const checks = byId(pipelineChecks(pipeline, room, 'integrate', false));
	assert.equal(checks.ci.since, at(5));
	assert.equal(checks.reviewer.since, at(5));
	assert.equal(checks.security.since, at(6));
});

test('a head without CI is met when Kuayle reported no blocking reason, and waits when it gave one', () => {
	const start = [fact(1, 'pr_exists', true), fact(2, 'head_sha', HEAD)];
	const noCI = { head: HEAD, state: 'none', failing: [] };
	const first = fact(3, 'ci_green', noCI, 'Waiting for a check to register.');
	const later = fact(4, 'ci_green', noCI);
	assert.deepEqual(brief(byId(pipelineChecks(pipeline, [...start, first], 'integrate', false)).ci),
		['waiting', first.body, null, at(3)]);
	assert.deepEqual(brief(byId(pipelineChecks(pipeline, [...start, later], 'integrate', false)).ci),
		['met', '', null, at(4)]);
});
