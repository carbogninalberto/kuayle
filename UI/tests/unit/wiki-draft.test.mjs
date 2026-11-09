import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeWikiContent } from '../../src/lib/features/wiki/wiki-draft.ts';

test('normalizeWikiContent collapses empty documents', () => {
	assert.equal(normalizeWikiContent('<p></p>'), '');
	assert.equal(normalizeWikiContent('  '), '');
	assert.equal(normalizeWikiContent('<p>Hi</p>'), '<p>Hi</p>');
});
