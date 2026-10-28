import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	messageActions,
	messageQuery,
	messageTone,
	summarize
} from '../../src/lib/features/agent-messages/agent-messages.ts';

const message = (over) => ({
	id: 'm1',
	state: 'queued',
	channel: 'internal',
	can_moderate: true,
	from_issue_id: 'i1',
	to_issue_id: 'i2',
	conversation_hold_id: null,
	...over
});

test('moderators get only the actions the message state allows', () => {
	assert.deepEqual(messageActions(message({})), ['hold', 'drop', 'hold_conversation']);
	assert.deepEqual(messageActions(message({ state: 'held', conversation_hold_id: 'h1' })), [
		'release',
		'drop',
		'release_conversation'
	]);
	assert.deepEqual(messageActions(message({ state: 'delivered' })), ['hold_conversation']);
	assert.deepEqual(messageActions(message({ state: 'rejected', to_issue_id: null })), []);
	assert.deepEqual(messageActions(message({ can_moderate: false })), [], 'readers see no controls');
});

test('states map to visual tones and summaries count them', () => {
	assert.equal(messageTone('held'), 'warning');
	assert.equal(messageTone('delivered'), 'success');
	assert.equal(messageTone('rejected'), 'error');
	assert.equal(messageTone('dropped'), 'neutral');
	assert.equal(messageTone('queued'), 'running');
	assert.deepEqual(summarize([message({}), message({ state: 'held' }), message({ state: 'held' })]), {
		queued: 1,
		held: 2
	});
});

test('filters become a query string without empty values', () => {
	assert.equal(messageQuery({}), '');
	assert.equal(messageQuery({ issue: ' eng-12 ', state: 'held', channel: '' }), '?issue=ENG-12&state=held');
	assert.equal(messageQuery({ issue_id: 'a b', before: 'm9' }), '?issue_id=a%20b&before=m9');
});
