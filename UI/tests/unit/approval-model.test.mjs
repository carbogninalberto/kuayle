import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	initialSelection,
	modelsFor,
	selectHarness,
	choiceFrom
} from '../../src/lib/features/assistant/approval-model.ts';

const options = {
	providers: [{ id: 'claude-code' }, { id: 'codex' }],
	default_provider: 'codex',
	model_servers: [
		{ id: 's1', is_default: true, default_model: 'gpt-5', models: ['gpt-5', 'gpt-5-mini'], harnesses: ['codex'] },
		{ id: 's2', is_default: false, default_model: 'opus', models: ['opus', 'sonnet'], harnesses: ['claude-code'] }
	]
};

test('the card starts on the proposal, else the configured default', () => {
	assert.deepEqual(initialSelection(options, {}), { harness: 'codex', model: 'gpt-5' });
	assert.deepEqual(initialSelection(options, { harness: 'claude-code' }), { harness: 'claude-code', model: 'opus' });
	assert.deepEqual(initialSelection(options, { harness: 'claude-code', model: 'sonnet' }), {
		harness: 'claude-code',
		model: 'sonnet'
	});
});

test('a harness with no model server has no model to pick', () => {
	const bare = { ...options, model_servers: [] };
	assert.deepEqual(initialSelection(bare, {}), { harness: 'codex', model: '' });
	assert.deepEqual(modelsFor(bare.model_servers, 'codex'), []);
});

test('changing the harness resets the model to that harness default', () => {
	assert.deepEqual(selectHarness(options, 'claude-code'), { harness: 'claude-code', model: 'opus' });
});

test('models come from the servers that speak the harness', () => {
	assert.deepEqual(modelsFor(options.model_servers, 'codex'), ['gpt-5', 'gpt-5-mini']);
});

test('only what differs from the initial selection is sent', () => {
	const initial = { harness: 'codex', model: 'gpt-5' };
	assert.deepEqual(choiceFrom(initial, { ...initial }), {});
	assert.deepEqual(choiceFrom(initial, { harness: 'codex', model: 'gpt-5-mini' }), { model: 'gpt-5-mini' });
	assert.deepEqual(choiceFrom(initial, { harness: 'claude-code', model: 'opus' }), {
		harness: 'claude-code',
		model: 'opus'
	});
});
