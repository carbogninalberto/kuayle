import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import {
	MAX_EXPANDED_PARENTS,
	loadExpandedParents,
	loadNestSubIssues,
	saveExpandedParents,
	saveNestSubIssues
} from '../../src/lib/features/issues/sub-issue-tree-storage.ts';

class MemoryStorage {
	values = new Map();
	getItem(key) {
		return this.values.has(key) ? this.values.get(key) : null;
	}
	setItem(key, value) {
		this.values.set(key, String(value));
	}
	removeItem(key) {
		this.values.delete(key);
	}
}

beforeEach(() => {
	globalThis.localStorage = new MemoryStorage();
});

test('sub-issues are not nested until the user turns it on', () => {
	assert.equal(loadNestSubIssues(), false);
	saveNestSubIssues(true);
	assert.equal(loadNestSubIssues(), true);
	saveNestSubIssues(false);
	assert.equal(loadNestSubIssues(), false);
});

test('expanded parents are remembered per workspace', () => {
	saveExpandedParents('acme', new Set(['p1', 'p2']));
	assert.deepEqual([...loadExpandedParents('acme')].sort(), ['p1', 'p2']);
	assert.deepEqual([...loadExpandedParents('other')], []);
});

test('unreadable stored values fall back to the defaults', () => {
	saveNestSubIssues(true);
	const [nestKey] = globalThis.localStorage.values.keys();
	globalThis.localStorage.setItem(nestKey, '{not json');
	assert.equal(loadNestSubIssues(), false);

	saveExpandedParents('acme', ['p1']);
	const expandedKey = [...globalThis.localStorage.values.keys()].find((key) => key !== nestKey);
	globalThis.localStorage.setItem(expandedKey, '{not json');
	assert.deepEqual([...loadExpandedParents('acme')], []);
	globalThis.localStorage.setItem(expandedKey, JSON.stringify(['p1', 7, null, 'p2']));
	assert.deepEqual([...loadExpandedParents('acme')], ['p1', 'p2']);
	globalThis.localStorage.setItem(expandedKey, JSON.stringify({ p1: true }));
	assert.deepEqual([...loadExpandedParents('acme')], []);
});

test('only the most recently expanded parents are kept', () => {
	const ids = Array.from({ length: MAX_EXPANDED_PARENTS + 20 }, (_, i) => `p${i}`);
	saveExpandedParents('acme', ids);
	const kept = [...loadExpandedParents('acme')];
	assert.equal(kept.length, MAX_EXPANDED_PARENTS);
	assert.equal(kept[0], 'p20');
	assert.equal(kept.at(-1), `p${MAX_EXPANDED_PARENTS + 19}`);
});

test('nesting still works when storage is unavailable', () => {
	globalThis.localStorage = {
		getItem() {
			throw new Error('denied');
		},
		setItem() {
			throw new Error('quota');
		}
	};
	assert.equal(loadNestSubIssues(), false);
	assert.deepEqual([...loadExpandedParents('acme')], []);
	assert.doesNotThrow(() => saveNestSubIssues(true));
	assert.doesNotThrow(() => saveExpandedParents('acme', ['p1']));

	delete globalThis.localStorage;
	assert.equal(loadNestSubIssues(), false);
	assert.doesNotThrow(() => saveExpandedParents('acme', ['p1']));
});
