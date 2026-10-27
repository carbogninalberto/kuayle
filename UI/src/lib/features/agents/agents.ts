import { siClaude, siKimi, siOpencode, siQwen } from 'simple-icons';

// The coding agents Kuayle can run. Logos come from Simple Icons (CC0) where
// the brand publishes one; the rest use a monogram tile.
export interface AgentInfo {
	id: string;
	name: string;
	// Single-path 24×24 SVG logo and its brand colour, when available.
	icon?: { path: string; hex: string };
	monogram: string;
	// The model server protocol the harness speaks, if it can use one.
	protocol?: 'openai' | 'anthropic';
}

export const agents: AgentInfo[] = [
	{ id: 'codex', name: 'Codex', monogram: 'Cx', protocol: 'openai' },
	{ id: 'claude-code', name: 'Claude Code', icon: siClaude, monogram: 'CC', protocol: 'anthropic' },
	{ id: 'opencode', name: 'OpenCode', icon: siOpencode, monogram: 'OC' },
	{ id: 'qwen-code', name: 'Qwen Code', icon: siQwen, monogram: 'Qw' },
	{ id: 'crush', name: 'Crush', monogram: 'Cr' },
	{ id: 'kimi', name: 'Kimi', icon: siKimi, monogram: 'Ki' },
	{ id: 'goose', name: 'Goose', monogram: 'Go' }
];

export function agentInfo(id: string, displayName?: string): AgentInfo {
	return (
		agents.find((agent) => agent.id === id) ?? {
			id,
			name: displayName ?? id,
			monogram: (displayName ?? id).slice(0, 2)
		}
	);
}
