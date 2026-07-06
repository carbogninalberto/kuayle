import { toast } from 'svelte-sonner';
import type { Issue } from '$lib/types/issue';
import IssueDeletedToast from './IssueDeletedToast.svelte';

export function showIssueDeletedToast(issue: Issue) {
	toast.custom(IssueDeletedToast, {
		class: 'issue-deleted-toast-shell',
		duration: 5000,
		componentProps: {
			identifier: issue.identifier,
			title: issue.title,
			status: issue.status,
			statusCategory: issue.status_info?.category,
			statusColor: issue.status_info?.color,
			count: 1
		}
	});
}

export function showIssuesDeletedToast(count: number) {
	toast.custom(IssueDeletedToast, {
		class: 'issue-deleted-toast-shell',
		duration: 5000,
		componentProps: { count }
	});
}
