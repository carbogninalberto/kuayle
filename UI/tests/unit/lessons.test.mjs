import assert from 'node:assert/strict';
import test from 'node:test';
import { lessonBoard, lessonOrigin, movedLesson, wasEdited } from '../../src/lib/features/memory/lessons.ts';

const lesson = (id, repository, state, over = {}) => ({
	id,
	repository,
	team_id: null,
	state,
	text: `Lesson ${id}`,
	source_event: 'task',
	proposer: 'agent',
	source_task_id: null,
	proposed_by: null,
	decided_by: null,
	decided_at: null,
	position: 0,
	created_at: '2026-11-01T10:00:00Z',
	updated_at: '2026-11-01T10:00:00Z',
	...over
});

test('proposals wait in one place; only approved lessons are listed under their repository, in prompt order', () => {
	const board = lessonBoard(
		[
			lesson('a2', 'acme/api', 'approved', { position: 2 }),
			lesson('p1', 'acme/web', 'proposed'),
			lesson('a1', 'acme/api', 'approved', { position: 1 }),
			lesson('r1', 'acme/api', 'retired', { position: 3 }),
			lesson('d1', 'acme/api', 'dropped'),
			lesson('p2', 'acme/api', 'proposed')
		],
		['acme/web', 'acme/docs']
	);
	assert.deepEqual(
		board.proposals.map((entry) => entry.id),
		['p1', 'p2']
	);
	assert.deepEqual(
		board.repositories.map((group) => [group.repository, group.lessons.map((entry) => entry.id)]),
		[['acme/api', ['a1', 'a2']]]
	);
	// Connected repositories can take a lesson even when they have none.
	assert.deepEqual(board.choices, ['acme/api', 'acme/docs', 'acme/web']);
});

test('a lesson moves one place and never past the ends', () => {
	assert.deepEqual(movedLesson(['a', 'b', 'c'], 'b', -1), ['b', 'a', 'c']);
	assert.deepEqual(movedLesson(['a', 'b', 'c'], 'b', 1), ['a', 'c', 'b']);
	assert.equal(movedLesson(['a', 'b', 'c'], 'a', -1), null);
	assert.equal(movedLesson(['a', 'b', 'c'], 'c', 1), null);
	assert.equal(movedLesson(['a', 'b', 'c'], 'x', 1), null);
});

test('where a lesson came from: an agent, a GitHub review or a person', () => {
	assert.equal(lessonOrigin(lesson('1', 'r', 'proposed')), 'agent');
	assert.equal(lessonOrigin(lesson('2', 'r', 'proposed', { source_event: 'review', proposer: 'agent' })), 'agent');
	assert.equal(lessonOrigin(lesson('3', 'r', 'proposed', { source_event: 'review', proposer: 'external' })), 'github');
	assert.equal(lessonOrigin(lesson('4', 'r', 'proposed', { source_event: 'manual', proposer: 'person' })), 'person');
});

test('an edited lesson is one whose proposed text differs from what agents read', () => {
	assert.equal(wasEdited(lesson('1', 'r', 'approved', { text: 'Run make test', proposed_text: 'run tests' })), true);
	assert.equal(wasEdited(lesson('2', 'r', 'approved', { text: 'Same', proposed_text: 'Same' })), false);
	assert.equal(wasEdited(lesson('3', 'r', 'approved')), false);
});
