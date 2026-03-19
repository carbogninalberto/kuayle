type KeyHandler = (e: KeyboardEvent) => void;

interface Shortcut {
	key: string;
	ctrl?: boolean;
	meta?: boolean;
	shift?: boolean;
	handler: KeyHandler;
}

/**
 * Simple keyboard handler for single-key shortcuts.
 * Ignores events when focused on inputs/textareas.
 */
export function createKeyboardHandler(shortcuts: Shortcut[]): KeyHandler {
	return (e: KeyboardEvent) => {
		const target = e.target as HTMLElement;
		if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
			return;
		}

		for (const shortcut of shortcuts) {
			const metaOrCtrl = shortcut.meta || shortcut.ctrl;
			if (
				e.key.toLowerCase() === shortcut.key.toLowerCase() &&
				(!metaOrCtrl || e.metaKey || e.ctrlKey) &&
				(!shortcut.shift || e.shiftKey)
			) {
				e.preventDefault();
				shortcut.handler(e);
				return;
			}
		}
	};
}

// --- Key Sequence Engine ---

interface SequenceShortcut {
	/** Key sequence, e.g. ['g', 'i'] for G then I */
	keys: string[];
	handler: () => void;
	/** Description for the help dialog */
	label: string;
	/** Category for grouping */
	category: string;
}

interface SingleShortcut {
	key: string;
	ctrl?: boolean;
	meta?: boolean;
	shift?: boolean;
	handler: () => void;
	label: string;
	category: string;
}

export type ShortcutDef = {
	keys?: string[];
	key?: string;
	ctrl?: boolean;
	meta?: boolean;
	shift?: boolean;
	handler: () => void;
	label: string;
	category: string;
};

const SEQUENCE_TIMEOUT = 500; // ms

export function createShortcutEngine(defs: ShortcutDef[]) {
	let pendingKeys: string[] = [];
	let pendingTimer: ReturnType<typeof setTimeout> | null = null;

	const sequences = defs.filter((d) => d.keys && d.keys.length > 0);
	const singles = defs.filter((d) => d.key && !d.keys);

	function resetPending() {
		pendingKeys = [];
		if (pendingTimer) {
			clearTimeout(pendingTimer);
			pendingTimer = null;
		}
	}

	function handler(e: KeyboardEvent) {
		const target = e.target as HTMLElement;
		if (
			target.tagName === 'INPUT' ||
			target.tagName === 'TEXTAREA' ||
			target.isContentEditable
		) {
			return;
		}

		const key = e.key.toLowerCase();

		// Check single-key shortcuts with modifiers first
		for (const s of singles) {
			const metaOrCtrl = s.meta || s.ctrl;
			if (
				key === s.key!.toLowerCase() &&
				(!metaOrCtrl || e.metaKey || e.ctrlKey) &&
				(!s.shift || e.shiftKey)
			) {
				// Don't match single-key shortcuts if we're mid-sequence (unless they have modifiers)
				if (pendingKeys.length > 0 && !metaOrCtrl) continue;
				e.preventDefault();
				resetPending();
				s.handler();
				return;
			}
		}

		// Skip if modifiers are held (they shouldn't be part of sequences)
		if (e.metaKey || e.ctrlKey || e.altKey) return;

		// Build up key sequence
		pendingKeys.push(key);

		// Reset timer
		if (pendingTimer) clearTimeout(pendingTimer);
		pendingTimer = setTimeout(resetPending, SEQUENCE_TIMEOUT);

		// Check for matching sequences
		for (const seq of sequences) {
			if (seq.keys!.length === pendingKeys.length) {
				const matches = seq.keys!.every((k, i) => k.toLowerCase() === pendingKeys[i]);
				if (matches) {
					e.preventDefault();
					resetPending();
					seq.handler();
					return;
				}
			}
		}

		// Check if any sequence could still match (prefix check)
		const couldMatch = sequences.some((seq) => {
			if (seq.keys!.length <= pendingKeys.length) return false;
			return seq.keys!.slice(0, pendingKeys.length).every((k, i) => k.toLowerCase() === pendingKeys[i]);
		});

		if (!couldMatch) {
			resetPending();
		}
	}

	return { handler, defs };
}
