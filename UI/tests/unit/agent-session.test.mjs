import { test } from 'node:test';
import assert from 'node:assert/strict';
import { agentSessionCommand } from '../../src/lib/features/assistant/agent-session.ts';

const task = '420da554-1083-49dd-b8da-ba07bee30564';

test('codex resumes the task session from its worktree', () => {
	const command = agentSessionCommand('codex', task);
	assert.ok(command.includes(`ln -sfn /workspace/.kuayle/agent-sessions/${task}/codex ~/.codex/sessions`));
	assert.ok(command.endsWith('&& codex resume --last\r'));
});

test('claude continues the task session', () => {
	assert.ok(agentSessionCommand('claude-code', task).endsWith('&& claude --continue\r'));
});

test('unsupported agents and unsafe ids get no command', () => {
	assert.equal(agentSessionCommand('opencode', task), undefined);
	assert.equal(agentSessionCommand('codex', '$(rm -rf ~)'), undefined);
});

test('sign-in starts each agent’s own login in the terminal', async () => {
	const { signInCommand } = await import('../../src/lib/features/assistant/agent-session.ts');
	assert.equal(signInCommand('codex'), 'codex login --device-auth\r');
	assert.equal(signInCommand('claude-code'), 'claude /login\r');
	assert.equal(signInCommand('goose'), undefined);
});
