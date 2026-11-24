import assert from 'node:assert/strict';
import test from 'node:test';
import { usageParts } from '../../src/lib/features/assistant/run-usage.ts';

test('a run shows only what its harness reported, compact', () => {
	assert.deepEqual(
		usageParts(
			{
				input_tokens: 12_345,
				output_tokens: 4_100,
				cache_read_tokens: 210_000,
				cache_write_tokens: 3_200,
				cost_usd: 0.4182
			},
			'en'
		),
		[
			{ kind: 'input', value: '12.3K' },
			{ kind: 'output', value: '4.1K' },
			{ kind: 'cache_read', value: '210K' },
			{ kind: 'cache_write', value: '3.2K' },
			{ kind: 'cost', value: '$0.42' }
		]
	);
	// Codex reports no cost and this version no cache writes.
	assert.deepEqual(usageParts({ input_tokens: 315, output_tokens: 122, cache_read_tokens: 24_448 }, 'en'), [
		{ kind: 'input', value: '315' },
		{ kind: 'output', value: '122' },
		{ kind: 'cache_read', value: '24.4K' }
	]);
});

test('a cost below a cent keeps its digits, and a reported zero is shown', () => {
	assert.deepEqual(usageParts({ cost_usd: 0.0012 }, 'en'), [{ kind: 'cost', value: '$0.0012' }]);
	assert.deepEqual(usageParts({ input_tokens: 0, cost_usd: 0 }, 'en'), [
		{ kind: 'input', value: '0' },
		{ kind: 'cost', value: '$0.00' }
	]);
});

test('nothing reported shows nothing', () => {
	assert.deepEqual(usageParts({}, 'en'), []);
	assert.deepEqual(usageParts(undefined, 'en'), []);
	assert.deepEqual(usageParts({ input_tokens: null, cost_usd: null }, 'en'), []);
});

test('numbers follow the reader locale', () => {
	assert.deepEqual(usageParts({ input_tokens: 12_345, cost_usd: 1.5 }, 'it'), [
		{ kind: 'input', value: '12.345' },
		{ kind: 'cost', value: '1,50\u00a0USD' }
	]);
});
