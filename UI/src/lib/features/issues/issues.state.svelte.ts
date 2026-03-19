import type { Issue, IssueStatus, CreateIssueRequest, UpdateIssueRequest } from '$lib/types/issue';
import * as issueApi from '$lib/api/issues';

class IssuesState {
	issues = $state<Issue[]>([]);
	totalCount = $state(0);
	loading = $state(false);
	selectedIssue = $state<Issue | null>(null);
	filters = $state<Record<string, string>>({});

	issuesByStatus = $derived(
		this.issues.reduce(
			(acc, issue) => {
				if (!acc[issue.status]) acc[issue.status] = [];
				acc[issue.status].push(issue);
				return acc;
			},
			{} as Record<IssueStatus, Issue[]>
		)
	);

	async load(slug: string, params?: Record<string, string>) {
		this.loading = true;
		try {
			const res = await issueApi.listIssues(slug, params);
			this.issues = res.data;
			this.totalCount = res.total_count;
		} finally {
			this.loading = false;
		}
	}

	async create(slug: string, req: CreateIssueRequest): Promise<Issue> {
		const issue = await issueApi.createIssue(slug, req);
		this.issues = [issue, ...this.issues];
		this.totalCount++;
		return issue;
	}

	async update(slug: string, identifier: string, req: UpdateIssueRequest): Promise<Issue> {
		// Optimistic update
		const idx = this.issues.findIndex((i) => i.identifier === identifier);
		const original = idx >= 0 ? { ...this.issues[idx] } : null;

		if (idx >= 0) {
			this.issues[idx] = { ...this.issues[idx], ...req } as Issue;
		}

		try {
			const updated = await issueApi.updateIssue(slug, identifier, req);
			if (idx >= 0) {
				this.issues[idx] = updated;
			}
			if (this.selectedIssue?.identifier === identifier) {
				this.selectedIssue = updated;
			}
			return updated;
		} catch (err) {
			// Rollback
			if (idx >= 0 && original) {
				this.issues[idx] = original as Issue;
			}
			throw err;
		}
	}

	async remove(slug: string, identifier: string) {
		await issueApi.deleteIssue(slug, identifier);
		this.issues = this.issues.filter((i) => i.identifier !== identifier);
		this.totalCount--;
		if (this.selectedIssue?.identifier === identifier) {
			this.selectedIssue = null;
		}
	}

	select(issue: Issue | null) {
		this.selectedIssue = issue;
	}
}

export const issuesState = new IssuesState();
