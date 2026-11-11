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
import { decideNotification, type NotificationEventPayload } from './notification-policy.ts';
import { createNotificationSound } from './notification-sound.ts';
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
		isAudible: () => notificationSound.canStart()
	});
	return coordinator;
}

export function notificationTitle(type: string): string {
	switch (type) {
		case 'approval_requested':
			return m['notify.title.approval_requested']();
		case 'agent_task_finished':
			return m['notify.title.agent_task_finished']();
		case 'review_needed':
			return m['notify.title.review_needed']();
		case 'issues_deleted':
			return m['notify.title.issues_deleted']();
		case 'status_changed':
			return m['inbox.type.status_changed']();
		case 'assigned':
			return m['inbox.type.assigned']();
		case 'commented':
			return m['inbox.type.commented']();
		case 'mentioned':
			return m['inbox.type.mentioned']();
		case 'priority_changed':
			return m['inbox.type.priority_changed']();
		case 'issue_created':
			return m['inbox.type.issue_created']();
		case 'issue_updated':
			return m['inbox.type.issue_updated']();
		case 'due_date_changed':
			return m['inbox.type.due_date_changed']();
		case 'label_added':
			return m['inbox.type.label_added']();
		case 'cycle_changed':
			return m['inbox.type.cycle_changed']();
		default:
			return m['notify.title.generic']();
	}
}

/** Shows and/or plays one event in this tab, which the open tabs elected. */
function announce(event: NotificationEventPayload, slug: string, anyTabVisible: boolean): void {
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
		currentUserId: authState.user?.id
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
			.coordinate(notificationEventId(event))
			.then((result) => {
				if (result.act) announce(event, slug, result.visible);
			})
			.catch(() => {
				// Best effort, as below.
			});
	} catch {
		// Notifications are best effort and must never break realtime handling.
	}
}
