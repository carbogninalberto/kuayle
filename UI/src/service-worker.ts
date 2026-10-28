/// <reference types="@sveltejs/kit" />
/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

// Installable-app worker: precaches the built shell and public icons, serves
// them cache-first, and falls back to a static offline page for navigations.
// API data, sockets, authenticated responses and page HTML are never cached
// (see service-worker-policy.ts). A new version waits until the user chooses
// to reload, or until every Kuayle tab has closed.
import { build, files, version } from '$service-worker';
import {
	OFFLINE_PAGE,
	SHELL_CACHE_PREFIX,
	precacheList,
	requestStrategy
} from '$lib/features/pwa/service-worker-policy';

const worker = self as unknown as ServiceWorkerGlobalScope;
const CACHE = `${SHELL_CACHE_PREFIX}${version}`;
const ASSETS = precacheList(build, files);
const SHELL = new Set(ASSETS);

worker.addEventListener('install', (event) => {
	event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(ASSETS)));
});

worker.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			for (const key of await caches.keys()) {
				if (key.startsWith(SHELL_CACHE_PREFIX) && key !== CACHE) await caches.delete(key);
			}
			await worker.clients.claim();
		})()
	);
});

worker.addEventListener('message', (event) => {
	if (event.data?.type === 'SKIP_WAITING') void worker.skipWaiting();
});

worker.addEventListener('fetch', (event) => {
	const strategy = requestStrategy(event.request, worker.location.origin, SHELL);
	if (strategy === 'bypass') return;
	if (strategy === 'network-offline-fallback') {
		event.respondWith(
			fetch(event.request).catch(async () => (await caches.match(OFFLINE_PAGE)) ?? Response.error())
		);
		return;
	}
	event.respondWith(
		(async () => {
			const cached = await caches.match(event.request, { cacheName: CACHE });
			return cached ?? fetch(event.request);
		})()
	);
});
