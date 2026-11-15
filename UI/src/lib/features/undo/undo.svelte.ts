import { toast } from 'svelte-sonner';
import AppToast from '$lib/components/shared/AppToast.svelte';
import { m } from '$lib/features/i18n/messages';
import { appToast } from '$lib/features/toast/toast';
import { isUndoShortcut, undoFailure, UNDO_WINDOW_MS } from './undo-rules';

/**
 * One Undo is offered at a time: the latest reversible change. Its toast's
 * Undo button and Cmd/Ctrl+Z (while the toast is visible) run it once.
 */
type UndoEntry = {
	id: number;
	toastId: string | number;
	run: () => Promise<unknown>;
};

let active: UndoEntry | null = null;
let sequence = 0;

/** Called by the toast when it closes without the Undo being used. */
function expire(id: number) {
	if (active?.id === id) active = null;
}

/**
 * Registers `run` as the active undo and shows its toast. `render` builds the
 * toast from the callbacks it must wire: `onUndo` for its Undo button and
 * `onClose` for when it is dismissed or times out.
 */
export function offerUndo(
	run: () => Promise<unknown>,
	render: (callbacks: { onUndo: () => void; onClose: () => void; duration: number }) => string | number
) {
	// Only the latest change can be undone: its toast replaces an earlier one.
	if (active) toast.dismiss(active.toastId);
	const id = ++sequence;
	const toastId = render({
		onUndo: () => void triggerUndo(id),
		onClose: () => expire(id),
		duration: UNDO_WINDOW_MS
	});
	active = { id, toastId, run };
}

/** Shows a success toast with an Undo action for about eight seconds. */
export function showUndoToast(message: string, run: () => Promise<unknown>) {
	offerUndo(run, ({ onUndo, onClose, duration }) =>
		toast.custom(AppToast, {
			class: 'app-toast-shell',
			duration,
			onDismiss: onClose,
			onAutoClose: onClose,
			componentProps: { variant: 'success', title: message, action: { label: m['common.toast.undo'](), onclick: onUndo } }
		})
	);
}

/**
 * Whether the entry is still offered, i.e. its toast is still shown: the
 * toast's lifetime (about eight seconds, paused while hovered) is the undo
 * window. A custom toast closed with its own close button is marked dismissed
 * without calling onDismiss, so the toast list is the source of truth.
 */
function offered(entry: UndoEntry | null): entry is UndoEntry {
	return entry !== null && toast.getActiveToasts().some((shown) => shown.id === entry.toastId);
}

/** Whether an Undo is currently offered. */
export function hasActiveUndo(): boolean {
	return offered(active);
}

/**
 * Runs the active undo (or the one with `id`) once. Refusals are explained in
 * an error toast; the change stays as it is.
 */
export async function triggerUndo(id?: number): Promise<boolean> {
	const entry = active;
	if (!offered(entry) || (id !== undefined && entry.id !== id)) return false;
	active = null;
	toast.dismiss(entry.toastId);
	try {
		await entry.run();
	} catch (error) {
		showUndoError(error);
	}
	return true;
}

export function showUndoError(error: unknown) {
	switch (undoFailure(error)) {
		case 'edited':
			appToast.error(m['common.toast.edited_meanwhile']());
			return;
		case 'expired':
			appToast.error(m['common.toast.restore_expired']());
			return;
		case 'unavailable':
			appToast.error(m['common.toast.undo_unavailable']());
			return;
		default:
			appToast.apiError(error, m['common.toast.undo_failed']());
	}
}

/** Global keydown handler: Cmd/Ctrl+Z runs the offered undo, outside text fields. */
export function handleUndoShortcut(event: KeyboardEvent) {
	if (!hasActiveUndo() || !isUndoShortcut(event)) return;
	event.preventDefault();
	void triggerUndo();
}
