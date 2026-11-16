/**
 * Rules and flows for editing and deleting a single comment. The server
 * enforces who may do what; these only decide which actions the UI offers.
 */

/** Only a comment's author is offered Edit; nobody else, admins included. */
export function canEditComment(comment: { user_id: string }, userId: string | null | undefined): boolean {
	return !!userId && comment.user_id === userId;
}

/** Whether an edited body has no content worth saving. */
export function commentBodyIsEmpty(html: string | null | undefined): boolean {
	const body = (html ?? '').trim();
	return body === '' || body === '<p></p>';
}

export type DeleteCommentFlow = {
	/** Deletes the comment on the server. */
	remove: () => Promise<unknown>;
	/** Restores it within the grace period. */
	restore: () => Promise<unknown>;
	/** Reloads the comment thread. */
	refresh: () => unknown;
	/** Shows the success toast with an Undo that runs `undo`. */
	offerUndo: (message: string, undo: () => Promise<unknown>) => void;
	/** Explains a failed delete. */
	onError: (err: unknown) => void;
	message: string;
};

/**
 * Deletes a comment, reloads the thread and offers Undo, which restores the
 * comment and reloads again. A failed delete offers no Undo. Returns whether
 * the comment was deleted.
 */
export async function deleteCommentWithUndo(flow: DeleteCommentFlow): Promise<boolean> {
	try {
		await flow.remove();
	} catch (err) {
		flow.onError(err);
		return false;
	}
	flow.refresh();
	flow.offerUndo(flow.message, async () => {
		await flow.restore();
		flow.refresh();
	});
	return true;
}
