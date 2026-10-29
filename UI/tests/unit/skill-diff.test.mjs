import { test } from 'node:test';
import assert from 'node:assert/strict';
import { diffLines } from '../../src/lib/features/assistant/skill-diff.ts';

test('marks added and removed lines around shared ones', () => {
	assert.deepEqual(diffLines('a\nb\nc', 'a\nx\nc\nd'), [
		{ kind: 'same', text: 'a' },
		{ kind: 'del', text: 'b' },
		{ kind: 'add', text: 'x' },
		{ kind: 'same', text: 'c' },
		{ kind: 'add', text: 'd' }
	]);
});

test('an empty old text adds every line', () => {
	assert.deepEqual(diffLines('', 'one\ntwo'), [
		{ kind: 'add', text: 'one' },
		{ kind: 'add', text: 'two' }
	]);
});

test('identical texts have no changes', () => {
	assert.ok(diffLines('same\ntext', 'same\ntext').every((line) => line.kind === 'same'));
});

test('very long texts fall back to removing the old and adding the new', () => {
	const old = Array.from({ length: 3000 }, (_, i) => `old ${i}`).join('\n');
	const next = Array.from({ length: 3000 }, (_, i) => `new ${i}`).join('\n');
	const lines = diffLines(old, next);
	assert.equal(lines.length, 6000);
	assert.equal(lines[0].kind, 'del');
	assert.equal(lines.at(-1).kind, 'add');
});
