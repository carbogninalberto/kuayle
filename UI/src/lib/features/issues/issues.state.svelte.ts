import type { Issue, IssueStatus, IssuePriority, CreateIssueRequest, UpdateIssueRequest } from '$lib/types/issue';
import * as issueApi from '$lib/api/issues';

export type GroupByField = 'status' | 'priority' | 'assignee' | 'project' | null;

class IssuesState {
	issues = $state<Issue[]>([]);
	totalCount = $state(0);
	loading = $state(false);
	selectedIssue = $state<Issue | null>(null);
	filters = $state<Record<string, string>>({});
	selectedIds = $state<Set<string>>(new Set());
	groupBy = $state<GroupByField>('status');

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

	issueIdentifiers = $derived(this.issues.map((i) => i.identifier));

	selectionCount = $derived(this.selectedIds.size);

	groupedIssues = $derived.by(() => {
		if (!this.groupBy) return [{ key: 'all', label: 'All Issues', issues: this.issues }];

		const groups = new Map<string, Issue[]>();
		for (const issue of this.issues) {
			let key: string;
			switch (this.groupBy) {
				case 'status':
					key = issue.status;
					break;
				case 'priority':
					key = String(issue.priority);
					break;
				case 'assignee':
					key = issue.assignee_id ?? 'unassigned';
					break;
				case 'project':
					key = issue.project_id ?? 'no-project';
					break;
				default:
					key = 'all';
			}
			if (!groups.has(key)) groups.set(key, []);
			groups.get(key)!.push(issue);
		}

		return Array.from(groups.entries()).map(([key, issues]) => ({
			key,
			label: key,
			issues
		}));
	});

	getAdjacentIdentifier(current: string, direction: 'prev' | 'next'): string | null {
		const idx = this.issueIdentifiers.indexOf(current);
		if (idx === -1) return null;
		const newIdx = direction === 'prev' ? idx - 1 : idx + 1;
		return this.issueIdentifiers[newIdx] ?? null;
	}

	toggleSelect(id: string) {
		const next = new Set(this.selectedIds);
		if (next.has(id)) {
			next.delete(id);
		} else {
			next.add(id);
		}
		this.selectedIds = next;
	}

	selectRange(fromId: string, toId: string) {
		const fromIdx = this.issues.findIndex((i) => i.id === fromId);
		const toIdx = this.issues.findIndex((i) => i.id === toId);
		if (fromIdx === -1 || toIdx === -1) return;
		const start = Math.min(fromIdx, toIdx);
		const end = Math.max(fromIdx, toIdx);
		const next = new Set(this.selectedIds);
		for (let i = start; i <= end; i++) {
			next.add(this.issues[i].id);
		}
		this.selectedIds = next;
	}

	selectAll() {
		this.selectedIds = new Set(this.issues.map((i) => i.id));
	}

	clearSelection() {
		this.selectedIds = new Set();
	}

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

	async bulkUpdate(slug: string, updates: { status?: string; priority?: number; assignee_id?: string; label_ids?: string[] }) {
		const issueIds = Array.from(this.selectedIds);
		if (issueIds.length === 0) return;

		await issueApi.bulkUpdateIssues(slug, { issue_ids: issueIds, ...updates });

		// Apply optimistic updates locally
		for (const issue of this.issues) {
			if (this.selectedIds.has(issue.id)) {
				if (updates.status) (issue as any).status = updates.status;
				if (updates.priority !== undefined) (issue as any).priority = updates.priority;
				if (updates.assignee_id) (issue as any).assignee_id = updates.assignee_id;
			}
		}
		this.clearSelection();
	}

	select(issue: Issue | null) {
		this.selectedIssue = issue;
	}
}

export const issuesState = new IssuesState();
