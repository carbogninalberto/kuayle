import { test } from 'node:test';
import assert from 'node:assert/strict';
import { preferredServer, serversFor } from '../../src/lib/features/agents/model-choice.ts';

const server = (id, harnesses, is_default = false) => ({ id, harnesses, is_default });

test('a harness only sees servers that speak its protocol', () => {
	const servers = [
		server('anthropic', ['claude-code'], true),
		server('openai', ['codex']),
		server('both', ['codex', 'claude-code'])
	];
	assert.deepEqual(
		serversFor(servers, 'codex').map((s) => s.id),
		['openai', 'both']
	);
	assert.deepEqual(serversFor(servers, 'goose'), []);
	assert.deepEqual(serversFor(undefined, 'codex'), []);
});

test('the workspace default is preferred when it fits, else the first compatible server', () => {
	const servers = [
		server('anthropic', ['claude-code'], true),
		server('openai', ['codex']),
		server('both', ['codex', 'claude-code'])
	];
	assert.equal(preferredServer(servers, 'claude-code')?.id, 'anthropic');
	assert.equal(preferredServer(servers, 'codex')?.id, 'openai');
	assert.equal(preferredServer(servers, 'kimi'), undefined, 'other agents use their own sign-in');
});
