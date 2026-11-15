import assert from 'node:assert/strict';
import test from 'node:test';
import { isUndoShortcut, undoFailure, UNDO_WINDOW_MS } from '../../src/lib/features/undo/undo-rules.ts';

const key = (overrides = {}) => ({
	key: 'z',
	metaKey: false,
	ctrlKey: true,
	shiftKey: false,
	altKey: false,
	defaultPrevented: false,
	isComposing: false,
	target: { tagName: 'DIV', isContentEditable: false, closest: () => null },
	...overrides
});

test('undo toasts stay for about eight seconds', () => {
	assert.equal(UNDO_WINDOW_MS, 8000);
});

test('Ctrl+Z and Cmd+Z are the undo shortcut', () => {
	assert.equal(isUndoShortcut(key()), true);
	assert.equal(isUndoShortcut(key({ ctrlKey: false, metaKey: true })), true);
	assert.equal(isUndoShortcut(key({ key: 'Z' })), true, 'Caps Lock does not matter');
});

test('other chords and plain Z are not undo', () => {
	assert.equal(isUndoShortcut(key({ ctrlKey: false })), false);
	assert.equal(isUndoShortcut(key({ shiftKey: true })), false, 'Ctrl+Shift+Z is redo');
	assert.equal(isUndoShortcut(key({ altKey: true })), false);
	assert.equal(isUndoShortcut(key({ key: 'y' })), false);
});

test('text fields and editors keep their own undo', () => {
	for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) {
		assert.equal(isUndoShortcut(key({ target: { tagName, isContentEditable: false, closest: () => null } })), false, tagName);
	}
	assert.equal(isUndoShortcut(key({ target: { tagName: 'P', isContentEditable: true, closest: () => null } })), false);
	assert.equal(isUndoShortcut(key({ target: { tagName: 'SPAN', isContentEditable: false, closest: () => ({}) } })), false, 'inside an editor');
	assert.equal(isUndoShortcut(key({ defaultPrevented: true })), false);
	assert.equal(isUndoShortcut(key({ isComposing: true })), false);
});

test('refusals are told apart from other failures', () => {
	assert.equal(undoFailure({ status: 409, error: { code: 'EDITED_MEANWHILE', message: 'x' } }), 'edited');
	assert.equal(undoFailure({ status: 410, error: { code: 'GRACE_EXPIRED', message: 'x' } }), 'expired');
	assert.equal(undoFailure({ status: 409, error: { code: 'UNDO_UNAVAILABLE', message: 'x' } }), 'unavailable');
	assert.equal(undoFailure({ status: 500, error: { code: 'INTERNAL', message: 'x' } }), 'other');
	assert.equal(undoFailure(new Error('offline')), 'other');
});
