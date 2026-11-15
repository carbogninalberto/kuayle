import assert from 'node:assert/strict';
import test from 'node:test';
import { canEnableWebhooks, deliveryProblem, deliveryState } from '../../src/lib/features/webhooks/deliveries.ts';

const delivery = (fields) => ({
	id: 'd1',
	event: 'issue.updated',
	status: 'pending',
	attempts: 0,
	last_status_code: null,
	last_error: null,
	next_attempt_at: '2026-11-15T10:00:00Z',
	created_at: '2026-11-15T09:59:00Z',
	updated_at: '2026-11-15T09:59:00Z',
	completed_at: null,
	...fields
});

test('a pending delivery that already failed is being retried', () => {
	assert.equal(deliveryState(delivery({})), 'pending');
	assert.equal(deliveryState(delivery({ attempts: 2, last_error: 'timeout' })), 'retrying');
	assert.equal(deliveryState(delivery({ status: 'delivered', attempts: 1, next_attempt_at: null })), 'delivered');
	assert.equal(deliveryState(delivery({ status: 'failed', attempts: 6, next_attempt_at: null })), 'failed');
	assert.equal(deliveryState(delivery({ status: 'failed', last_error: 'workspace_private', next_attempt_at: null })), 'failed');
});

test('the last problem names the HTTP status of a rejection and the code of other failures', () => {
	assert.equal(deliveryProblem(delivery({})), null);
	assert.equal(deliveryProblem(delivery({ status: 'delivered', last_status_code: 204 })), null);
	assert.equal(deliveryProblem(delivery({ last_error: 'bad_status', last_status_code: 503 })), 'HTTP 503');
	assert.equal(deliveryProblem(delivery({ last_error: 'timeout' })), 'timeout');
	assert.equal(deliveryProblem(delivery({ last_error: 'bad_status' })), 'bad_status');
});

test('webhooks can be created or enabled only while the workspace has no private team', () => {
	assert.equal(canEnableWebhooks({ privacy_enabled: false }), true);
	assert.equal(canEnableWebhooks({}), true);
	assert.equal(canEnableWebhooks({ privacy_enabled: true }), false);
	assert.equal(canEnableWebhooks(null), false, 'unknown until the workspace is loaded');
});
