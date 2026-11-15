import { toast } from 'svelte-sonner';
import type { Issue } from '$lib/types/issue';
import { offerUndo } from '$lib/features/undo/undo.svelte';
import IssueDeletedToast from './IssueDeletedToast.svelte';

type DeletedToastProps = {
	identifier?: string;
	title?: string;
	status?: string;
	statusCategory?: string;
	statusColor?: string | null;
	count: number;
};

/**
 * Shows the deleted toast. With `restore` it offers Undo (button and
 * Cmd/Ctrl+Z) for about eight seconds; without it, it closes after five.
 */
function showDeleted(props: DeletedToastProps, restore?: () => Promise<unknown>) {
	if (!restore) {
		toast.custom(IssueDeletedToast, { class: 'issue-deleted-toast-shell', duration: 5000, componentProps: props });
		return;
	}
	offerUndo(restore, ({ onUndo, onClose, duration }) =>
		toast.custom(IssueDeletedToast, {
			class: 'issue-deleted-toast-shell',
			duration,
			onDismiss: onClose,
			onAutoClose: onClose,
			componentProps: { ...props, onUndo }
		})
	);
}

export function showIssueDeletedToast(issue: Issue, restore?: () => Promise<unknown>) {
	showDeleted(
		{
			identifier: issue.identifier,
			title: issue.title,
			status: issue.status,
			statusCategory: issue.status_info?.category,
			statusColor: issue.status_info?.color,
			count: 1
		},
		restore
	);
}

export function showIssuesDeletedToast(count: number, restore?: () => Promise<unknown>) {
	showDeleted({ count }, restore);
}
