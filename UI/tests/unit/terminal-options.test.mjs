import assert from 'node:assert/strict';
import test from 'node:test';
import {
	TERMINAL_OPTIONS,
	TERMINAL_UNICODE_VERSION
} from '../../src/lib/features/dev-machines/terminal-options.ts';

test('uses raw PTY newlines and Unicode 11 for full-screen terminal applications', () => {
	assert.equal(TERMINAL_OPTIONS.convertEol, false);
	assert.equal(TERMINAL_OPTIONS.allowProposedApi, true);
	assert.equal(TERMINAL_OPTIONS.lineHeight, 1);
	assert.equal(TERMINAL_UNICODE_VERSION, '11');
});
