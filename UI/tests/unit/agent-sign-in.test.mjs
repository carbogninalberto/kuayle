import { test } from 'node:test';
import assert from 'node:assert/strict';
import { signInState, tokenProblem } from '../../src/lib/features/agents/sign-in.ts';

const token = (expired = false) => ({ saved_at: '2026-10-01T00:00:00Z', expires_at: '2027-10-01T00:00:00Z', expired });
const terminal = { saved_at: '2026-09-01T00:00:00Z' };
const signIn = (fields) => ({ provider: 'claude-code', supports_token: true, token: null, terminal: null, ...fields });

test('a saved token wins over the terminal sign-in, unless it expired', () => {
	assert.equal(signInState(signIn({ token: token(), terminal })), 'token');
	assert.equal(signInState(signIn({ token: token(true), terminal })), 'terminal');
	assert.equal(signInState(signIn({ token: token(true) })), 'expired');
	assert.equal(signInState(signIn({ terminal })), 'terminal');
	assert.equal(signInState(signIn({})), 'none');
	assert.equal(signInState(undefined), 'none');
});

test('a pasted token is one line without spaces', () => {
	assert.equal(tokenProblem('  sk-ant-oat01-abcdefghijklmnopqrstuvwxyz\n'), null, 'surrounding whitespace is trimmed');
	assert.equal(tokenProblem('   '), 'empty');
	assert.equal(tokenProblem('sk-ant-oat01-abc defghijklmnopqrstuvwxyz'), 'invalid');
	assert.equal(tokenProblem('sk-ant'), 'invalid', 'too short to be a token');
	assert.equal(tokenProblem('sk-ant-oat01-' + 'a'.repeat(1100)), 'invalid');
	assert.equal(tokenProblem('sk-ant-oat01-abcdéfghijklmnopqrstuvwxyz'), 'invalid');
});
