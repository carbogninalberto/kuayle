import { getContext, setContext } from 'svelte';

export interface TerminalTab {
	id: string;
	slug: string;
	machineId: string;
	machineName: string;
	checkoutId?: string;
	checkoutLabel?: string;
	sessionName?: string;
	runtimeTitle?: string;
}

class TerminalDockState {
	tabs = $state<TerminalTab[]>([]);
	activeTabId = $state<string | null>(null);
	expanded = $state(false);
	fullscreen = $state(false);
	height = $state(320);

	#slug = '';

	/** The pinned Agents tab id; it is never a terminal session. */
	static readonly AGENTS = 'agents';

	/** Whether this browser has seen agent work in the workspace; only then
	 * does the dock poll the agent queue. */
	agentsTracked = $state(false);

	get slug() {
		return this.#slug;
	}

	trackAgents() {
		if (this.agentsTracked || !this.#slug) return;
		this.agentsTracked = true;
		try {
			localStorage.setItem(`kuayle.agents.${this.#slug}`, '1');
		} catch {
			// The dock still shows agent work for this page view.
		}
	}

	showAgents() {
		this.activeTabId = TerminalDockState.AGENTS;
		this.expanded = true;
	}

	setWorkspace(slug: string) {
		if (slug !== this.#slug) {
			this.tabs = [];
			this.activeTabId = null;
			this.expanded = false;
			this.fullscreen = false;
			this.#slug = slug;
			try {
				this.agentsTracked = localStorage.getItem(`kuayle.agents.${slug}`) === '1';
			} catch {
				this.agentsTracked = false;
			}
		}
	}

	open(tab: Omit<TerminalTab, 'id'>) {
		const id = crypto.randomUUID();
		const newTab: TerminalTab = { ...tab, id };
		this.tabs = [...this.tabs, newTab];
		this.activeTabId = id;
		this.expanded = true;
		return id;
	}

	closeTab(id: string) {
		const idx = this.tabs.findIndex((t) => t.id === id);
		if (idx === -1) return;
		this.tabs = this.tabs.filter((t) => t.id !== id);
		if (this.activeTabId === id) {
			if (this.tabs.length > 0) {
				const newIdx = Math.min(idx, this.tabs.length - 1);
				this.activeTabId = this.tabs[newIdx].id;
			} else {
				this.activeTabId = null;
				this.expanded = false;
				this.fullscreen = false;
			}
		}
	}

	setActiveTab(id: string) {
		this.activeTabId = id;
		this.expanded = true;
	}

	setRuntimeTitle(id: string, title: string) {
		const tab = this.tabs.find((item) => item.id === id);
		const runtimeTitle = title || undefined;
		if (!tab || tab.runtimeTitle === runtimeTitle) return;
		this.tabs = this.tabs.map((item) => item.id === id ? { ...item, runtimeTitle } : item);
	}

	toggle() {
		if (this.expanded) this.fullscreen = false;
		this.expanded = !this.expanded;
	}

	toggleFullscreen() {
		if (this.expanded) this.fullscreen = !this.fullscreen;
	}

	closeAll() {
		this.tabs = [];
		this.activeTabId = null;
		this.expanded = false;
		this.fullscreen = false;
	}

	setHeight(h: number) {
		this.height = h;
	}
}

const SYMBOL_KEY = 'terminal-dock';

export function setTerminalDock(): TerminalDockState {
	return setContext(Symbol.for(SYMBOL_KEY), new TerminalDockState());
}

export function useTerminalDock(): TerminalDockState {
	return getContext<TerminalDockState>(Symbol.for(SYMBOL_KEY));
}
