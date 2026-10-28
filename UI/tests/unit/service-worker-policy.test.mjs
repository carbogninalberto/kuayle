import assert from 'node:assert/strict';
import test from 'node:test';
import {
	OFFLINE_PAGE,
	precacheList,
	requestStrategy,
	shouldRegisterServiceWorker
} from '../../src/lib/features/pwa/service-worker-policy.ts';

const origin = 'https://kuayle.example';
const shell = new Set(['/_app/immutable/entry/start.js', '/favicon.svg', OFFLINE_PAGE]);
const get = (path, mode = 'cors', headers = {}) => ({ url: origin + path, method: 'GET', mode, headers: new Headers(headers) });

test('precaches the built app shell and public static assets only', () => {
	const list = precacheList(
		['/_app/immutable/entry/start.js', '/_app/version.json'],
		['/favicon.svg', '/manifest.webmanifest', '/icons/icon-192.png', '/offline.html', '/releases.json', '/upgrading.html', '/robots.txt']
	);
	assert.deepEqual(list, [
		'/_app/immutable/entry/start.js',
		'/favicon.svg',
		'/manifest.webmanifest',
		'/icons/icon-192.png',
		'/offline.html'
	]);
});

test('serves shell assets from the cache and never caches API, sockets, other origins or writes', () => {
	assert.equal(requestStrategy(get('/_app/immutable/entry/start.js'), origin, shell), 'cache-first');
	assert.equal(requestStrategy(get('/favicon.svg'), origin, shell), 'cache-first');
	for (const path of ['/api/workspaces/acme/issues', '/api/auth/me', '/api/workspaces/acme/conversations/1/stream']) {
		assert.equal(requestStrategy(get(path), origin, shell), 'bypass', path);
		assert.equal(requestStrategy(get(path, 'navigate'), origin, shell), 'bypass', path);
	}
	assert.equal(requestStrategy(get('/ws', 'cors', { upgrade: 'websocket' }), origin, shell), 'bypass');
	assert.equal(requestStrategy({ ...get('/_app/immutable/entry/start.js'), method: 'POST' }, origin, shell), 'bypass');
	assert.equal(requestStrategy({ ...get('/favicon.svg'), url: 'https://cdn.example/favicon.svg' }, origin, shell), 'bypass');
	assert.equal(requestStrategy(get('/_app/version.json'), origin, shell), 'bypass', 'version checks always reach the server');
	assert.equal(requestStrategy(get('/releases.json'), origin, shell), 'bypass');
	assert.equal(requestStrategy(get('/_app/immutable/entry/start.js', 'cors', { authorization: 'Bearer x' }), origin, shell), 'bypass');
});

test('pages always come from the network, with only the offline page as a fallback', () => {
	assert.equal(requestStrategy(get('/acme/my-issues', 'navigate'), origin, shell), 'network-offline-fallback');
	assert.equal(requestStrategy(get('/', 'navigate'), origin, shell), 'network-offline-fallback');
});

test('registers only in production builds, never under automation unless a test opts in', () => {
	const base = { dev: false, supported: true, webdriver: false, optIn: false };
	assert.equal(shouldRegisterServiceWorker(base), true);
	assert.equal(shouldRegisterServiceWorker({ ...base, dev: true }), false);
	assert.equal(shouldRegisterServiceWorker({ ...base, dev: true, optIn: true }), false);
	assert.equal(shouldRegisterServiceWorker({ ...base, supported: false }), false);
	assert.equal(shouldRegisterServiceWorker({ ...base, webdriver: true }), false);
	assert.equal(shouldRegisterServiceWorker({ ...base, webdriver: true, optIn: true }), true);
});
