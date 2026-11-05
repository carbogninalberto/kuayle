import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	initialSelection,
	selectHarness,
	selectServer,
	serverForModel,
	choiceFrom
} from '../../src/lib/features/assistant/approval-model.ts';

const options = {
	providers: [{ id: 'claude-code' }, { id: 'codex' }],
	default_provider: 'codex',
	model_servers: [
		{ id: 's1', is_default: true, default_model: 'gpt-5', models: ['gpt-5', 'gpt-5-mini'], harnesses: ['codex'] },
		{ id: 's2', is_default: false, default_model: 'opus', models: ['opus', 'sonnet'], harnesses: ['claude-code'] },
		{ id: 's3', is_default: false, default_model: 'gpt-5', models: ['gpt-5', 'o4'], harnesses: ['codex'] }
	]
};

test('an untouched card shows what the backend starts: the default server only when it speaks the harness', () => {
	assert.deepEqual(initialSelection(options, {}), { harness: 'codex', server: 's1', model: 'gpt-5' });
	// The default server does not speak Claude Code, so it runs on the agent's own sign-in,
	// not on the first compatible server.
	assert.deepEqual(initialSelection(options, { harness: 'claude-code' }), {
		harness: 'claude-code',
		server: '',
		model: ''
	});
	assert.deepEqual(selectHarness(options, 'claude-code'), { harness: 'claude-code', server: '', model: '' });
	assert.deepEqual(selectHarness(options, 'codex'), { harness: 'codex', server: 's1', model: 'gpt-5' });
});

test('a proposed model opens on the server the backend resolves for it', () => {
	assert.deepEqual(initialSelection(options, { harness: 'claude-code', model: 'sonnet' }), {
		harness: 'claude-code',
		server: 's2',
		model: 'sonnet'
	});
	assert.deepEqual(initialSelection(options, { model: 'o4' }), { harness: 'codex', server: 's3', model: 'o4' });
	// Both servers list gpt-5: the default wins unless the task already runs on the other.
	assert.equal(serverForModel(options.model_servers, 'codex', 'gpt-5')?.id, 's1');
	assert.equal(serverForModel(options.model_servers, 'codex', 'gpt-5', 's3')?.id, 's3');
	assert.deepEqual(initialSelection(options, { model: 'gpt-5', server: 's3' }), {
		harness: 'codex',
		server: 's3',
		model: 'gpt-5'
	});
	assert.equal(serverForModel(options.model_servers, 'codex', 'opus'), undefined);
});

test('a server with no model list runs any model', () => {
	const open = [{ id: 'x', is_default: false, default_model: 'm', models: [], harnesses: ['codex'] }];
	assert.equal(serverForModel(open, 'codex', 'anything')?.id, 'x');
});

test('choosing a server starts on its default model', () => {
	const current = initialSelection(options, {});
	assert.deepEqual(selectServer(options, current, 's3'), { harness: 'codex', server: 's3', model: 'gpt-5' });
	assert.deepEqual(selectServer(options, current, ''), { harness: 'codex', server: '', model: '' });
});

test('only what differs from the initial selection is sent, with the server of a picked model', () => {
	const initial = { harness: 'codex', server: 's1', model: 'gpt-5' };
	assert.deepEqual(choiceFrom(initial, { ...initial }), {});
	assert.deepEqual(choiceFrom(initial, { ...initial, model: 'gpt-5-mini' }), {
		model: 'gpt-5-mini',
		model_server_id: 's1'
	});
	// The same model name on another server is a different choice.
	assert.deepEqual(choiceFrom(initial, { ...initial, server: 's3' }), { model: 'gpt-5', model_server_id: 's3' });
	assert.deepEqual(choiceFrom(initial, { harness: 'claude-code', server: 's2', model: 'opus' }), {
		harness: 'claude-code',
		model: 'opus',
		model_server_id: 's2'
	});
	assert.deepEqual(choiceFrom(initial, { harness: 'claude-code', server: '', model: '' }), { harness: 'claude-code' });
});

test('a follow-up opens on the server the task actually runs on, even if it no longer lists the model', () => {
	// The task has no pinned server, so it runs on the current default (s1), which does not list o4.
	assert.deepEqual(initialSelection(options, { harness: 'codex', model: 'o4', current: true }), {
		harness: 'codex',
		server: 's1',
		model: 'o4'
	});
	assert.deepEqual(initialSelection(options, { harness: 'codex', model: 'gpt-5', server: 's3', current: true }), {
		harness: 'codex',
		server: 's3',
		model: 'gpt-5'
	});
	// A harness the default does not speak and no pinned server: the agent's own sign-in.
	assert.deepEqual(initialSelection(options, { harness: 'claude-code', model: 'opus', current: true }), {
		harness: 'claude-code',
		server: '',
		model: 'opus'
	});
});
