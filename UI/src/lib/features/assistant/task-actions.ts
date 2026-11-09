import type { Component } from 'svelte';

// One task action: the next steps are labelled buttons on the task card; every
// other action is a labelled item of the card's single "More actions" menu.
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
	// Where the action sits: a button in the card's action row ('main'), or the
	// "More actions" menu, tools first and then the rest ('more', e.g. cancel).
	group: 'main' | 'tools' | 'more';
	// The main next step is emphasised; destructive actions are marked.
	variant?: 'primary' | 'danger';
	// A tool may open a submenu instead (the task app); its dot shows a state.
	menu?: TaskMenuEntry[];
	onopen?: () => void;
	// The action focuses something else (the chat composer, a terminal): the
	// menu runs it after closing and does not hand focus back to its trigger.
	movesFocus?: boolean;
	indicator?: 'starting' | 'running' | 'failed';
};

export type TaskMenuEntry =
	| { kind: 'note'; id: string; text: string; tone?: 'error' }
	| { kind: 'separator'; id: string }
	| {
			kind: 'item';
			id: string;
			label: string;
			icon?: Component;
			// An agent's logo instead of an icon (the review harness).
			logo?: string;
			// Secondary text after the label, e.g. the port number.
			hint?: string;
			onclick: () => void;
			disabled?: boolean;
			variant?: 'danger';
			movesFocus?: boolean;
	  };
