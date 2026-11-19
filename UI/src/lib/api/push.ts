import { api } from './client';
import { pushDeviceStorage } from '$lib/features/notifications/push-device';
import {
	PUSH_SUBSCRIPTIONS_PATH,
	type EnrollPushDevice,
	type RevokePushDevice
} from '$lib/features/notifications/push';

export async function getVapidPublicKey(): Promise<string> {
	return (await api.get<{ public_key: string }>('/api/push/vapid-public-key')).public_key;
}

export const enrollPushDevice: EnrollPushDevice = (body) => api.post(PUSH_SUBSCRIPTIONS_PATH, body);
export const revokePushDevice: RevokePushDevice = (id) =>
	api.fetch(`${PUSH_SUBSCRIPTIONS_PATH}/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		signal: AbortSignal.timeout(5000),
		redirectOnUnauthorized: false
	});

export async function disconnectPushDevice(): Promise<void> {
	const device = await pushDeviceStorage.take();
	// Either server revocation or browser unsubscription stops delivery.
	const unsubscribe = async () => {
		const registration = await navigator.serviceWorker?.getRegistration();
		const subscription = await registration?.pushManager?.getSubscription();
		await subscription?.unsubscribe();
	};
	const [local, server] = await Promise.allSettled([
		unsubscribe(),
		device && revokePushDevice(device.id)
	]);
	if (local.status === 'rejected' && (!device || server.status === 'rejected')) {
		if (device) await pushDeviceStorage.replace(undefined, device);
		throw new Error('Could not disconnect push notifications');
	}
}
