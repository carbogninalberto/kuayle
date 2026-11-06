import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	AGENT_EFFORT,
	effortChoice,
	modelDefaultInput,
	overridingServer,
	preferredServer,
	serversFor
} from '../../src/lib/features/agents/model-choice.ts';

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

test('a default model server that speaks the agent protocol takes precedence over its sign-in default', () => {
	const servers = [server('anthropic', ['claude-code'], true), server('openai', ['codex'])];
	assert.equal(overridingServer(servers, 'claude-code')?.id, 'anthropic');
	assert.equal(
		overridingServer(servers, 'codex'),
		undefined,
		'only the workspace default server applies without a choice'
	);
	assert.equal(overridingServer(undefined, 'codex'), undefined);
});

test('an agent default is sent trimmed, and the agent default effort is sent as empty', () => {
	assert.deepEqual(modelDefaultInput(' claude-opus-5-5 ', 'medium'), { model: 'claude-opus-5-5', thinking: 'medium' });
	assert.deepEqual(modelDefaultInput('', AGENT_EFFORT), { model: '', thinking: '' });
	assert.equal(effortChoice(''), AGENT_EFFORT);
	assert.equal(effortChoice('xhigh'), 'xhigh');
});
