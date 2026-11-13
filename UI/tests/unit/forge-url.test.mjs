import assert from 'node:assert/strict';
import test from 'node:test';
import { safeForgeUrl } from '../../src/lib/security/forge-url.ts';

test('accepts plain http and https forge links', () => {
	assert.equal(
		safeForgeUrl('https://gitlab.example.com/team/app/-/merge_requests/7'),
		'https://gitlab.example.com/team/app/-/merge_requests/7'
	);
	assert.equal(
		safeForgeUrl('http://gitea.internal:3000/team/app/pulls/3'),
		'http://gitea.internal:3000/team/app/pulls/3'
	);
	assert.equal(safeForgeUrl('https://forgejo.example.com'), 'https://forgejo.example.com/');
});

test('rejects anything that is not a plain http(s) URL', () => {
	for (const value of [
		'javascript:alert(1)',
		'data:text/html,<script>alert(1)</script>',
		'ftp://forge.example.com/team/app',
		'https://user:password@forge.example.com/team/app',
		'https://forge.example.com/team/app ',
		' https://forge.example.com/team/app',
		'',
		null,
		undefined,
		42
	]) {
		assert.equal(safeForgeUrl(value), null, String(value));
	}
});
