import { notificationTargetUrl } from './notification-click.ts';
import type { PushDeviceStorage } from '../notifications/push-device.ts';
import {
	pushOptions,
	pushSubscriptionBody,
	type EnrollPushDevice,
	type RevokePushDevice
} from '../notifications/push.ts';

type PushPayload = { title?: unknown; body?: unknown; tag?: unknown; data?: { url?: unknown } };

export async function showPushNotification(
	registration: Pick<ServiceWorkerRegistration, 'showNotification'>,
	origin: string,
	data: { json(): unknown } | null
): Promise<void> {
	let payload: PushPayload = {};
	try {
		const value = data?.json();
		if (value && typeof value === 'object') payload = value as PushPayload;
	} catch {
		// Every push is user-visible, including an unreadable payload.
	}
	const url = payload.data?.url;
	const target = notificationTargetUrl(url, origin);
	await registration.showNotification(typeof payload.title === 'string' && payload.title ? payload.title : 'Kuayle', {
		body: typeof payload.body === 'string' ? payload.body : undefined,
		tag: typeof payload.tag === 'string' ? payload.tag : undefined,
		renotify: false,
		data: target ? { url } : undefined,
		icon: '/icons/icon-192.png'
	} as NotificationOptions);
}

export async function renewPushSubscription(
	manager: Pick<PushManager, 'subscribe'>,
	storage: Pick<PushDeviceStorage, 'read' | 'replace'>,
	enroll: EnrollPushDevice,
	revoke: RevokePushDevice,
	change: { newSubscription?: PushSubscription | null; oldSubscription?: PushSubscription | null } = {}
): Promise<void> {
	const device = await storage.read();
	if (!device || device.needsReenable) return;
	let subscription = change.newSubscription;
	let id = device.id;
	try {
		subscription ??= await manager.subscribe(pushOptions(device.publicKey));
		id = (await enroll({ ...pushSubscriptionBody(subscription, device.workspaceSlug), replace_id: device.id })).id;
		if (!(await storage.replace(device.id, { ...device, id }))) {
			throw new Error('Push enrollment changed during renewal');
		}
	} catch {
		const stale = subscription ?? change.oldSubscription;
		await Promise.allSettled([
			storage.replace(device.id, { ...device, needsReenable: true }),
			revoke(id),
			stale?.unsubscribe()
		]);
	}
}
