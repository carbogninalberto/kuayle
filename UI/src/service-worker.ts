/// <reference types="@sveltejs/kit" />
/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

// Installable-app worker: precaches the built shell and public icons, serves
// them cache-first, and falls back to a static offline page for navigations.
// API data, sockets, authenticated responses and page HTML are never cached
// (see service-worker-policy.ts). A new version waits until the user chooses
// to reload, or until every Kuayle tab has closed.
import { version } from '$app/env';
import { assets, immutable } from '$app/manifest';
import {
	OFFLINE_PAGE,
	SHELL_CACHE_PREFIX,
	precacheList,
	requestStrategy
} from '$lib/features/pwa/service-worker-policy';
import { notificationTargetUrl, openNotificationTarget, requestClientNavigation } from '$lib/features/pwa/notification-click';

import { showPushNotification, renewPushSubscription } from '$lib/features/pwa/push-events';
import { PUSH_SUBSCRIPTIONS_PATH, PUSH_DEVICE_CHANGED } from '$lib/features/notifications/push';
import { pushDeviceStorage } from '$lib/features/notifications/push-device';

const worker = self as unknown as ServiceWorkerGlobalScope;
const CACHE = `${SHELL_CACHE_PREFIX}${version}`;
// Manifest paths are relative to the base path, where this worker is served;
// the policy compares root-relative pathnames.
const pathname = (path: string) => new URL(path, worker.location.href).pathname;
const ASSETS = precacheList(
	immutable.map((file) => pathname(file.path)),
	assets.map((file) => pathname(file.path))
);
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

worker.addEventListener('push', (event) => {
	event.waitUntil(showPushNotification(worker.registration, worker.location.origin, event.data));
});

async function pushRequest(path: string, options: RequestInit): Promise<Response> {
	let response = await fetch(path, options);
	if (response.status === 401 && (await fetch('/api/auth/refresh', { method: 'POST' })).ok) {
		response = await fetch(path, options);
	}
	if (!response.ok) throw new Error('Could not update push subscription');
	return response;
}

worker.addEventListener('pushsubscriptionchange', (event) => {
	// lib.webworker does not yet expose PushSubscriptionChangeEvent.
	const change = event as ExtendableEvent & {
		newSubscription?: PushSubscription | null;
		oldSubscription?: PushSubscription | null;
	};
	change.waitUntil((async () => {
		await renewPushSubscription(
			worker.registration.pushManager,
			pushDeviceStorage,
			async (body) => (await pushRequest(PUSH_SUBSCRIPTIONS_PATH, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(body)
			})).json(),
			async (id) => { await pushRequest(`${PUSH_SUBSCRIPTIONS_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' }); },
			change
		);
		for (const client of await worker.clients.matchAll({ type: 'window' })) {
			client.postMessage({ type: PUSH_DEVICE_CHANGED });
		}
	})());
});

// A clicked notification reuses an open Kuayle window when there is one. That
// window routes to the target itself, so it keeps its state; it is reloaded
// onto the target only when it does not confirm.
worker.addEventListener('notificationclick', (event) => {
	event.notification.close();
	const origin = worker.location.origin;
	const target = notificationTargetUrl(event.notification.data?.url, origin);
	if (!target) return;
	event.waitUntil(
		(async () => {
			const clients = await worker.clients.matchAll({ type: 'window', includeUncontrolled: true });
			await openNotificationTarget({
				clients,
				target,
				origin,
				requestNavigation: (client, url) =>
					requestClientNavigation(client, url, {
						createChannel: () => new MessageChannel(),
						setTimeout: (callback, ms) => setTimeout(callback, ms),
						clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>)
					}),
				openWindow: (url) => worker.clients.openWindow(url)
			});
		})()
	);
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
