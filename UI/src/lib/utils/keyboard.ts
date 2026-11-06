type KeyHandler = (e: KeyboardEvent) => void;

interface Shortcut {
	key: string;
	ctrl?: boolean;
	meta?: boolean;
	shift?: boolean;
	handler: KeyHandler;
}

const EDITABLE_SELECTOR = 'input, textarea, select, [contenteditable]:not([contenteditable="false"])';

/** True when the key or paste belongs to a text field, select or rich/terminal editor. */
export function isEditableTarget(target: EventTarget | null): boolean {
	const el = target as HTMLElement | null;
	if (!el || typeof el.tagName !== 'string') return false;
	if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable) return true;
	return typeof el.closest === 'function' && el.closest(EDITABLE_SELECTOR) !== null;
}

/** Events that a global shortcut must leave to the focused control or the browser. */
function ignoredEvent(e: KeyboardEvent): boolean {
	return e.defaultPrevented || e.isComposing || isEditableTarget(e.target);
}

/**
 * Plain shortcuts never fire with Cmd, or with Ctrl/Alt on letters, digits and named
 * keys, so browser chords such as copy (Ctrl/Cmd+C) and paste keep their native
 * behaviour. Printed symbols still match when typed with AltGr (see isPrintedSymbol).
 */
function matchesModifiers(e: KeyboardEvent, s: { ctrl?: boolean; meta?: boolean; shift?: boolean }): boolean {
	if (s.meta || s.ctrl) {
		if (!(e.metaKey || e.ctrlKey)) return false;
	} else if (e.metaKey || (isPrintedSymbol(e.key) ? e.ctrlKey && !e.altKey : e.ctrlKey || e.altKey)) {
		return false;
	}
	return !s.shift || e.shiftKey;
}

/**
 * Symbols such as `/` and `?` are already the result of the keyboard layout. Many
 * layouts type them with AltGr, which Windows reports as Ctrl+Alt (and macOS as
 * Option), so those modifiers must not hide a symbol shortcut. Letters, digits
 * and named keys keep rejecting them.
 */
function isPrintedSymbol(key: string): boolean {
	return [...key].length === 1 && !/[\p{L}\p{N}\s]/u.test(key);
}

/**
 * Simple keyboard handler for single-key shortcuts.
 * Ignores events when focused on editable controls.
 */
export function createKeyboardHandler(shortcuts: Shortcut[]): KeyHandler {
	return (e: KeyboardEvent) => {
		if (ignoredEvent(e)) return;

		for (const shortcut of shortcuts) {
			if (e.key.toLowerCase() === shortcut.key.toLowerCase() && matchesModifiers(e, shortcut)) {
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
	/** When it returns false the shortcut is unavailable and the key is left alone. */
	when?: () => boolean;
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
		if (ignoredEvent(e)) return;

		const key = e.key.toLowerCase();

		// Check single-key shortcuts with modifiers first
		for (const s of singles) {
			const metaOrCtrl = s.meta || s.ctrl;
			if (key === s.key!.toLowerCase() && matchesModifiers(e, s)) {
				// Don't match single-key shortcuts if we're mid-sequence (unless they have modifiers)
				if (pendingKeys.length > 0 && !metaOrCtrl) continue;
				if (s.when && !s.when()) continue;
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
