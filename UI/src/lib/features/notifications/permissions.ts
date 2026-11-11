import type { NotificationPermissionState } from './notification-policy.ts';

interface NotificationApi {
	permission?: string;
	requestPermission?: () => Promise<string> | string | void;
}
type Scope = { Notification?: NotificationApi };

function notificationApi(scope: Scope): NotificationApi | undefined {
	try {
		return scope.Notification;
	} catch {
		return undefined;
	}
}

/** Current Web Notification permission; `unsupported` when the API is missing. */
export function readPermission(scope: Scope = globalThis as Scope): NotificationPermissionState {
	const api = notificationApi(scope);
	if (!api) return 'unsupported';
	return api.permission === 'granted' || api.permission === 'denied' ? api.permission : 'default';
}

/**
 * Ask the browser for permission. Browsers only honour this from a user
 * gesture, so it is called from the Preferences button and never on load.
 */
export async function requestPermission(scope: Scope = globalThis as Scope): Promise<NotificationPermissionState> {
	const api = notificationApi(scope);
	if (!api) return 'unsupported';
	try {
		await api.requestPermission?.();
	} catch {
		// Blocked or dismissed: report whatever the browser now says.
	}
	return readPermission(scope);
}
