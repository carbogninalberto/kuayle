import { getContext, setContext } from 'svelte';

export interface TerminalTab {
	id: string;
	slug: string;
	machineId: string;
	machineName: string;
	checkoutId?: string;
	checkoutLabel?: string;
	sessionName?: string;
}

class TerminalDockState {
	tabs = $state<TerminalTab[]>([]);
	activeTabId = $state<string | null>(null);
	expanded = $state(false);
	height = $state(320);

	#slug = '';

	setWorkspace(slug: string) {
		if (slug !== this.#slug) {
			this.tabs = [];
			this.activeTabId = null;
			this.expanded = false;
			this.#slug = slug;
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
			}
		}
	}

	setActiveTab(id: string) {
		this.activeTabId = id;
		this.expanded = true;
	}

	toggle() {
		this.expanded = !this.expanded;
	}

	closeAll() {
		this.tabs = [];
		this.activeTabId = null;
		this.expanded = false;
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
