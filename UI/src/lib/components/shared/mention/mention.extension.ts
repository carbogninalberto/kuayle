import { Node, mergeAttributes } from '@tiptap/core';
import { Plugin, PluginKey } from 'prosemirror-state';

export interface MentionUser {
	id: string;
	name: string;
	email: string;
}

export interface MentionState {
	active: boolean;
	query: string;
	range: { from: number; to: number } | null;
	x: number;
	y: number;
}

export const mentionPluginKey = new PluginKey('mention');

/** Custom TipTap node for @mentions */
export const MentionNode = Node.create({
	name: 'mention',
	group: 'inline',
	inline: true,
	selectable: false,
	atom: true,

	addAttributes() {
		return {
			id: {
				default: null,
				parseHTML: (el: HTMLElement) => el.getAttribute('data-id'),
				renderHTML: (attrs: Record<string, any>) => ({ 'data-id': attrs.id })
			},
			label: {
				default: null,
				parseHTML: (el: HTMLElement) => el.getAttribute('data-label'),
				renderHTML: (attrs: Record<string, any>) => ({ 'data-label': attrs.label })
			}
		};
	},

	parseHTML() {
		return [{ tag: 'span[data-type="mention"]' }];
	},

	renderHTML({ node, HTMLAttributes }) {
		return [
			'span',
			mergeAttributes(HTMLAttributes, {
				'data-type': 'mention',
				class: 'mention'
			}),
			`@${node.attrs.label}`
		];
	}
});

/** ProseMirror plugin that detects "@" and manages mention state */
export function createMentionPlugin(callbacks: {
	onStateChange: (state: MentionState) => void;
	onNavigate: (direction: 'up' | 'down') => void;
	onSelect: () => void;
}) {
	let active = false;
	let atFrom = 0;

	function deactivate() {
		if (!active) return;
		active = false;
		callbacks.onStateChange({ active: false, query: '', range: null, x: 0, y: 0 });
	}

	function emitState(view: any) {
		const to = view.state.selection.from;
		const query = view.state.doc.textBetween(atFrom + 1, to, '\0');
		try {
			const coords = view.coordsAtPos(atFrom);
			callbacks.onStateChange({
				active: true,
				query,
				range: { from: atFrom, to },
				x: coords.left,
				y: coords.bottom + 4
			});
		} catch {
			deactivate();
		}
	}

	return new Plugin({
		key: mentionPluginKey,

		props: {
			handleTextInput(view, from, _to, text) {
				if (text === '@' && !active) {
					const $pos = view.state.doc.resolve(from);
					const isStartOfBlock = $pos.parentOffset === 0;
					const charBefore = from > 0 ? view.state.doc.textBetween(from - 1, from) : '';
					const isAfterSpace = charBefore === ' ' || charBefore === '\n';

					if (isStartOfBlock || isAfterSpace) {
						active = true;
						atFrom = from;
						setTimeout(() => {
							if (active) emitState(view);
						}, 0);
					}
				} else if (active) {
					setTimeout(() => {
						if (active) emitState(view);
					}, 0);
				}
				return false;
			},

			handleKeyDown(view, event) {
				if (!active) return false;
				// Let modifier combos (e.g. Mod+Enter for submit) pass through
				if (event.metaKey || event.ctrlKey) return false;

				if (event.key === 'ArrowDown') {
					callbacks.onNavigate('down');
					return true;
				}
				if (event.key === 'ArrowUp') {
					callbacks.onNavigate('up');
					return true;
				}
				if (event.key === 'Enter') {
					event.preventDefault();
					callbacks.onSelect();
					return true;
				}
				if (event.key === 'Escape') {
					deactivate();
					return true;
				}
				if (event.key === 'Backspace') {
					const cursorPos = view.state.selection.from;
					if (cursorPos <= atFrom + 1) {
						deactivate();
						return false;
					}
					setTimeout(() => {
						if (active) emitState(view);
					}, 0);
					return false;
				}
				if (event.key === ' ') {
					deactivate();
					return false;
				}
				return false;
			},

			handleClick() {
				if (active) deactivate();
				return false;
			}
		},

		view() {
			return {
				update(view) {
					if (!active) return;
					const cursorPos = view.state.selection.from;
					if (cursorPos < atFrom || cursorPos > atFrom + 50) {
						deactivate();
						return;
					}
					try {
						const char = view.state.doc.textBetween(atFrom, atFrom + 1);
						if (char !== '@') deactivate();
					} catch {
						deactivate();
					}
				},
				destroy() {
					deactivate();
				}
			};
		}
	});
}
