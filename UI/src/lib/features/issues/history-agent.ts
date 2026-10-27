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
