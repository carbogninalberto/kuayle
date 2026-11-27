import { siClaude, siKimi, siOpencode, siQwen } from 'simple-icons';

// A single-path 24×24 SVG logo and its brand colour. `evenodd` is for marks with
// holes cut out of them.
type AgentLogoPath = { path: string; hex: string; evenodd?: boolean };

// A multi-colour mark whose colours follow the theme: each layer has a light-
// and a dark-theme fill.
type AgentLogoLayers = { viewBox: string; layers: { path: string; light: string; dark: string }[] };

// Codex: LobeHub Icons (MIT), the monochrome mark. Near-black, so AgentLogo draws
// it in the theme's foreground colour.
const codexIcon: AgentLogoPath = {
	hex: '000000',
	evenodd: true,
	path: 'M8.086.457a6.105 6.105 0 013.046-.415c1.333.153 2.521.72 3.564 1.7a.117.117 0 00.107.029c1.408-.346 2.762-.224 4.061.366l.063.03.154.076c1.357.703 2.33 1.77 2.918 3.198.278.679.418 1.388.421 2.126a5.655 5.655 0 01-.18 1.631.167.167 0 00.04.155 5.982 5.982 0 011.578 2.891c.385 1.901-.01 3.615-1.183 5.14l-.182.22a6.063 6.063 0 01-2.934 1.851.162.162 0 00-.108.102c-.255.736-.511 1.364-.987 1.992-1.199 1.582-2.962 2.462-4.948 2.451-1.583-.008-2.986-.587-4.21-1.736a.145.145 0 00-.14-.032c-.518.167-1.04.191-1.604.185a5.924 5.924 0 01-2.595-.622 6.058 6.058 0 01-2.146-1.781c-.203-.269-.404-.522-.551-.821a7.74 7.74 0 01-.495-1.283 6.11 6.11 0 01-.017-3.064.166.166 0 00.008-.074.115.115 0 00-.037-.064 5.958 5.958 0 01-1.38-2.202 5.196 5.196 0 01-.333-1.589 6.915 6.915 0 01.188-2.132c.45-1.484 1.309-2.648 2.577-3.493.282-.188.55-.334.802-.438.286-.12.573-.22.861-.304a.129.129 0 00.087-.087A6.016 6.016 0 015.635 2.31C6.315 1.464 7.132.846 8.086.457zm-.804 7.85a.848.848 0 00-1.473.842l1.694 2.965-1.688 2.848a.849.849 0 001.46.864l1.94-3.272a.849.849 0 00.007-.854l-1.94-3.393zm5.446 6.24a.849.849 0 000 1.695h4.848a.849.849 0 000-1.696h-4.848z'
};

// Pi: its three-block logo. The brand colours are used on dark themes; light
// themes get deeper tones of the same hues so the blocks keep their contrast.
const piLayers: AgentLogoLayers = {
	viewBox: '165.29 165.29 469.43 469.43',
	layers: [
		{ path: 'M165.29 165.29H517.36V400H400V282.65H165.29Z', light: '#D6523C', dark: '#F09082' },
		{ path: 'M165.29 282.65H282.65V400H400V517.36H282.65V634.72H165.29Z', light: '#2B7BA3', dark: '#4D9ABF' },
		{ path: 'M517.36 400H634.72V634.72H517.36Z', light: '#C98A0B', dark: '#F1BE58' }
	]
};

// The coding agents Kuayle can run. Logos come from Simple Icons (CC0) where the
// brand publishes one, else from the marks above or an image under static/agents
// (the project's GitHub avatar). An agent with none, or whose image fails to
// load, uses a monogram tile.
export interface AgentInfo {
	id: string;
	name: string;
	// Single-path logo, when available.
	icon?: AgentLogoPath;
	// A multi-colour logo that follows the theme.
	logo?: AgentLogoLayers;
	// A logo image served from static/.
	image?: string;
	monogram: string;
	// The model server protocol the harness speaks, if it can use one.
	protocol?: 'openai' | 'anthropic';
}

export const agents: AgentInfo[] = [
	{ id: 'codex', name: 'Codex', icon: codexIcon, monogram: 'Cx', protocol: 'openai' },
	{ id: 'claude-code', name: 'Claude Code', icon: siClaude, monogram: 'CC', protocol: 'anthropic' },
	{ id: 'opencode', name: 'OpenCode', icon: siOpencode, monogram: 'OC' },
	{ id: 'pi', name: 'Pi', logo: piLayers, monogram: 'Pi' },
	{ id: 'qwen-code', name: 'Qwen Code', icon: siQwen, monogram: 'Qw' },
	{ id: 'crush', name: 'Crush', image: '/agents/crush.png', monogram: 'Cr' },
	{ id: 'kimi', name: 'Kimi', icon: siKimi, monogram: 'Ki' },
	{ id: 'goose', name: 'Goose', image: '/agents/goose.png', monogram: 'Go' }
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
