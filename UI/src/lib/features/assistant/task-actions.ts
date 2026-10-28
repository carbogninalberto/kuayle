import type { Component } from 'svelte';

// One task action, rendered as a labelled button in the task row and as an
// icon button with a tooltip in the compact sticky header.
export type TaskAction = {
	id: string;
	label: string;
	icon: Component;
	onclick?: () => void;
	href?: string;
	// Opens outside the app (pull request); internal links stay in the tab.
	external?: boolean;
	disabled?: boolean;
	title?: string;
	// Where the action sits in the task row: labelled main actions, icon-only
	// tools, or the overflow menu. The compact header shows all as icons.
	group: 'main' | 'tools' | 'more';
	// The main next step is emphasised; destructive actions are marked.
	variant?: 'primary' | 'danger';
	// A tool may open a menu instead (the task app); its dot shows a state.
	menu?: TaskMenuEntry[];
	onopen?: () => void;
	indicator?: 'starting' | 'running' | 'failed';
};

export type TaskMenuEntry =
	| { kind: 'note'; id: string; text: string; tone?: 'error' }
	| { kind: 'separator'; id: string }
	| {
			kind: 'item';
			id: string;
			label: string;
			icon: Component;
			// Secondary text after the label, e.g. the port number.
			hint?: string;
			onclick: () => void;
			disabled?: boolean;
			variant?: 'danger';
	  };

// The newest task's header, shown pinned above the chat once its row scrolls away.
export type TaskBar = {
	taskId: string;
	identifier?: string;
	title: string;
	status: string;
	badgeClass: string;
	actions: TaskAction[];
};
