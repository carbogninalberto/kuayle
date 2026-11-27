import { siClaude, siKimi, siOpencode, siQwen } from 'simple-icons';

type AgentLogoPath = { path: string; hex: string };

// Logos for agents Simple Icons has no mark for: single-path 24×24 glyphs drawn
// by AgentLogo exactly like the Simple Icons ones. Near-black `hex` values
// render in the theme's foreground colour, so they stay visible in dark themes.

// Cloud with a terminal prompt cut out of it.
const codexIcon: AgentLogoPath = {
	hex: '000000',
	path: 'M7 19a5 5 0 0 1-.9-9.92A6.5 6.5 0 0 1 18.6 9.3 4.9 4.9 0 0 1 17.5 19ZM8.2 9.6 7.3 10.8l1.6 1.2-1.6 1.2.9 1.2 3.2-2.4ZM12.6 14v1.5H16V14Z'
};

// The Greek letter π.
const piIcon: AgentLogoPath = {
	hex: '000000',
	path: 'M3 5h18v3h-3.5v8.2c0 .6.3.8.8.8H20v2.5h-2c-2.2 0-3.5-1.1-3.5-3.3V8h-3.2v11H8.2V8H3Z'
};

// A goose in profile, beak to the right.
const gooseIcon: AgentLogoPath = {
	hex: '000000',
	path: 'M21 5.2l-2.8-.9C17.6 2.9 16.4 2 15.2 2c-1.5 0-2.6 1.1-2.6 2.6 0 1.2.4 2 1 2.8.2 1.6-.4 2.9-2.1 3.4C7 11.5 4 14 2.5 17l3 2.5C8 21 9 21 11 21h5c3.5 0 5.5-2.5 5.5-5.2 0-2.8-1.9-4.8-4.5-4.8-.6-.1-.8-.5-.7-1V8c.1-1 .7-1.7 1.7-2.1ZM15.2 3.7a.7.7 0 1 1 0 1.4.7.7 0 0 1 0-1.4Z'
};

// A faceted gem, the "crush".
const crushIcon: AgentLogoPath = {
	hex: '6B50FF',
	path: 'M7 3h10l5 6-10 12L2 9Zm.9 2L5 8.5h14L16.1 5Zm-1.9 5.5 6 7.2 6-7.2Z'
};

// The coding agents Kuayle can run. Logos come from Simple Icons (CC0) where
// the brand publishes one, else from the glyphs above. They are inline paths, so
// no image asset can fail to load; an agent with no logo uses a monogram tile.
export interface AgentInfo {
	id: string;
	name: string;
	// Single-path 24×24 SVG logo and its brand colour, when available.
	icon?: AgentLogoPath;
	monogram: string;
	// The model server protocol the harness speaks, if it can use one.
	protocol?: 'openai' | 'anthropic';
}

export const agents: AgentInfo[] = [
	{ id: 'codex', name: 'Codex', icon: codexIcon, monogram: 'Cx', protocol: 'openai' },
	{ id: 'claude-code', name: 'Claude Code', icon: siClaude, monogram: 'CC', protocol: 'anthropic' },
	{ id: 'opencode', name: 'OpenCode', icon: siOpencode, monogram: 'OC' },
	{ id: 'pi', name: 'Pi', icon: piIcon, monogram: 'Pi' },
	{ id: 'qwen-code', name: 'Qwen Code', icon: siQwen, monogram: 'Qw' },
	{ id: 'crush', name: 'Crush', icon: crushIcon, monogram: 'Cr' },
	{ id: 'kimi', name: 'Kimi', icon: siKimi, monogram: 'Ki' },
	{ id: 'goose', name: 'Goose', icon: gooseIcon, monogram: 'Go' }
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
