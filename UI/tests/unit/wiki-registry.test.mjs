import assert from 'node:assert/strict';
import test from 'node:test';
import { addDays, recentPeriod, relativeDay, validPeriod } from '../../src/lib/features/wiki/registry.ts';

const now = new Date(2026, 2, 1, 9, 30); // 1 March 2026, local time

test('days are labelled today and yesterday across month ends', () => {
	assert.equal(relativeDay('2026-03-01', now), 'today');
	assert.equal(relativeDay('2026-02-28', now), 'yesterday');
	assert.equal(relativeDay('2026-02-27', now), null);
	assert.equal(addDays('2024-02-28', 1), '2024-02-29', 'leap day');
});

test('a recent period includes today', () => {
	assert.deepEqual(recentPeriod(1, now), { period_start: '2026-03-01', period_end: '2026-03-01' });
	assert.deepEqual(recentPeriod(7, now), { period_start: '2026-02-23', period_end: '2026-03-01' });
});

test('custom periods must be ordered and short enough', () => {
	assert.equal(validPeriod('2026-01-01', '2026-01-31'), true);
	assert.equal(validPeriod('2026-01-01', '2026-02-01'), false);
	assert.equal(validPeriod('2026-01-02', '2026-01-01'), false);
	assert.equal(validPeriod('', '2026-01-01'), false);
});
