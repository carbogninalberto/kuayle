import { test } from 'node:test';
import assert from 'node:assert/strict';
import { agentInfo, agents } from '../../src/lib/features/agents/agents.ts';

test('every coding agent has a logo for AgentLogo to draw', () => {
	for (const agent of agents) {
		assert.ok(agent.icon, `${agent.id} has no icon`);
		assert.match(agent.icon.path, /^[Mm][\d\s.,a-zA-Z-]+$/, `${agent.id} icon is not an SVG path`);
		assert.match(agent.icon.hex, /^[0-9A-Fa-f]{6}$/, `${agent.id} icon colour is not a hex value`);
	}
});

test('codex, pi, goose and crush resolve to their own distinct logos', () => {
	const paths = ['codex', 'pi', 'goose', 'crush'].map((id) => agentInfo(id).icon?.path);
	assert.ok(paths.every(Boolean));
	assert.equal(new Set(paths).size, 4);
});

test('an unknown agent keeps the monogram fallback with no icon', () => {
	const info = agentInfo('some-new-agent', 'Some New Agent');
	assert.equal(info.icon, undefined);
	assert.equal(info.name, 'Some New Agent');
	assert.equal(info.monogram, 'So');
	assert.equal(agentInfo('mystery').monogram, 'my');
});
