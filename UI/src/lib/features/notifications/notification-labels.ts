// The message that names each notification type, shared by the inbox rows and
// the browser notification title. Pure: the callers resolve the key with `m`.

const TYPE_MESSAGE_KEYS = {
	approval_requested: 'notify.title.approval_requested',
	agent_task_finished: 'notify.title.agent_task_finished',
	review_needed: 'notify.title.review_needed',
	issues_deleted: 'notify.title.issues_deleted',
	status_changed: 'inbox.type.status_changed',
	assigned: 'inbox.type.assigned',
	commented: 'inbox.type.commented',
	mentioned: 'inbox.type.mentioned',
	priority_changed: 'inbox.type.priority_changed',
	issue_created: 'inbox.type.issue_created',
	issue_updated: 'inbox.type.issue_updated',
	due_date_changed: 'inbox.type.due_date_changed',
	label_added: 'inbox.type.label_added',
	cycle_changed: 'inbox.type.cycle_changed'
} as const;

export type NotificationTypeMessageKey = (typeof TYPE_MESSAGE_KEYS)[keyof typeof TYPE_MESSAGE_KEYS];

/** The message key for a notification type, or null for a type without one. */
export function notificationTypeMessageKey(type: string): NotificationTypeMessageKey | null {
	return Object.hasOwn(TYPE_MESSAGE_KEYS, type) ? TYPE_MESSAGE_KEYS[type as keyof typeof TYPE_MESSAGE_KEYS] : null;
}
