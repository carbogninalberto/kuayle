import assert from 'node:assert/strict';
import test from 'node:test';
import { wikiFiles, wikiOutline } from '../../src/lib/features/wiki/wiki-content.ts';
import { diffVersions, htmlToTextLines } from '../../src/lib/components/shared/version-diff.ts';

test('the outline lists headings in order with their position among headings', () => {
	const html = '<h1>Setup</h1><p>x</p><h2>Run <code>make&nbsp;dev</code></h2><h3></h3><h2>Q &amp; A</h2>';
	assert.deepEqual(wikiOutline(html), [
		{ level: 1, text: 'Setup', index: 0 },
		{ level: 2, text: 'Run make dev', index: 1 },
		{ level: 2, text: 'Q & A', index: 3 }
	]);
	assert.deepEqual(wikiOutline(''), []);
	assert.deepEqual(wikiOutline(null), []);
});

test('files are the attachments and images the page embeds, each once', () => {
	const html =
		'<p><a class="editor-attachment" data-type="attachment" data-filename="spec.pdf" data-size="2048" href="/api/assets/1">spec.pdf (2 KB)</a></p>' +
		'<p><img src="/api/assets/2/diagram%20v2.png"></p><p><img src="/api/assets/3" alt="Logo"></p>' +
		'<p><a href="https://example.com">not a file</a><img src="/api/assets/2/diagram%20v2.png"></p>';
	assert.deepEqual(wikiFiles(html), [
		{ kind: 'file', href: '/api/assets/1', name: 'spec.pdf', size: 2048 },
		{ kind: 'image', href: '/api/assets/2/diagram%20v2.png', name: 'diagram v2.png', size: 0 },
		{ kind: 'image', href: '/api/assets/3', name: 'Logo', size: 0 }
	]);
});

test('version diffs compare the text of each block, not markup', () => {
	assert.deepEqual(
		htmlToTextLines('<h2>Steps</h2><ul><li><p>Tag</p></li><li><p>Ship &amp; tell</p></li></ul><p></p>'),
		['Steps', '- Tag', '- Ship & tell']
	);
	assert.deepEqual(diffVersions('<p>Keep</p><p>Old</p>', '<p>Keep</p><p><strong>New</strong></p>', true), [
		{ kind: 'same', text: 'Keep' },
		{ kind: 'del', text: 'Old' },
		{ kind: 'add', text: 'New' }
	]);
	// Formatting alone is not a change in the text.
	assert.deepEqual(diffVersions('<p>Same</p>', '<p><em>Same</em></p>', true), [{ kind: 'same', text: 'Same' }]);
	assert.deepEqual(diffVersions('Old title', 'New title', false), [
		{ kind: 'del', text: 'Old title' },
		{ kind: 'add', text: 'New title' }
	]);
});
