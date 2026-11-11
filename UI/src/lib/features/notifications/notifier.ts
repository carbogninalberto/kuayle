// Turns a `notification.created` realtime event into a browser notification
// and/or a sound, according to the user's preferences. The workspace layout
// calls this once per event; the decisions live in notification-policy.ts.
import { goto } from '$app/navigation';
import { authState } from '$lib/features/auth/auth.state.svelte';
import { m } from '$lib/features/i18n/messages';
import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
import { buildBrowserNotification, showBrowserNotification } from './browser-notifications.ts';
import {
	browserStorage,
	createNotificationCoordinator,
	createSharedThrottleStore,
	newTabId,
	notificationEventId,
	openCoordinationChannel,
	type NotificationCoordinator
} from './notification-coordinator.ts';
import { notificationTypeMessageKey } from './notification-labels.ts';
import { decideNotification, notificationSubject, type NotificationEventPayload } from './notification-policy.ts';
import { createNotificationSound } from './notification-sound.ts';
import { onScreenSubjects } from './on-screen-subjects.ts';
import { readPermission } from './permissions.ts';

/**
 * One player per tab for the audio context; its throttle is shared by every
 * tab, because a different tab may announce the next event.
 */
export const notificationSound = createNotificationSound({ throttleStore: createSharedThrottleStore(browserStorage()) });

let coordinator: NotificationCoordinator | undefined;

/** Created on first use: the module is also loaded where there is no document. */
function tabCoordinator(): NotificationCoordinator {
	coordinator ??= createNotificationCoordinator({
		tabId: newTabId(),
		channel: openCoordinationChannel(),
		isVisible: () => document.visibilityState === 'visible',
		isAudible: () => notificationSound.canStart(),
		showsSubject: (subject) => onScreenSubjects.has(subject)
	});
	return coordinator;
}

export function notificationTitle(type: string): string {
	const key = notificationTypeMessageKey(type);
	return key ? m[key]() : m['notify.title.generic']();
}

/** Shows and/or plays one event in this tab, which the open tabs elected. */
function announce(
	event: NotificationEventPayload,
	slug: string,
	anyTabVisible: boolean,
	subjectOnScreen: boolean
): void {
	const decision = decideNotification({
		payload: event,
		preferences: {
			browserNotifications: preferencesState.browserNotifications,
			sound: preferencesState.notificationSound,
			events: preferencesState.notificationEvents
		},
		permission: readPermission(),
		// A system notification is noise while any Kuayle tab that received the
		// event is on screen, not only this one.
		visibility: anyTabVisible ? 'visible' : 'hidden',
		currentUserId: authState.user?.id,
		// So is anything about a conversation the user is looking at, in any tab.
		subjectOnScreen
	});
	if (decision.playSound) void notificationSound.play(preferencesState.notificationSound, decision.eventKey);
	if (decision.showBrowser && slug) {
		const built = buildBrowserNotification({ payload: event, slug, title: notificationTitle(event.type) });
		void showBrowserNotification(built, {
			serviceWorker: navigator.serviceWorker,
			Notification: window.Notification,
			onClick: (url) => {
				window.focus();
				void goto(url);
			}
		});
	}
}

export function handleNotificationCreated(payload: unknown, slug: string): void {
	try {
		if (!payload || typeof payload !== 'object' || typeof (payload as { type?: unknown }).type !== 'string') return;
		const event = payload as NotificationEventPayload;
		// Every open tab receives the event; exactly one of them announces it.
		void tabCoordinator()
			.coordinate(notificationEventId(event), notificationSubject(event))
			.then((result) => {
				if (result.act) announce(event, slug, result.visible, result.subjectOnScreen);
			})
			.catch(() => {
				// Best effort, as below.
			});
	} catch {
		// Notifications are best effort and must never break realtime handling.
	}
}
