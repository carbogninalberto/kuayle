import { test } from 'node:test';
import assert from 'node:assert/strict';
import { reviewHarnesses, reviewSessions, reviewTone } from '../../src/lib/features/assistant/agent-review.ts';

test('review harnesses are the allowed ones, the implementer first', () => {
	assert.deepEqual(reviewHarnesses(['codex', 'claude-code', 'opencode', 'goose'], 'codex'), [
		'codex',
		'claude-code',
		'opencode'
	]);
	assert.deepEqual(
		reviewHarnesses(['claude-code'], 'codex'),
		['claude-code'],
		'a disallowed implementer is not offered'
	);
	assert.deepEqual(reviewHarnesses([], 'codex'), []);
});

const session = (over) => ({
	id: 's',
	task_id: 't',
	provider: 'codex',
	purpose: 'review',
	state: 'completed',
	created_at: '2026-10-28T10:00:00Z',
	updated_at: '2026-10-28T10:00:00Z',
	task_state: 'completed',
	review: { verdict: 'approve' },
	...over
});

test('only review sessions with a review become review cards, newest first', () => {
	const cards = reviewSessions([
		session({ id: 'old' }),
		session({ id: 'implement', purpose: 'implement', review: undefined }),
		session({ id: 'new', created_at: '2026-10-28T11:00:00Z' })
	]);
	assert.deepEqual(
		cards.map((s) => s.id),
		['new', 'old']
	);
});

test('a review tone follows the verdict only once the reviewer finished', () => {
	assert.equal(reviewTone(session({})), 'success');
	assert.equal(reviewTone(session({ review: { verdict: 'request_changes' } })), 'warning');
	assert.equal(reviewTone(session({ review: { verdict: 'unknown' } })), 'neutral', 'no verdict is never a pass');
	assert.equal(reviewTone(session({ state: 'active', review: { verdict: 'approve' } })), 'running');
	assert.equal(reviewTone(session({ state: 'failed' })), 'error');
});
