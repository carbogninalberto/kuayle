import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	allowedEfforts,
	catalogStatus,
	effortAfterModelChange,
	filterModels,
	typedChoice
} from '../../src/lib/features/agents/model-catalog.ts';

const LEVELS = ['off', 'low', 'medium', 'high', 'xhigh', 'max'];
const models = [
	{ id: 'opencode-go/deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash', efforts: ['off', 'low', 'high', 'max'] },
	{ id: 'opencode-go/glm-6', name: 'GLM 6', efforts: ['off', 'high'] },
	{ id: 'opencode-go/kimi-k3', name: 'Kimi K3', efforts: [] },
	{ id: 'opencode-go/qwen-coder', name: 'Qwen Coder' }
];
const ids = (list) => list.map((model) => model.id);

test('suggestions match the id and the name, whatever the case', () => {
	assert.deepEqual(ids(filterModels(models, '')), ids(models), 'nothing typed lists every model');
	assert.deepEqual(ids(filterModels(models, '  ')), ids(models));
	assert.deepEqual(ids(filterModels(models, 'GLM')), ['opencode-go/glm-6']);
	assert.deepEqual(ids(filterModels(models, 'v4.1-fl')), ['opencode-go/deepseek-v4.1-flash'], 'part of the id');
	assert.deepEqual(ids(filterModels(models, 'seek v4.1 flash')), ['opencode-go/deepseek-v4.1-flash'], 'the name');
	assert.deepEqual(ids(filterModels(models, 'opencode-go/')), ids(models));
	assert.deepEqual(ids(filterModels(models, 'coder qwen')), ['opencode-go/qwen-coder'], 'every word must match');
	assert.deepEqual(filterModels(models, 'anthropic/claude-sonnet-5'), []);
	assert.deepEqual(filterModels(undefined, 'glm'), []);
});

test('what was typed is always offered as it is, unless it already is a suggestion', () => {
	assert.equal(typedChoice(models, ' anthropic/claude-sonnet-5 '), 'anthropic/claude-sonnet-5');
	assert.equal(typedChoice(models, 'glm'), 'glm', 'a partial name can still be saved as typed');
	assert.equal(typedChoice(models, 'opencode-go/glm-6'), '', 'an exact id is the suggestion itself');
	assert.equal(typedChoice(models, '   '), '');
	assert.equal(typedChoice([], 'gpt-5.5'), 'gpt-5.5', 'free text works without any list');
});

test('the effort choices are those the chosen model allows', () => {
	assert.deepEqual(allowedEfforts(models, 'opencode-go/glm-6', LEVELS), ['off', 'high']);
	assert.deepEqual(allowedEfforts(models, 'opencode-go/deepseek-v4.1-flash', LEVELS), ['off', 'low', 'high', 'max']);
	assert.deepEqual(allowedEfforts(models, 'opencode-go/kimi-k3', LEVELS), LEVELS, 'efforts not known: any level');
	assert.deepEqual(allowedEfforts(models, 'opencode-go/qwen-coder', LEVELS), LEVELS, 'an older server sends none');
	assert.deepEqual(allowedEfforts(models, 'anthropic/claude-sonnet-5', LEVELS), LEVELS, 'a typed model: any level');
	assert.deepEqual(allowedEfforts(models, '', LEVELS), LEVELS);
	assert.deepEqual(allowedEfforts(undefined, 'opencode-go/glm-6', LEVELS), LEVELS, 'no list: any level');
	assert.deepEqual(
		allowedEfforts([{ id: 'x/y', name: 'Y', efforts: ['high', 'ultra'] }], 'x/y', LEVELS),
		['high'],
		'a level this app does not know is not offered'
	);
});

test('choosing a model that does not allow the current effort resets it to the agent default', () => {
	assert.deepEqual(effortAfterModelChange(models, 'opencode-go/glm-6', 'medium'), { effort: '', reset: 'medium' });
	assert.deepEqual(effortAfterModelChange(models, 'opencode-go/glm-6', 'high'), { effort: 'high', reset: '' });
	assert.deepEqual(effortAfterModelChange(models, 'opencode-go/glm-6', ''), { effort: '', reset: '' });
	assert.deepEqual(effortAfterModelChange(models, 'opencode-go/kimi-k3', 'xhigh'), { effort: 'xhigh', reset: '' });
	assert.deepEqual(effortAfterModelChange(models, 'openrouter/x/y', 'xhigh'), { effort: 'xhigh', reset: '' });
	assert.deepEqual(effortAfterModelChange(undefined, 'opencode-go/glm-6', 'medium'), { effort: 'medium', reset: '' });
});

test('the list says whether it is loading, unavailable, stale, empty or ready', () => {
	const catalog = { harness: 'opencode', models, source: 'opencode-go', stale: false, unavailable: false };
	assert.equal(catalogStatus(null, true, false), 'loading');
	assert.equal(catalogStatus(catalog, true, false), 'loading', 'a refresh shows as loading');
	assert.equal(catalogStatus(null, false, true), 'unavailable', 'the request failed');
	assert.equal(catalogStatus({ ...catalog, models: [], unavailable: true }, false, false), 'unavailable');
	assert.equal(catalogStatus({ ...catalog, stale: true }, false, false), 'stale');
	assert.equal(catalogStatus({ ...catalog, models: [], source: '' }, false, false), 'empty');
	assert.equal(catalogStatus(catalog, false, false), 'ready');
	assert.equal(catalogStatus(null, false, false), 'empty', 'nothing asked yet');
});
