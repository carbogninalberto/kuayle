import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { agentInfo, agents } from '../../src/lib/features/agents/agents.ts';

test('every coding agent has a logo for AgentLogo to draw', () => {
	for (const agent of agents) {
		assert.ok(agent.icon || agent.logo || agent.image, `${agent.id} has no logo`);
		if (agent.icon) {
			assert.match(agent.icon.path, /^[Mm][\d\s.,a-zA-Z-]+$/, `${agent.id} icon is not an SVG path`);
			assert.match(agent.icon.hex, /^[0-9A-Fa-f]{6}$/, `${agent.id} icon colour is not a hex value`);
		}
	}
});

test('image logos are served from static/ and exist', () => {
	for (const agent of agents.filter((a) => a.image)) {
		assert.match(agent.image, /^\/agents\/[\w-]+\.png$/);
		assert.ok(
			existsSync(new URL(`../../static${agent.image}`, import.meta.url)),
			`${agent.id}: ${agent.image} is missing`
		);
	}
});

test('a theme-aware logo has a light and a dark fill for every layer', () => {
	const pi = agentInfo('pi').logo;
	assert.ok(pi);
	assert.ok(pi.layers.length > 0);
	for (const layer of pi.layers) {
		assert.match(layer.light, /^#[0-9A-Fa-f]{6}$/);
		assert.match(layer.dark, /^#[0-9A-Fa-f]{6}$/);
		assert.notEqual(layer.light, layer.dark);
	}
});

test('codex, pi, goose and crush resolve to their own logos', () => {
	for (const id of ['codex', 'pi', 'goose', 'crush']) {
		const info = agentInfo(id);
		assert.ok(info.icon || info.logo || info.image, `${id} has no logo`);
	}
	assert.equal(new Set(['goose', 'crush'].map((id) => agentInfo(id).image)).size, 2);
});

test('an unknown agent keeps the monogram fallback with no logo', () => {
	const info = agentInfo('some-new-agent', 'Some New Agent');
	assert.equal(info.icon, undefined);
	assert.equal(info.logo, undefined);
	assert.equal(info.image, undefined);
	assert.equal(info.name, 'Some New Agent');
	assert.equal(info.monogram, 'So');
	assert.equal(agentInfo('mystery').monogram, 'my');
});
