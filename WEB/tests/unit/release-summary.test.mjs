import assert from 'node:assert/strict';
import test from 'node:test';

import { releaseSummary } from '../../src/lib/release-summary.ts';

test('release summaries use the first bold headline without issue keys or trailing punctuation', () => {
	assert.equal(releaseSummary('### Features\n- **A new assistant, built around your agents (KUA-93).**\n  - **Layout:** x'), 'A new assistant, built around your agents');
	assert.equal(releaseSummary('- **Workspace default model and effort for coding agents.**'), 'Workspace default model and effort for coding agents');
	assert.equal(releaseSummary('- **Fix both (KUA-1, KUA-2).**'), 'Fix both');
	assert.equal(releaseSummary('No headline here'), undefined);
	assert.equal(releaseSummary(42), undefined);
	assert.equal(releaseSummary(`**${'x'.repeat(200)}**`), undefined);
});
