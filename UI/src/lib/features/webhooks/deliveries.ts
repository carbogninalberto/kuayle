import type { WebhookDelivery } from '../../api/webhooks';

/** The event names a webhook can subscribe to, in display order. */
export const WEBHOOK_EVENTS = [
	'issue.created',
	'issue.updated',
	'issue.deleted',
	'issue.triaged',
	'comment.created',
	'comment.updated',
	'comment.deleted'
] as const;

export type DeliveryState = 'pending' | 'retrying' | 'delivered' | 'failed';

/** A pending delivery that has already failed is being retried. */
export function deliveryState(delivery: WebhookDelivery): DeliveryState {
	if (delivery.status === 'pending') return delivery.attempts > 0 ? 'retrying' : 'pending';
	return delivery.status;
}

/**
 * The last problem of a delivery as the log shows it: "HTTP 503" when the
 * receiver rejected it, the error code otherwise, null when there is none.
 */
export function deliveryProblem(delivery: WebhookDelivery): string | null {
	if (!delivery.last_error) return null;
	if (delivery.last_error === 'bad_status' && delivery.last_status_code) return `HTTP ${delivery.last_status_code}`;
	return delivery.last_error;
}

/**
 * A workspace that has or had a private team delivers no webhooks, and the
 * server refuses to create or enable one there. Unknown until the workspace
 * is loaded.
 */
export function canEnableWebhooks(workspace: { privacy_enabled?: boolean } | null): boolean {
	return workspace !== null && !workspace.privacy_enabled;
}
