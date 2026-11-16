import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	canEditComment,
	commentBodyIsEmpty,
	deleteCommentWithUndo
} from '../../src/lib/features/issues/comment-actions.ts';

test('only the author is offered Edit', () => {
	assert.equal(canEditComment({ user_id: 'u1' }, 'u1'), true);
	assert.equal(canEditComment({ user_id: 'u1' }, 'admin'), false);
	assert.equal(canEditComment({ user_id: 'u1' }, null), false);
	assert.equal(canEditComment({ user_id: '' }, ''), false);
});

test('an emptied editor is not saved', () => {
	assert.equal(commentBodyIsEmpty(''), true);
	assert.equal(commentBodyIsEmpty(' <p></p> '), true);
	assert.equal(commentBodyIsEmpty(undefined), true);
	assert.equal(commentBodyIsEmpty('<p>x</p>'), false);
});

function recorder({ failRemove = false } = {}) {
	const calls = [];
	let undo = null;
	const flow = {
		message: 'Comment deleted',
		remove: async () => {
			calls.push('remove');
			if (failRemove) throw new Error('forbidden');
		},
		restore: async () => calls.push('restore'),
		refresh: () => calls.push('refresh'),
		offerUndo: (message, run) => {
			calls.push(`toast:${message}`);
			undo = run;
		},
		onError: (err) => calls.push(`error:${err.message}`)
	};
	return { flow, calls, undo: () => undo };
}

test('a delete reloads the thread and offers Undo, which restores the comment', async () => {
	const { flow, calls, undo } = recorder();
	assert.equal(await deleteCommentWithUndo(flow), true);
	assert.deepEqual(calls, ['remove', 'refresh', 'toast:Comment deleted']);

	await undo()();
	assert.deepEqual(calls.slice(3), ['restore', 'refresh']);
});

test('a failed delete is explained and offers no Undo', async () => {
	const { flow, calls, undo } = recorder({ failRemove: true });
	assert.equal(await deleteCommentWithUndo(flow), false);
	assert.deepEqual(calls, ['remove', 'error:forbidden']);
	assert.equal(undo(), null);
});
