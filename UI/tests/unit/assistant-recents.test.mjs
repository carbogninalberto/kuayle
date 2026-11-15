import assert from 'node:assert/strict';
import test from 'node:test';
import { clearRecents, readRecents, recentsKey, writeRecents } from '../../src/lib/features/assistant/recents-cache.ts';

const chat = (id) => ({ id, title: id, workspace_id: 'w', creator_id: 'u' });

test('the key separates users, workspaces and project filters', () => {
	const user = recentsKey('user-1', 'test', 'all');
	assert.equal(user, 'user-1:test:all');
	assert.notEqual(user, recentsKey('user-2', 'test', 'all'));
	assert.notEqual(user, recentsKey('user-1', 'other', 'all'));
	assert.notEqual(user, recentsKey('user-1', 'test', 'project-1'));
	writeRecents(user, [chat('a')]);
	assert.deepEqual(readRecents(recentsKey('user-1', 'test', 'all')), [chat('a')]);
	assert.equal(readRecents(recentsKey('user-2', 'test', 'all')), undefined);
	assert.equal(readRecents(recentsKey('user-1', 'other', 'all')), undefined);
	assert.equal(readRecents(recentsKey('user-1', 'test', 'project-1')), undefined);
});

test('anonymous keys are never stored', () => {
	const key = recentsKey(undefined, 'test', 'all');
	assert.equal(key, null);
	writeRecents(key, [chat('a')]);
	assert.equal(readRecents(null), undefined);
	assert.equal(readRecents(key), undefined);
});

test('writing then reading returns the items', () => {
	const key = recentsKey('user-1', 'test', 'all');
	writeRecents(key, [chat('a'), chat('b')]);
	assert.deepEqual(readRecents(key), [chat('a'), chat('b')]);
});

test('a reader cannot mutate the cached list', () => {
	const key = recentsKey('user-1', 'test', 'all');
	const items = [chat('a')];
	writeRecents(key, items);
	items.push(chat('b'));
	readRecents(key).push(chat('c'));
	assert.deepEqual(readRecents(key), [chat('a')]);
});

test('eviction keeps the newest entries', () => {
	clearRecents();
	for (let i = 0; i < 21; i += 1) writeRecents(recentsKey('user-1', 'test', String(i)), [chat(String(i))]);
	assert.equal(readRecents(recentsKey('user-1', 'test', '0')), undefined);
	for (let i = 1; i < 21; i += 1)
		assert.deepEqual(readRecents(recentsKey('user-1', 'test', String(i))), [chat(String(i))]);
	// Reading does not refresh an entry; a new write evicts the oldest insertion.
	writeRecents(recentsKey('user-1', 'test', '21'), [chat('21')]);
	assert.equal(readRecents(recentsKey('user-1', 'test', '1')), undefined);
	assert.deepEqual(readRecents(recentsKey('user-1', 'test', '21')), [chat('21')]);
});

test('clearing empties the cache', () => {
	const key = recentsKey('user-1', 'test', 'all');
	writeRecents(key, [chat('a')]);
	clearRecents();
	assert.equal(readRecents(key), undefined);
});
