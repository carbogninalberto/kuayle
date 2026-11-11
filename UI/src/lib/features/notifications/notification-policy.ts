// Decides what a `notification.created` realtime event does in this tab: a
// browser notification, a sound, both or nothing. Pure, so it is unit tested.

export const NOTIFICATION_EVENT_KEYS = [
	'approval_requested',
	'task_finished',
	'review_needed',
	'assigned',
	'mentioned',
	'comment',
	'issue_updated'
] as const;
export type NotificationEventKey = (typeof NOTIFICATION_EVENT_KEYS)[number];
export type NotificationEvents = Record<NotificationEventKey, boolean>;

export const NOTIFICATION_SOUNDS = ['off', 'chime', 'bell', 'pop', 'drop', 'pulse'] as const;
export type NotificationSound = (typeof NOTIFICATION_SOUNDS)[number];
export const DEFAULT_NOTIFICATION_SOUND: NotificationSound = 'chime';

export type NotificationPermissionState = 'default' | 'granted' | 'denied' | 'unsupported';

/** Payload of the user-scoped `notification.created` event. Only `type` is guaranteed. */
export interface NotificationEventPayload {
	type: string;
	issue_id?: string | null;
	issue_identifier?: string | null;
	title?: string | null;
	approval_id?: string | null;
	conversation_id?: string | null;
	task_id?: string | null;
	status?: string | null;
	/** The user whose action caused the notification, when a person did. */
	actor_id?: string | null;
	/** Identifies one published event across the recipient's open tabs. */
	event_id?: string | null;
	/** "true" when no inbox row was stored for the event. */
	live_only?: string | boolean | null;
}

/**
 * Whether the event added an unread inbox notification. A live-only event (an
 * approval that belongs to no issue or team) has no row to count.
 */
export function countsAsUnread(payload: unknown): boolean {
	if (!payload || typeof payload !== 'object') return true;
	const liveOnly = (payload as { live_only?: unknown }).live_only;
	return liveOnly !== true && liveOnly !== 'true';
}

export interface NotificationPreferences {
	browserNotifications: boolean;
	sound: NotificationSound;
	events: Partial<NotificationEvents>;
}

export interface NotificationDecision {
	showBrowser: boolean;
	playSound: boolean;
	eventKey: NotificationEventKey;
}

const TYPE_EVENT_KEYS: Record<string, NotificationEventKey> = {
	approval_requested: 'approval_requested',
	agent_task_finished: 'task_finished',
	review_needed: 'review_needed',
	assigned: 'assigned',
	mentioned: 'mentioned',
	commented: 'comment'
};

/** Every type without its own preference is an issue update. */
export function eventKeyForType(type: string | null | undefined): NotificationEventKey {
	return (type && Object.hasOwn(TYPE_EVENT_KEYS, type) && TYPE_EVENT_KEYS[type]) || 'issue_updated';
}

export function defaultNotificationEvents(): NotificationEvents {
	return Object.fromEntries(NOTIFICATION_EVENT_KEYS.map((key) => [key, true])) as NotificationEvents;
}

/** All seven keys, enabled unless explicitly `false`; unknown keys are dropped. */
export function normalizeNotificationEvents(value: unknown): NotificationEvents {
	const events = defaultNotificationEvents();
	if (value && typeof value === 'object') {
		for (const key of NOTIFICATION_EVENT_KEYS) {
			if ((value as Record<string, unknown>)[key] === false) events[key] = false;
		}
	}
	return events;
}

export function normalizeNotificationSound(value: unknown): NotificationSound {
	return NOTIFICATION_SOUNDS.includes(value as NotificationSound) ? (value as NotificationSound) : DEFAULT_NOTIFICATION_SOUND;
}

export function decideNotification(input: {
	payload: NotificationEventPayload;
	preferences: NotificationPreferences;
	permission: NotificationPermissionState;
	visibility: string;
	currentUserId?: string | null;
}): NotificationDecision {
	const { payload, preferences, permission, visibility, currentUserId } = input;
	const eventKey = eventKeyForType(payload.type);
	const muted = preferences.events[eventKey] === false;
	const own = !!payload.actor_id && payload.actor_id === currentUserId;
	if (muted || own) return { showBrowser: false, playSound: false, eventKey };
	return {
		// A visible tab already shows the unread badge and the inbox update.
		showBrowser: preferences.browserNotifications && permission === 'granted' && visibility !== 'visible',
		playSound: preferences.sound !== 'off',
		eventKey
	};
}
