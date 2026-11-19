import assert from 'node:assert/strict';
import test from 'node:test';
import { consumeSignInLink, loginRedirectUrl, safeLoginNext } from '../../src/lib/features/auth/sign-in-link.ts';

test('strips the sign-in token before exchanging it and preserves the destination', async () => {
	const url = new URL('https://kuayle.test/acme/assistant?conversation=abc&sign_in=one-use#latest');
	const calls = [];
	await consumeSignInLink(url, 'acme', (clean) => calls.push(clean.href), async (slug, token) => {
		calls.push([slug, token]);
	});
	assert.deepEqual(calls, ['https://kuayle.test/acme/assistant?conversation=abc#latest', ['acme', 'one-use']]);
});

test('removes unusable credentials and leaves ordinary URLs alone', async () => {
	const calls = [];
	const replace = (clean) => calls.push(clean.pathname + clean.search);
	const reject = async () => { throw new Error('expired'); };
	await assert.rejects(consumeSignInLink(new URL('https://kuayle.test/acme/inbox?sign_in=expired'), 'acme', replace, reject), /expired/);
	await consumeSignInLink(new URL('https://kuayle.test/login?sign_in=ignored'), undefined, replace, reject);
	await consumeSignInLink(new URL('https://kuayle.test/acme/inbox'), 'acme', replace, reject);
	assert.deepEqual(calls, ['/acme/inbox', '/login']);
});

test('only returns to an internal destination and never forwards a sign-in token', () => {
	const origin = 'https://kuayle.test';
	const destination = '/acme/issue/ACME-2?mode=full#activity';
	const login = new URL(loginRedirectUrl(new URL(origin + '/acme/issue/ACME-2?mode=full&sign_in=secret#activity')), origin);
	assert.equal(login.pathname, '/login');
	assert.equal(safeLoginNext(login.searchParams.get('next'), origin), destination);
	for (const unsafe of ['https://other.test/', '//other.test/', '/a/..//other.test/', '/\\other.test/', '/%6cogin?next=/elsewhere']) {
		assert.equal(safeLoginNext(unsafe, origin), null, unsafe);
	}
});
