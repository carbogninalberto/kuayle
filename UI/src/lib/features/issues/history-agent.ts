// Display names for agents that acted on a user's behalf in issue history.
const agentNames: Record<string, string> = {
	assistant: 'Assistant',
	codex: 'Codex',
	'claude-code': 'Claude Code',
	opencode: 'OpenCode',
	'qwen-code': 'Qwen Code',
	crush: 'Crush',
	kimi: 'Kimi',
	goose: 'Goose'
};

export function agentName(agent: string): string {
	return agentNames[agent] ?? agent;
}

/**
 * Who a history entry names beside the change. A task run as an agent
 * identity (an assigned or mentioned agent) is shown under that identity,
 * "on behalf of" the member it acted for; other agents keep "via <harness>".
 */
export function historyActor(
	entry: { agent?: string | null; agent_name?: string | null; user_id?: string | null },
	members: { user_id: string; name?: string; email?: string }[]
): { kind: 'identity'; agent: string; user: string } | { kind: 'via'; agent: string } | null {
	if (entry.agent_name) {
		const member = members.find((candidate) => candidate.user_id === entry.user_id);
		const user = member?.name || member?.email;
		return user ? { kind: 'identity', agent: entry.agent_name, user } : { kind: 'via', agent: entry.agent_name };
	}
	return entry.agent ? { kind: 'via', agent: agentName(entry.agent) } : null;
}
