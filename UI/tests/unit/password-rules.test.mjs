import assert from 'node:assert/strict';
import test from 'node:test';
import { validateNewPassword } from '../../src/lib/features/auth/password-rules.ts';

test('rejects short, weak and mismatched passwords in that order', () => {
	assert.equal(validateNewPassword('Short1', 'Short1'), 'too_short');
	assert.equal(validateNewPassword('alllowercase123', 'alllowercase123'), 'weak');
	assert.equal(validateNewPassword('NODIGITSHEREabc', 'NODIGITSHEREabc'), 'weak');
	assert.equal(validateNewPassword('Str0ngPassw0rd!', 'Str0ngPassw0rd?'), 'mismatch');
});

test('accepts a strong matching password', () => {
	assert.equal(validateNewPassword('Str0ngPassw0rd!', 'Str0ngPassw0rd!'), null);
});
