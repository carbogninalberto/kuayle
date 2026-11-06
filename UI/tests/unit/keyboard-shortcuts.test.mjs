import assert from 'node:assert/strict';
import test from 'node:test';
import { createKeyboardHandler, createShortcutEngine, isEditableTarget } from '../../src/lib/utils/keyboard.ts';
import { isWorkspaceSettingsPath } from '../../src/lib/utils/workspace-routes.ts';

function element(tagName = 'DIV', { editableAncestor = false, contentEditable = false } = {}) {
	return {
		tagName,
		isContentEditable: contentEditable || editableAncestor,
		closest: (selector) => (editableAncestor && selector.includes('[contenteditable') ? {} : null)
	};
}

function keydown(key, { target = element('BODY'), ...modifiers } = {}) {
	let prevented = false;
	return {
		key,
		target,
		ctrlKey: false,
		metaKey: false,
		altKey: false,
		shiftKey: false,
		isComposing: false,
		defaultPrevented: false,
		...modifiers,
		preventDefault() {
			prevented = true;
		},
		get prevented() {
			return prevented;
		}
	};
}

function engine(createAllowed = () => true) {
	const calls = [];
	const { handler } = createShortcutEngine([
		{ key: 'c', when: createAllowed, handler: () => calls.push('create'), label: 'Create issue', category: 'Actions' },
		{ key: 'k', meta: true, handler: () => calls.push('palette'), label: 'Command palette', category: 'Actions' },
		{ keys: ['g', 's'], handler: () => calls.push('settings'), label: 'Go to settings', category: 'Navigation' }
	]);
	return { handler, calls };
}

test('plain C opens issue creation where creation is allowed', () => {
	const { handler, calls } = engine();
	const event = keydown('c');
	handler(event);
	assert.deepEqual(calls, ['create']);
	assert.equal(event.prevented, true);
});

test('copy and paste chords never trigger the plain C shortcut', () => {
	const { handler, calls } = engine();
	for (const modifiers of [{ ctrlKey: true }, { metaKey: true }, { altKey: true }]) {
		const copy = keydown('c', modifiers);
		handler(copy);
		assert.equal(copy.prevented, false, 'the browser must still copy');
		handler(keydown('v', modifiers));
	}
	assert.deepEqual(calls, []);
});

test('modifier shortcuts still require their modifier', () => {
	const { handler, calls } = engine();
	handler(keydown('k'));
	handler(keydown('k', { metaKey: true }));
	handler(keydown('k', { ctrlKey: true }));
	assert.deepEqual(calls, ['palette', 'palette']);
});

test('a shortcut that is disabled for the current page is ignored without blocking the key', () => {
	let onSettings = true;
	const { handler, calls } = engine(() => !onSettings);
	const event = keydown('c');
	handler(event);
	assert.deepEqual(calls, []);
	assert.equal(event.prevented, false);

	onSettings = false;
	handler(keydown('c'));
	assert.deepEqual(calls, ['create']);
});

test('typing or pasting into editable controls never triggers shortcuts', () => {
	const { handler, calls } = engine();
	const targets = [
		element('INPUT'),
		element('TEXTAREA'),
		element('SELECT'),
		element('DIV', { contentEditable: true }),
		element('SPAN', { editableAncestor: true })
	];
	for (const target of targets) {
		handler(keydown('c', { target }));
		handler(keydown('v', { target, ctrlKey: true }));
		handler(keydown('g', { target }));
		handler(keydown('s', { target }));
	}
	assert.deepEqual(calls, []);
});

test('composition and already handled keys are left alone', () => {
	const { handler, calls } = engine();
	handler(keydown('c', { isComposing: true }));
	handler(keydown('c', { defaultPrevented: true }));
	assert.deepEqual(calls, []);
});

test('sequences keep working outside editable controls', () => {
	const { handler, calls } = engine();
	handler(keydown('g'));
	handler(keydown('s'));
	assert.deepEqual(calls, ['settings']);
});

test('page shortcut handlers share the same editable and modifier rules', () => {
	const calls = [];
	const handler = createKeyboardHandler([
		{ key: 'x', handler: () => calls.push('toggle') },
		{ key: 'a', ctrl: true, handler: () => calls.push('select-all') }
	]);
	handler(keydown('x', { ctrlKey: true }));
	handler(keydown('x', { target: element('INPUT') }));
	handler(keydown('a'));
	assert.deepEqual(calls, []);
	handler(keydown('x'));
	handler(keydown('a', { ctrlKey: true }));
	assert.deepEqual(calls, ['toggle', 'select-all']);
});

test('isEditableTarget tolerates non-element targets', () => {
	assert.equal(isEditableTarget(null), false);
	assert.equal(isEditableTarget({}), false);
	assert.equal(isEditableTarget(element('BUTTON')), false);
});

test('only workspace settings routes count as settings pages', () => {
	assert.equal(isWorkspaceSettingsPath('/acme/settings'), true);
	assert.equal(isWorkspaceSettingsPath('/acme/settings/'), true);
	assert.equal(isWorkspaceSettingsPath('/acme/settings/members'), true);
	assert.equal(isWorkspaceSettingsPath('/acme/my-issues'), false);
	assert.equal(isWorkspaceSettingsPath('/acme/teams/t1'), false);
	assert.equal(isWorkspaceSettingsPath('/acme/issue/ENG-1'), false);
	assert.equal(isWorkspaceSettingsPath('/acme/views/settings-review'), false);
	assert.equal(isWorkspaceSettingsPath('/settings'), false);
});

test('symbols typed with AltGr (reported as Ctrl+Alt) still trigger symbol shortcuts', () => {
	const calls = [];
	const { handler } = createShortcutEngine([
		{ key: 'c', handler: () => calls.push('create'), label: 'Create issue', category: 'Actions' },
		{ key: '/', handler: () => calls.push('search'), label: 'Search', category: 'Actions' },
		{ key: '?', shift: true, handler: () => calls.push('help'), label: 'Help', category: 'Help' }
	]);
	handler(keydown('/', { ctrlKey: true, altKey: true }));
	handler(keydown('?', { ctrlKey: true, altKey: true, shiftKey: true }));
	handler(keydown('/', { altKey: true }));
	assert.deepEqual(calls, ['search', 'help', 'search']);

	// Real Ctrl or Cmd chords on symbols and AltGr letters stay with the browser.
	handler(keydown('/', { ctrlKey: true }));
	handler(keydown('/', { metaKey: true }));
	handler(keydown('c', { ctrlKey: true, altKey: true }));
	assert.deepEqual(calls, ['search', 'help', 'search']);
});
