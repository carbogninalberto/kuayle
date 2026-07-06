import { toast } from 'svelte-sonner';
import { authState } from '$lib/features/auth/auth.state.svelte';
import type { Issue } from '$lib/types/issue';
import IssueCreatedToast from './IssueCreatedToast.svelte';

function getUsername(): string {
	const user = authState.user;
	if (!user) return 'user';

	const name = (user.name || user.email.split('@')[0])
		.toLowerCase()
		.replace(/[^a-z0-9]/g, '');

	return name || 'user';
}

function getBranchName(issue: Issue): string {
	const id = issue.identifier.toLowerCase();
	const title = issue.title
		.toLowerCase()
		.replace(/[^a-z0-9\s-]/g, '')
		.replace(/\s+/g, '-')
		.slice(0, 50)
		.replace(/-$/, '');

	return `${getUsername()}/${id}-${title}`;
}

export function showIssueCreatedToast(slug: string, issue: Issue) {
	const href = `/${slug}/issue/${issue.identifier}`;
	const origin = typeof window === 'undefined' ? '' : window.location.origin;

	toast.custom(IssueCreatedToast, {
		class: 'issue-created-toast-shell',
		duration: 8000,
		componentProps: {
			identifier: issue.identifier,
			title: issue.title,
			href,
			url: `${origin}${href}`,
			branchName: getBranchName(issue),
			status: issue.status,
			statusCategory: issue.status_info?.category,
			statusColor: issue.status_info?.color
		}
	});
}
