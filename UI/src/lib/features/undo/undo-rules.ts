import { isEditableTarget } from '../../utils/keyboard.ts';

/** How long an Undo action stays offered after a change (the toast's lifetime). */
export const UNDO_WINDOW_MS = 8000;

type UndoKey = Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey' | 'defaultPrevented' | 'isComposing' | 'target'>;

/**
 * Cmd+Z / Ctrl+Z outside text fields. Inputs, textareas and rich editors keep
 * their native undo; Shift (redo) and Alt chords are not undo.
 */
export function isUndoShortcut(e: UndoKey): boolean {
	if (e.defaultPrevented || e.isComposing || e.shiftKey || e.altKey) return false;
	if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== 'z') return false;
	return !isEditableTarget(e.target);
}

export type UndoFailure = 'edited' | 'expired' | 'unavailable' | 'other';

/** Why the server refused an undo or restore, from its error response. */
export function undoFailure(error: unknown): UndoFailure {
	const code = (error as { error?: { code?: unknown } } | null)?.error?.code;
	if (code === 'EDITED_MEANWHILE') return 'edited';
	if (code === 'GRACE_EXPIRED') return 'expired';
	if (code === 'UNDO_UNAVAILABLE') return 'unavailable';
	return 'other';
}
