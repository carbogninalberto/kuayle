import assert from 'node:assert/strict';
import test from 'node:test';
import { linkIssueKeys, registryEntries, wikiFiles, wikiOutline } from '../../src/lib/features/wiki/wiki-content.ts';
import {
	diffRows,
	diffVersions,
	foldRows,
	htmlToTextLines,
	splitPairs
} from '../../src/lib/components/shared/version-diff.ts';

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

test('a changed line is paired with its replacement and only the differing words are marked', () => {
	const rows = diffRows(
		'<p>Intro</p><p>Deploy on Friday at noon</p>',
		'<p>Intro</p><p>Deploy on Monday at noon</p><p>New</p>',
		true
	);
	assert.deepEqual(
		rows.map((r) => [r.kind, r.oldNo, r.newNo]),
		[
			['same', 1, 1],
			['del', 2, undefined],
			['add', undefined, 2],
			['add', undefined, 3]
		]
	);
	assert.deepEqual(rows[1].segments, [
		{ text: 'Deploy on ', changed: false },
		{ text: 'Friday', changed: true },
		{ text: ' at noon', changed: false }
	]);
	assert.deepEqual(rows[2].segments, [
		{ text: 'Deploy on ', changed: false },
		{ text: 'Monday', changed: true },
		{ text: ' at noon', changed: false }
	]);
	assert.equal(rows[3].segments, undefined, 'an added line without a counterpart is wholly new');
});

test('long unchanged runs fold, keeping three lines of context around changes', () => {
	const lines = Array.from({ length: 12 }, (_, i) => `line ${i + 1}`);
	const after = [...lines];
	after[9] = 'line ten';
	const blocks = foldRows(diffRows(lines.join('\n'), after.join('\n'), false));
	assert.deepEqual(
		blocks.map((b) => [b.kind, b.rows.map((r) => r.text)]),
		[
			['folded', ['line 1', 'line 2', 'line 3', 'line 4', 'line 5', 'line 6']],
			['rows', ['line 7', 'line 8', 'line 9', 'line 10', 'line ten', 'line 11', 'line 12']]
		]
	);
	// Short unchanged runs between changes stay visible.
	assert.deepEqual(
		foldRows(diffRows('a\nb\nc', 'A\nb\nC', false)).map((b) => b.kind),
		['rows']
	);
});

test('side-by-side pairs removed lines with the lines that replaced them', () => {
	const pairs = splitPairs(diffRows('a\nb\nc', 'a\nB\nc\nd', false));
	assert.deepEqual(
		pairs.map((p) => [p.left?.text ?? null, p.right?.text ?? null]),
		[
			['a', 'a'],
			['b', 'B'],
			['c', 'c'],
			[null, 'd']
		]
	);
});

test('registry days read as timed entries, from paragraphs or list items', () => {
	assert.deepEqual(registryEntries('<p><strong>9:05</strong> Opened PR #161</p><p></p><p>No time here</p>'), [
		{ time: '09:05', html: 'Opened <span class="registry-ref" data-kind="pull-request">PR #161</span>' },
		{ time: null, html: 'No time here' }
	]);
	assert.deepEqual(registryEntries('<ul><li><p>14:30 — Deployed</p></li></ul>'), [{ time: '14:30', html: 'Deployed' }]);
	assert.deepEqual(registryEntries(''), []);
});

test('plain issue keys become issue mentions, but not inside links, code or mentions', () => {
	const mention = (key) =>
		`<span data-type="mention" data-kind="issue" data-id="" data-label="${key}" data-identifier="${key}" class="mention mention-issue">${key}</span>`;
	assert.equal(
		linkIssueKeys('Task KUA-20 done, see ENG-3.'),
		`Task ${mention('KUA-20')} done, see ${mention('ENG-3')}.`
	);
	assert.equal(
		linkIssueKeys('<a href="/x">KUA-20</a> <code>KUA-21</code>'),
		'<a href="/x">KUA-20</a> <code>KUA-21</code>'
	);
	const existing = '<span data-type="mention" data-kind="issue" data-identifier="KUA-20">KUA-20</span> then KUA-22';
	assert.equal(linkIssueKeys(existing), existing.replace('then KUA-22', `then ${mention('KUA-22')}`));
	assert.equal(linkIssueKeys('utf-8 and A-1 stay text'), 'utf-8 and A-1 stay text');
	assert.equal(linkIssueKeys('Opened PR #161'), 'Opened <span class="registry-ref" data-kind="pull-request">PR #161</span>');
});
