// Resuming an agent's native session in a task terminal. The task's
// transcripts live on the workspace volume; linking them into the terminal's
// home lets the CLI's own "latest session" pick the agent's conversation,
// which is keyed by the same worktree directory the terminal opens in.
const sessionRoot = '/workspace/.kuayle/agent-sessions';
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** Tasks still running hold the session; finished ones have it purged. */
export const resumableStates = ['awaiting_review', 'failed', 'blocked'];

export function agentSessionCommand(provider: string, taskId: string): string | undefined {
	if (!uuidPattern.test(taskId)) return undefined;
	const dir = `${sessionRoot}/${taskId}`;
	// Only replace a missing entry or an earlier link, never a real directory.
	const link = (source: string, target: string) =>
		`mkdir -p "$(dirname ${target})" && { [ ! -e ${target} ] || [ -L ${target} ]; } && ln -sfn ${source} ${target}`;
	switch (provider) {
		case 'codex':
			return `${link(`${dir}/codex`, '~/.codex/sessions')} && codex resume --last\r`;
		case 'claude-code':
			return `${link(`${dir}/claude`, '~/.claude/projects')} && claude --continue\r`;
		default:
			return undefined;
	}
}

// Starts the agent's own sign-in in the machine's terminal. Codex uses the
// device flow, which works without a browser on the machine.
export function signInCommand(provider: string): string | undefined {
	switch (provider) {
		case 'codex':
			return 'codex login --device-auth\r';
		case 'claude-code':
			return 'claude /login\r';
		default:
			return undefined;
	}
}
