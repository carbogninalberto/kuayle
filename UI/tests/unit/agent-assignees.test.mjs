import assert from 'node:assert/strict';
import test from 'node:test';
import {
	agentIsLive,
	agentsForTeam,
	assigneeGroup,
	groupAgentId,
	toggleAgent,
	unassignCancelsTask
} from '../../src/lib/features/agents/agent-assignees.ts';

const fixer = { id: 'b-fixer', name: 'Fixer', team_ids: ['eng'] };
const writer = { id: 'a-writer', name: 'Docs writer', team_ids: ['eng', 'docs'] };

test('pickers offer only the agents enabled for the issue team', () => {
	assert.deepEqual(agentsForTeam([fixer, writer], 'eng').map((a) => a.name), ['Docs writer', 'Fixer']);
	assert.deepEqual(agentsForTeam([fixer, writer], 'docs'), [writer]);
	assert.deepEqual(agentsForTeam([fixer, writer], 'sales'), []);
	assert.deepEqual(agentsForTeam([fixer, writer], null), []);
});

test('toggling an agent adds or removes it without touching the others', () => {
	assert.deepEqual(toggleAgent(['a'], 'b'), ['a', 'b']);
	assert.deepEqual(toggleAgent(['a', 'b'], 'a'), ['b']);
});

test('unassigning asks first only while the agent task is still working', () => {
	for (const state of ['queued', 'preparing', 'running', 'waiting_input', 'validating', 'publishing', 'blocked', 'failed']) {
		assert.equal(unassignCancelsTask({ id: 'a', name: 'A', task_state: state }), true, state);
	}
	for (const state of ['awaiting_review', 'completed', 'cancelled', 'cancelling', null]) {
		assert.equal(unassignCancelsTask({ id: 'a', name: 'A', task_state: state }), false, String(state));
	}
	assert.equal(unassignCancelsTask(undefined), false);
});

test('a status is live until the task waits on a person or ends', () => {
	assert.equal(agentIsLive({ id: 'a', name: 'A', task_state: null }), true, 'the task is being started');
	assert.equal(agentIsLive({ id: 'a', name: 'A', task_state: 'running' }), true);
	assert.equal(agentIsLive({ id: 'a', name: 'A', task_state: 'awaiting_review' }), false);
	assert.equal(agentIsLive({ id: 'a', name: 'A', task_state: null, error: 'Coding agents are not enabled' }), false);
});

test('grouping by assignee puts agent-only issues in the agent group', () => {
	assert.deepEqual(assigneeGroup({ assignee_id: 'u1', assignee: { name: 'Ada' }, agent_assignees: [fixer] }), { key: 'u1', label: 'Ada' });
	// The lowest agent ID names the group, as the server orders pages.
	assert.deepEqual(assigneeGroup({ assignee_id: null, agent_assignees: [fixer, writer] }), { key: 'agent:a-writer', label: 'Docs writer' });
	assert.deepEqual(assigneeGroup({ assignee_id: null }), { key: 'unassigned', label: 'unassigned' });
	assert.equal(groupAgentId('agent:a-writer'), 'a-writer');
	assert.equal(groupAgentId('u1'), null);
	assert.equal(groupAgentId('unassigned'), null);
});
