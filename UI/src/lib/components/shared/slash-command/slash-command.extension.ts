import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from 'prosemirror-state';

export interface SlashCommandState {
	active: boolean;
	query: string;
	range: { from: number; to: number } | null;
	x: number;
	y: number;
}

export const slashCommandPluginKey = new PluginKey('slashCommand');

export function createSlashCommandExtension(callbacks: {
	onStateChange: (state: SlashCommandState) => void;
	onNavigate: (direction: 'up' | 'down') => void;
	onSelect: () => void;
}) {
	return Extension.create({
		name: 'slashCommand',

		addProseMirrorPlugins() {
			const editor = this.editor;

			let active = false;
			let slashFrom = 0;

			function deactivate() {
				if (!active) return;
				active = false;
				callbacks.onStateChange({ active: false, query: '', range: null, x: 0, y: 0 });
			}

			function emitState(view: any) {
				const to = view.state.selection.from;
				const query = view.state.doc.textBetween(slashFrom + 1, to, '\0');
				try {
					const coords = view.coordsAtPos(slashFrom);
					callbacks.onStateChange({
						active: true,
						query,
						range: { from: slashFrom, to },
						x: coords.left,
						y: coords.bottom + 4
					});
				} catch {
					deactivate();
				}
			}

			return [
				new Plugin({
					key: slashCommandPluginKey,

					props: {
						handleTextInput(view, from, _to, text) {
							if (text === '/' && !active) {
								// Check if at start of block or preceded by whitespace
								const $pos = view.state.doc.resolve(from);
								const isStartOfBlock = $pos.parentOffset === 0;
								const charBefore = from > 0 ? view.state.doc.textBetween(from - 1, from) : '';
								const isAfterSpace = charBefore === ' ' || charBefore === '\n';

								if (isStartOfBlock || isAfterSpace) {
									// Activate after the "/" is inserted (next tick)
									active = true;
									slashFrom = from;
									setTimeout(() => {
										if (active) emitState(view);
									}, 0);
								}
							} else if (active) {
								// User is typing after "/" — update on next tick after the char is inserted
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
								// If we'd backspace past the "/" character, deactivate
								if (cursorPos <= slashFrom + 1) {
									deactivate();
									// Let ProseMirror handle the actual deletion
									return false;
								}
								// Otherwise, update state after deletion
								setTimeout(() => {
									if (active) emitState(view);
								}, 0);
								return false;
							}
							if (event.key === ' ') {
								// Space dismisses if no results would match
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
								// Check if cursor is still in the slash range
								const cursorPos = view.state.selection.from;
								if (cursorPos < slashFrom || cursorPos > slashFrom + 50) {
									deactivate();
									return;
								}
								// Verify the "/" is still there
								try {
									const char = view.state.doc.textBetween(slashFrom, slashFrom + 1);
									if (char !== '/') {
										deactivate();
									}
								} catch {
									deactivate();
								}
							},
							destroy() {
								deactivate();
							}
						};
					}
				})
			];
		}
	});
}
