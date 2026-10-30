import assert from 'node:assert/strict';
import test from 'node:test';
import {
	AVATAR_MAX_BYTES,
	validateAvatarFile,
	validateAvatarUrl
} from '../../src/lib/features/profile/avatar-rules.ts';

test('validates avatar file type and size', () => {
	assert.equal(validateAvatarFile({ type: 'image/png', size: 1000 }), null);
	assert.equal(validateAvatarFile({ type: 'image/svg+xml', size: 1000 }), 'type');
	assert.equal(validateAvatarFile({ type: 'application/pdf', size: 1000 }), 'type');
	assert.equal(validateAvatarFile({ type: 'image/jpeg', size: AVATAR_MAX_BYTES + 1 }), 'size');
	assert.equal(validateAvatarFile({ type: 'image/jpeg', size: AVATAR_MAX_BYTES }), null);
});

test('validates avatar URLs', () => {
	assert.equal(validateAvatarUrl(' https://example.com/a.png '), null);
	assert.equal(validateAvatarUrl('http://example.com/a.png'), null);
	assert.equal(validateAvatarUrl('javascript:alert(1)'), 'url');
	assert.equal(validateAvatarUrl('not a url'), 'url');
	assert.equal(validateAvatarUrl(''), 'url');
});
