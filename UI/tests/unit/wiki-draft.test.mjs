import assert from 'node:assert/strict';
import test from 'node:test';
import { draftChanged, normalizeWikiContent } from '../../src/lib/features/wiki/wiki-draft.ts';

test('an untouched draft is not a change, even after the editor normalizes it', () => {
	const saved = { title: 'Runbook', content: '' };
	assert.equal(draftChanged(saved, { title: 'Runbook', content: '' }), false);
	// The editor reports an empty document as an empty paragraph.
	assert.equal(draftChanged(saved, { title: 'Runbook', content: '<p></p>' }), false);
	assert.equal(draftChanged({ title: 'A', content: '<p>x</p>' }, { title: 'A', content: ' <p>x</p>\n' }), false);
});

test('edited title or content is an unsaved change', () => {
	const saved = { title: 'Runbook', content: '<p>v1</p>' };
	assert.equal(draftChanged(saved, { title: 'Runbook v2', content: '<p>v1</p>' }), true);
	assert.equal(draftChanged(saved, { title: 'Runbook', content: '<p>v2</p>' }), true);
	assert.equal(draftChanged(saved, { title: 'Runbook', content: '<p></p>' }), true, 'clearing content counts');
});

test('a new page with only whitespace typed is not worth a warning', () => {
	const blank = { title: '', content: '' };
	assert.equal(draftChanged(blank, { title: '   ', content: '<p></p>' }), false);
	assert.equal(draftChanged(blank, { title: 'Draft', content: '' }), true);
});

test('normalizeWikiContent collapses empty documents', () => {
	assert.equal(normalizeWikiContent('<p></p>'), '');
	assert.equal(normalizeWikiContent('  '), '');
	assert.equal(normalizeWikiContent('<p>Hi</p>'), '<p>Hi</p>');
});
