import assert from 'node:assert/strict';
import test from 'node:test';
import { expiresAt, localDateString } from '../../src/lib/features/tokens/expiry.ts';

test('custom token expiry validates actual calendar dates and ends the selected local day', () => {
	const now = new Date(2026, 0, 31, 10);
	assert.equal(expiresAt('custom', '2026-02-30', now), null);
	assert.equal(expiresAt('custom', 'not-a-date', now), null);
	assert.equal(expiresAt('custom', '', now), null);
	assert.equal(expiresAt('custom', '2026-01-30', now), null);
	const expiration = new Date(expiresAt('custom', '2026-01-31', now));
	assert.equal(localDateString(expiration), '2026-01-31');
	assert.equal(expiration.getHours(), 23);
	assert.equal(expiration.getMinutes(), 59);
	assert.equal(expiration.getSeconds(), 59);
	assert.equal(expiresAt('custom', '2026-01-31', new Date(2026, 1, 1)), null);
});

test('token expiration presets have bounded durations and never omits the timestamp', () => {
	const now = new Date('2026-01-01T12:00:00Z');
	for (const [choice, days] of [
		['30d', 30],
		['90d', 90],
		['1y', 365]
	]) {
		assert.equal(new Date(expiresAt(choice, '', now)).getTime() - now.getTime(), days * 86400000);
	}
	assert.equal(expiresAt('never', '', now), undefined);
});
