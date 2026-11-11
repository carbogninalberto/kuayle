import type { NotificationEventPayload } from './notification-policy.ts';

export interface BrowserNotificationOptions {
	body?: string;
	tag: string;
	data: { url: string };
	renotify: boolean;
	silent: boolean;
	icon: string;
}

export interface BuiltBrowserNotification {
	title: string;
	options: BrowserNotificationOptions;
}

const ICON = '/icons/icon-192.png';

/** One notification per type and subject: a newer one replaces the older. */
export function notificationTag(payload: NotificationEventPayload): string {
	return `${payload.type}:${payload.issue_id ?? payload.approval_id ?? payload.task_id ?? 'workspace'}`;
}

/** Where a click lands: the issue, else the conversation, else the inbox. */
export function notificationUrl(slug: string, payload: NotificationEventPayload): string {
	const base = `/${encodeURIComponent(slug)}`;
	const issue = payload.issue_identifier ?? payload.issue_id;
	if (issue && payload.type !== 'issues_deleted') return `${base}/issue/${encodeURIComponent(issue)}`;
	if (payload.conversation_id) return `${base}/assistant?conversation=${encodeURIComponent(payload.conversation_id)}`;
	return `${base}/inbox`;
}

export function buildBrowserNotification(input: {
	payload: NotificationEventPayload;
	slug: string;
	title: string;
}): BuiltBrowserNotification {
	const { payload, slug, title } = input;
	const body = [payload.issue_identifier, payload.title].filter(Boolean).join(' · ');
	return {
		title,
		options: {
			...(body ? { body } : {}),
			tag: notificationTag(payload),
			data: { url: notificationUrl(slug, payload) },
			renotify: false,
			// The sound is ours (see notification-sound.ts), not the system's.
			silent: true,
			icon: ICON
		}
	};
}

interface WindowNotification {
	// Declared as a method so the DOM `Notification` class is assignable.
	onclick: ((this: never, event: never) => unknown) | null;
	close(): void;
}

export interface BrowserNotificationEnv {
	serviceWorker?: {
		getRegistration(): Promise<
			{ showNotification(title: string, options: BrowserNotificationOptions): Promise<void> } | undefined
		>;
	};
	Notification?: new (title: string, options: BrowserNotificationOptions) => WindowNotification;
	/** Opens the deep link for a notification shown without a service worker. */
	onClick?: (url: string) => void;
}

function defaultEnv(): BrowserNotificationEnv {
	const scope = globalThis as { navigator?: { serviceWorker?: BrowserNotificationEnv['serviceWorker'] } } & Pick<
		BrowserNotificationEnv,
		'Notification'
	>;
	return { serviceWorker: scope.navigator?.serviceWorker, Notification: scope.Notification };
}

/**
 * Show through the service worker registration when there is one (the only way
 * on Android and in installed apps, and its click handler can reopen a closed
 * tab), else through the window. Never throws: a notification is best effort.
 */
export async function showBrowserNotification(
	built: BuiltBrowserNotification,
	env: BrowserNotificationEnv = defaultEnv()
): Promise<'service-worker' | 'window' | 'none'> {
	try {
		const registration = await env.serviceWorker?.getRegistration();
		if (registration) {
			await registration.showNotification(built.title, built.options);
			return 'service-worker';
		}
	} catch {
		// No usable registration: fall through to the window notification.
	}
	try {
		if (!env.Notification) return 'none';
		const notification = new env.Notification(built.title, built.options);
		notification.onclick = () => {
			notification.close();
			env.onClick?.(built.options.data.url);
		};
		return 'window';
	} catch {
		return 'none';
	}
}
