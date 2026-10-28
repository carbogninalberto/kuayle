import assert from 'node:assert/strict';
import test from 'node:test';
import {
	TERMINAL_FONT_FACES,
	TERMINAL_OPTIONS,
	TERMINAL_UNICODE_VERSION
} from '../../src/lib/features/dev-machines/terminal-options.ts';

test('uses raw PTY newlines and Unicode 11 for full-screen terminal applications', () => {
	assert.equal(TERMINAL_OPTIONS.convertEol, false);
	assert.equal(TERMINAL_OPTIONS.allowProposedApi, true);
	assert.equal(TERMINAL_OPTIONS.lineHeight, 1);
	assert.equal(TERMINAL_UNICODE_VERSION, '11');
});

test('draws box and block glyphs itself with a bundled monospace font', () => {
	assert.equal(TERMINAL_OPTIONS.customGlyphs, true);
	assert.equal(TERMINAL_OPTIONS.rescaleOverlappingGlyphs, true);
	assert.equal(TERMINAL_OPTIONS.letterSpacing, 0);
	assert.match(TERMINAL_OPTIONS.fontFamily, /^"JetBrains Mono", /);
	assert.match(TERMINAL_OPTIONS.fontFamily, /monospace$/);
	assert.ok(TERMINAL_OPTIONS.scrollback > 0 && TERMINAL_OPTIONS.scrollback <= 10_000);
	assert.deepEqual(TERMINAL_FONT_FACES, [
		`400 ${TERMINAL_OPTIONS.fontSize}px "JetBrains Mono"`,
		`700 ${TERMINAL_OPTIONS.fontSize}px "JetBrains Mono"`
	]);
});
