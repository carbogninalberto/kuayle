import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { setImmediate } from 'node:timers/promises';
import test from 'node:test';

// Substitute only SvelteKit's navigation adapter; exercise the actual request implementation.
async function loadClient() {
	const source = await readFile(new URL('../../src/lib/api/client.ts', import.meta.url), 'utf8');
	const isolated = source.replace(
		"import { goto } from '$app/navigation';",
		'export const redirects = []; const goto = async (path) => { redirects.push(path); };'
	);
	const javascript = stripTypeScriptTypes(isolated);
	return import(`data:text/javascript;base64,${Buffer.from(`${javascript}\n// ${Math.random()}`).toString('base64')}`);
}
const unauthorized = () => new Response(JSON.stringify({ error: { code: 'UNAUTHORIZED' } }), { status: 401 });

for (const firstRedirect of [false, true]) {
	test(`concurrent session requests share refresh and retain individual redirect policy (${firstRedirect} first)`, async (t) => {
		const { api, redirects } = await loadClient();
		let releaseRefresh;
		const gate = new Promise((resolve) => {
			releaseRefresh = resolve;
		});
		let refreshes = 0;
		t.mock.method(globalThis, 'fetch', async (path) => {
			if (path === '/api/auth/refresh') {
				refreshes++;
				await gate;
			}
			return unauthorized();
		});
		const first = api.get('/api/auth/me', { redirectOnUnauthorized: firstRedirect }).catch((e) => e);
		await setImmediate();
		const second = api.get('/api/private', { redirectOnUnauthorized: !firstRedirect }).catch((e) => e);
		await setImmediate();
		assert.equal(refreshes, 1);
		releaseRefresh();
		const failures = await Promise.all([first, second]);
		assert.ok(failures.every((e) => e.error.code === 'UNAUTHORIZED'));
		assert.deepEqual(redirects, ['/login']);
	});
}

test('rejected refreshed credentials terminate after one refresh', async (t) => {
	const { api, redirects } = await loadClient();
	const paths = [];
	t.mock.method(globalThis, 'fetch', async (path) => {
		paths.push(path);
		return path === '/api/auth/refresh' ? new Response('{}') : unauthorized();
	});
	await assert.rejects(
		api.get('/api/auth/me', { redirectOnUnauthorized: false }),
		(e) => e.error.code === 'UNAUTHORIZED'
	);
	assert.deepEqual(paths, ['/api/auth/me', '/api/auth/refresh', '/api/auth/me']);
	assert.deepEqual(redirects, []);
});

test('credential errors do not refresh or redirect and preserve the backend error', async (t) => {
	const { api, redirects } = await loadClient();
	const paths = [];
	t.mock.method(globalThis, 'fetch', async (path) => {
		paths.push(path);
		return new Response(
			JSON.stringify({ error: { code: 'INVALID_CREDENTIALS', message: 'Invalid email or password' } }),
			{ status: 401 }
		);
	});
	await assert.rejects(
		api.post(
			'/api/auth/login',
			{ email: 'user@test', password: 'bad' },
			{ redirectOnUnauthorized: false, retryUnauthorized: false }
		),
		(e) => e.error.code === 'INVALID_CREDENTIALS'
	);
	assert.deepEqual(paths, ['/api/auth/login']);
	assert.deepEqual(redirects, []);
});
