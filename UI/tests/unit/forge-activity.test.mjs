import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeForgeActivity } from '../../src/lib/api/forge-activity.ts';

const empty = { merge_requests: [], branches: [], commits: [] };

test('normalises missing or malformed activity to empty collections', () => {
	assert.deepEqual(normalizeForgeActivity([]), empty);
	assert.deepEqual(normalizeForgeActivity({}), empty);
	assert.deepEqual(normalizeForgeActivity(null), empty);
	assert.deepEqual(normalizeForgeActivity(undefined), empty);
	assert.deepEqual(normalizeForgeActivity('nope'), empty);
	assert.deepEqual(normalizeForgeActivity({ merge_requests: {}, branches: 'x', commits: null }), empty);
});

test('keeps well-formed activity unchanged', () => {
	const activity = {
		merge_requests: [{ id: 'mr-1' }],
		branches: [{ id: 'branch-1' }],
		commits: [{ id: 'commit-1' }]
	};
	assert.deepEqual(normalizeForgeActivity(activity), activity);
});
