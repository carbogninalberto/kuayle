import type { Issue, IssueStatus, IssuePriority, CreateIssueRequest, UpdateIssueRequest } from '$lib/types/issue';
import * as issueApi from '$lib/api/issues';
import { preferencesState, type GroupByField } from '$lib/features/preferences/preferences.state.svelte';
import type { StatusCategory } from '$lib/types/team-status';

export type { GroupByField } from '$lib/features/preferences/preferences.state.svelte';

class IssuesState {
	issues = $state<Issue[]>([]);
	totalCount = $state(0);
	loading = $state(false);
	loadingMore = $state(false);
	hasMore = $state(false);
	selectedIssue = $state<Issue | null>(null);
	filters = $state<Record<string, string>>({});
	selectedIds = $state<Set<string>>(new Set());
	groupBy = $state<GroupByField>('status');
	private currentSlug = '';
	private currentPage = 1;
	private loadRequestId = 0;

	/**
	 * Key identifying the view (filters) the current selection belongs to.
	 * When the view changes via `load()`, the selection is cleared so that
	 * bulk operations stay scoped to the view where the selection happened.
	 */
	private selectionScope: string | null = null;

	private viewKey(slug: string, params?: Record<string, string>): string {
		if (!params) return slug;
		const entries = Object.keys(params)
			.sort()
			.map((k) => `${k}=${params[k]}`);
		return [slug, ...entries].join('&');
	}

	issuesByStatus = $derived(
		this.issues.reduce(
			(acc, issue) => {
				const key = issue.status_id ?? issue.status;
				if (!acc[key]) acc[key] = [];
				acc[key].push(issue);
				return acc;
			},
			{} as Record<string, Issue[]>
		)
	);

	issueIdentifiers = $derived(this.issues.map((i) => i.identifier));

	selectionCount = $derived(this.selectedIds.size);

	groupedIssues = $derived.by(() => {
		if (!this.groupBy) return [{ key: 'all', label: 'All Issues', issues: this.issues }];

		const groups = new Map<string, { issues: Issue[]; label: string }>();
		for (const issue of this.issues) {
			let key: string;
			let label: string;
			switch (this.groupBy) {
				case 'status':
					key = issue.status_id ?? issue.status;
					label = issue.status_info?.name ?? issue.status;
					break;
				case 'priority':
					key = String(issue.priority);
					label = key;
					break;
				case 'assignee':
					key = issue.assignee_id ?? 'unassigned';
					label = issue.assignee?.name ?? key;
					break;
				case 'project':
					key = issue.project_id ?? 'no-project';
					label = key;
					break;
				default:
					key = 'all';
					label = key;
			}
			if (!groups.has(key)) groups.set(key, { issues: [], label });
			groups.get(key)!.issues.push(issue);
		}

		const result = Array.from(groups.entries()).map(([key, { issues, label }]) => ({
			key,
			label,
			issues
		}));

		if (this.groupBy === 'status') {
			result.sort((a, b) => {
				const aIssue = a.issues[0];
				const bIssue = b.issues[0];
				const teamId = this.filters.team;
				const mode = preferencesState.getWorkflowSortMode(this.currentSlug, teamId);
				if (mode !== 'default') {
					const order = preferencesState.getWorkflowSortOrder(this.currentSlug, teamId);
					const aCategory = aIssue?.status_info?.category as StatusCategory | undefined;
					const bCategory = bIssue?.status_info?.category as StatusCategory | undefined;
					const aRank = aCategory ? order.indexOf(aCategory) : -1;
					const bRank = bCategory ? order.indexOf(bCategory) : -1;
					if (aRank !== bRank) return (aRank === -1 ? 999 : aRank) - (bRank === -1 ? 999 : bRank);
				}
				const aPos = aIssue?.status_info?.position ?? 999;
				const bPos = bIssue?.status_info?.position ?? 999;
				return aPos - bPos;
			});
		}

		return result;
	});

