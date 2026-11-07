import type { Component } from 'svelte';
import type { ConversationState } from '../conversation-state.svelte';
import type { TaskBoard } from '../task-board.svelte';
import type { AgentQueueItem } from '$lib/api/agent-tasks';

// What every side panel receives. A panel reads the open conversation and
// its task board, never owns them, so panels can be added, closed or moved
// between the inline column and the drawer without losing state.
export interface PanelContext {
	slug: string;
	chat: ConversationState;
	board: TaskBoard | null;
	// The person's agent work across conversations (polled), for overviews.
	queue: AgentQueueItem[];
	close: () => void;
	open: (id: string) => void;
	// Asks the chat composer to show text, e.g. a request to open a pull request.
	draft: (text: string) => void;
}

/**
 * One side panel. Panels are registered in AssistantChat's list; each is one
 * entry, for example the workflow panel (KUA-95):
 *
 *   { id: 'workflow', label: m['assistant.panel_workflow'], icon: Workflow,
 *     component: WorkflowPanel }
 *
 * The host renders the active panel's component with the PanelContext, as a
 * resizable column on wide screens and as a drawer on narrow ones.
 */
export interface PanelDefinition {
	id: string;
	label: () => string;
	icon: Component;
	component: Component<{ context: PanelContext }>;
	// Hidden from the panel switcher (and closed) while this returns false.
	available?: () => boolean;
}

/** Which side panel is open; the choice is remembered per person. */
export class PanelHost {
	active = $state<string | null>(null);
	constructor(
		private readonly storageKey: string,
		initial: string | null
	) {
		let saved: string | null | undefined;
		try {
			saved = localStorage.getItem(storageKey);
		} catch {
			saved = undefined;
		}
		this.active = saved === undefined || saved === null ? initial : saved === 'closed' ? null : saved;
	}
	open(id: string) {
		this.active = id;
		this.remember(id);
	}
	close() {
		this.active = null;
		this.remember('closed');
	}
	toggle(id: string) {
		if (this.active === id) this.close();
		else this.open(id);
	}
	private remember(value: string) {
		try {
			// Transient panels (a diff) are not restored on the next visit.
			if (value !== 'diff') localStorage.setItem(this.storageKey, value);
		} catch {
			/* The panel still works without storage. */
		}
	}
}
