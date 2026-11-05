import assert from 'node:assert/strict';
import test from 'node:test';
import { parseDiff } from '../../src/lib/features/assistant/diff.ts';

test('parses files, counts and line kinds', () => {
	const files = parseDiff(
		[
			'diff --git a/src/text.js b/src/text.js',
			'index 1..2 100644',
			'--- a/src/text.js',
			'+++ b/src/text.js',
			'@@ -1,2 +1,3 @@',
			' keep',
			'-old',
			'+new',
			'+added',
			'diff --git a/README.md b/README.md',
			'new file mode 100644',
			'@@ -0,0 +1 @@',
			'+# Title',
			''
		].join('\n')
	);
	assert.equal(files.length, 2);
	assert.deepEqual([files[0].path, files[0].additions, files[0].deletions], ['src/text.js', 2, 1]);
	assert.deepEqual(files[0].lines.map((l) => l.kind), ['meta', 'hunk', 'context', 'del', 'add', 'add']);
	assert.deepEqual([files[1].path, files[1].additions], ['README.md', 1]);
});

test('an empty diff has no files', () => {
	assert.deepEqual(parseDiff(''), []);
});

test('totals aggregate across files and handle no diffs', async () => {
	const { diffTotals } = await import('../../src/lib/features/assistant/diff.ts');
	assert.deepEqual(diffTotals([]), { files: 0, additions: 0, deletions: 0 });
	const files = parseDiff(
		['diff --git a/a b/a', '+x', '+y', '-z', 'diff --git a/b b/b', '+q'].join('\n')
	);
	assert.deepEqual(diffTotals(files), { files: 2, additions: 3, deletions: 1 });
});

test('search matches path or line text case-insensitively; blank matches all', async () => {
	const { searchDiff } = await import('../../src/lib/features/assistant/diff.ts');
	const files = parseDiff(
		['diff --git a/src/Foo.ts b/src/Foo.ts', '+hello', 'diff --git a/bar.md b/bar.md', '+world (x)'].join('\n')
	);
	assert.equal(searchDiff(files, '').length, 2);
	assert.equal(searchDiff(files, '  ').length, 2);
	assert.deepEqual(searchDiff(files, 'FOO').map((f) => f.path), ['src/Foo.ts']);
	assert.deepEqual(searchDiff(files, 'world (').map((f) => f.path), ['bar.md']);
	assert.equal(searchDiff(files, 'nope').length, 0);
});
