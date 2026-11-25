import assert from 'node:assert/strict';
import test from 'node:test';
import { common, createLowlight } from 'lowlight';
import {
	diffOf,
	fileError,
	folderOpen,
	matchFiles,
	nextPage,
	previousPage
} from '../../src/lib/features/assistant/task-files.ts';
import { parseDiff } from '../../src/lib/features/assistant/diff.ts';
import { highlightCap, languageFor, tokenLines } from '../../src/lib/features/assistant/code-highlight.ts';

const entries = [
	{ path: 'BE/internal/handler/agent_task.go', changed: true },
	{ path: 'UI/src/lib/api/agent-tasks.ts', changed: false },
	{ path: 'UI/src/lib/features/assistant/Files.svelte', changed: true },
	{ path: 'README.md', changed: false }
];

test('a filter matches paths case-insensitively and caps the matches', () => {
	assert.deepEqual(matchFiles(entries, ''), { files: entries, count: 4 });
	assert.deepEqual(
		matchFiles(entries, '  agent ').files.map((f) => f.path),
		['BE/internal/handler/agent_task.go', 'UI/src/lib/api/agent-tasks.ts']
	);
	assert.deepEqual(matchFiles(entries, 'FILES').files, [entries[2]]);
	// The query is text, never a pattern.
	assert.deepEqual(matchFiles(entries, '.*').files, []);
	const capped = matchFiles(entries, 'ui/', 1);
	assert.equal(capped.files.length, 1);
	assert.equal(capped.count, 2);
});

test('first-level folders start open, deeper ones closed; a filter opens all; a toggle wins', () => {
	assert.equal(folderOpen('src', 0, {}, false), true);
	assert.equal(folderOpen('src/lib', 1, {}, false), false);
	assert.equal(folderOpen('src/lib', 1, {}, true), true);
	assert.equal(folderOpen('src', 0, { src: false }, false), false);
	assert.equal(folderOpen('src/lib', 1, { 'src/lib': true }, false), true);
	assert.equal(folderOpen('src/lib', 1, { 'src/lib': false }, true), false);
});

test('errors map to what the reader can do about them', () => {
	assert.equal(fileError({ status: 404, error: { code: 'NOT_FOUND' } }), 'denied');
	assert.equal(fileError({ status: 403, error: { code: 'FORBIDDEN' } }), 'denied');
	assert.equal(fileError({ status: 409, error: { code: 'FILES_UNAVAILABLE' } }), 'unavailable');
	assert.equal(fileError({ status: 429, error: { code: 'FILES_BUSY' } }), 'busy');
	// The file or commit moved on since the listing was read.
	assert.equal(fileError({ status: 404, error: { code: 'FILE_NOT_FOUND' } }), 'stale');
	assert.equal(fileError({ status: 400, error: { code: 'INVALID_REF' } }), 'stale');
	assert.equal(fileError({ status: 502, error: { code: 'FILES_FAILED' } }), 'error');
	assert.equal(fileError(new TypeError('Failed to fetch')), 'error');
	assert.equal(fileError(undefined), 'error');
});

test('pages move on from the last line read and back to where the previous page started', () => {
	const page = { start: 1, end: 5000, total_lines: 12000, truncated: true };
	assert.deepEqual(nextPage(page, []), { start: 5001, history: [1] });
	assert.deepEqual(nextPage({ start: 5001, end: 9000, total_lines: 12000, truncated: true }, [1]), {
		start: 9001,
		history: [1, 5001]
	});
	// The last page, or a short file with a cut line, has no next page.
	assert.equal(nextPage({ start: 9001, end: 12000, total_lines: 12000, truncated: false }, [1, 5001]), null);
	assert.equal(nextPage({ start: 1, end: 1, total_lines: 1, truncated: true }, []), null);
	assert.deepEqual(previousPage([1, 5001]), { start: 5001, history: [1] });
	assert.equal(previousPage([]), null);
});

test('a changed file shows its own part of the stored diff', () => {
	const diff = parseDiff([
		'diff --git a/README.md b/README.md',
		'@@ -1 +1 @@',
		'-old',
		'+new',
		'diff --git a/src/old.ts b/src/new.ts',
		'similarity index 90%',
		'rename from src/old.ts',
		'rename to src/new.ts'
	].join('\n'));
	assert.deepEqual(
		diffOf(diff, 'README.md')?.lines.map((line) => line.kind),
		['hunk', 'del', 'add']
	);
	assert.equal(diffOf(diff, 'src/new.ts')?.path, 'src/new.ts');
	assert.equal(diffOf(diff, 'src/other.ts'), undefined);
});

test('a language is chosen by file name, never guessed', () => {
	const lowlight = createLowlight(common);
	assert.equal(languageFor('BE/main.go', lowlight), 'go');
	assert.equal(languageFor('UI/src/App.svelte', lowlight), 'xml');
	assert.equal(languageFor('ui/x.TSX', lowlight), 'typescript');
	assert.equal(languageFor('Makefile', lowlight), 'makefile');
	assert.equal(languageFor('config/app.toml', lowlight), 'ini');
	assert.equal(languageFor('notes.unknownext', lowlight), undefined);
	assert.equal(languageFor('LICENSE', lowlight), undefined);
	assert.ok(highlightCap > 0);
});

test('highlighted code splits into lines, keeping a token that spans lines in both', () => {
	const lowlight = createLowlight(common);
	const lines = tokenLines(lowlight.highlight('typescript', 'const a = 1; /* one\ntwo */\nreturn "x";\n'));
	assert.equal(lines.length, 3);
	assert.deepEqual(
		lines.map((line) => line.map((token) => token.content).join('')),
		['const a = 1; /* one', 'two */', 'return "x";']
	);
	assert.deepEqual(lines[0][0], { content: 'const', class: 'hljs-keyword' });
	assert.equal(lines[1][0].class, 'hljs-comment');
	assert.ok(lines[2].some((token) => token.class === 'hljs-string' && token.content === '"x"'));
	// Plain text between tokens carries no class.
	assert.equal(lines[0][1].class, undefined);
	// A blank line stays a line.
	assert.deepEqual(tokenLines(lowlight.highlight('go', 'a\n\nb')).map((line) => line.length > 0), [true, false, true]);
});