	/** Flat issue order matching the visual rendering (respects grouping). */
	private visibleOrder = $derived<Issue[]>(
		this.groupBy
			? this.groupedIssues.flatMap((g) => g.issues)
			: this.issues
	);

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
		const order = this.visibleOrder;
		const fromIdx = order.findIndex((i) => i.id === fromId);
		const toIdx = order.findIndex((i) => i.id === toId);
		if (fromIdx === -1 || toIdx === -1) return;
		const start = Math.min(fromIdx, toIdx);
		const end = Math.max(fromIdx, toIdx);
		const next = new Set(this.selectedIds);
		for (let i = start; i <= end; i++) {
			next.add(order[i].id);
		}
		this.selectedIds = next;
	}

	selectAll() {
		this.selectedIds = new Set(this.issues.map((i) => i.id));
	}

	clearSelection() {
		this.selectedIds = new Set();
	}

	clear() {
		this.issues = [];
		this.totalCount = 0;
		this.hasMore = false;
		this.selectedIssue = null;
		this.clearSelection();
	}

	beginLoad(slug: string, params?: Record<string, string>, showLoading = true) {
		this.loadRequestId++;
		this.currentSlug = slug;
		// If the view changed, drop any selection from the previous view so
		// bulk operations remain scoped to the current view.
		const newScope = this.viewKey(slug, params);
		if (newScope !== this.selectionScope) {
			this.clear();
		}
		this.selectionScope = newScope;
		this.filters = params ?? {};
		this.currentPage = Number(params?.page ?? 1) || 1;
		this.hasMore = false;
		this.loadingMore = false;
		if (showLoading) this.loading = true;
	}

	async load(slug: string, params?: Record<string, string>, showLoading = true) {
		this.beginLoad(slug, params, showLoading);
		const requestId = this.loadRequestId;
		try {
			const res = await issueApi.listIssues(slug, params);
			if (requestId !== this.loadRequestId) return;
			this.issues = res.data;
			this.totalCount = res.total_count;
			this.currentPage = res.page;
			this.hasMore = res.has_more;
		} finally {
			if (requestId === this.loadRequestId) {
				this.loading = false;
			}
		}
	}

	async loadMore() {
		if (!this.currentSlug || this.loading || this.loadingMore || !this.hasMore) return;
		const requestId = this.loadRequestId;
		this.loadingMore = true;
		try {
			const nextPage = this.currentPage + 1;
			const res = await issueApi.listIssues(this.currentSlug, {
				...this.filters,
				page: String(nextPage)
			});
			if (requestId !== this.loadRequestId) return;
			const existingIds = new Set(this.issues.map((issue) => issue.id));
			this.issues = [...this.issues, ...res.data.filter((issue) => !existingIds.has(issue.id))];
			this.totalCount = res.total_count;
			this.currentPage = res.page;
			this.hasMore = res.has_more;
		} finally {
			if (requestId === this.loadRequestId) {
				this.loadingMore = false;
			}
		}
	}

	async create(slug: string, req: CreateIssueRequest): Promise<Issue> {
		const issue = await issueApi.createIssue(slug, req);
		// Only add to local list if it matches the current team filter
		const teamFilter = this.filters.team;
		if (!teamFilter || issue.team_id === teamFilter) {
			const existingIndex = this.issues.findIndex((existing) => existing.id === issue.id);
			if (existingIndex >= 0) {
				this.issues[existingIndex] = issue;
			} else {
				this.issues = [issue, ...this.issues];
				this.totalCount++;
			}
		}
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

	setSubscription(identifier: string, isSubscribed: boolean) {
		const idx = this.issues.findIndex((i) => i.identifier === identifier);
		if (idx >= 0) {
			this.issues[idx] = { ...this.issues[idx], is_subscribed: isSubscribed };
		}
		if (this.selectedIssue?.identifier === identifier) {
			this.selectedIssue = { ...this.selectedIssue, is_subscribed: isSubscribed };
		}
	}

	async bulkUpdate(slug: string, updates: { status?: string; status_id?: string; priority?: number; assignee_id?: string; label_ids?: string[]; cycle_id?: string; parent_id?: string }) {
		const issueIds = Array.from(this.selectedIds);
		if (issueIds.length === 0) return;

		await issueApi.bulkUpdateIssues(slug, { issue_ids: issueIds, ...updates });

		// Apply optimistic updates locally
		for (const issue of this.issues) {
			if (this.selectedIds.has(issue.id)) {
				if (updates.status_id) (issue as any).status_id = updates.status_id;
				if (updates.status) (issue as any).status = updates.status;
				if (updates.priority !== undefined) (issue as any).priority = updates.priority;
				if (updates.assignee_id) (issue as any).assignee_id = updates.assignee_id;
				if (updates.cycle_id !== undefined) (issue as any).cycle_id = updates.cycle_id || null;
				if (updates.parent_id !== undefined) (issue as any).parent_id = updates.parent_id || null;
			}
		}
		this.clearSelection();
	}

	select(issue: Issue | null) {
		this.selectedIssue = issue;
	}
}

export const issuesState = new IssuesState();
