import type { Issue, IssueStatus, IssuePriority, CreateIssueRequest, UpdateIssueRequest } from '$lib/types/issue';
import * as issueApi from '$lib/api/issues';
import { preferencesState, type GroupByField } from '$lib/features/preferences/preferences.state.svelte';
import type { StatusCategory } from '$lib/types/team-status';
import type { IssueAgentAssignee } from '$lib/api/agents';
import { assigneeGroup } from '$lib/features/agents/agent-assignees';
import { buildIssueTree, hiddenTreeIds, rangeSelection, visibleTreeIssues, type IssueTreeNode } from './issue-tree';
import { subIssueTreeState } from './sub-issue-tree.state.svelte';

export type { GroupByField } from '$lib/features/preferences/preferences.state.svelte';

class IssuesState {
	issues = $state<Issue[]>([]);
	totalCount = $state(0);
	loading = $state(false);
	loadError = $state(false);
	loadingMore = $state(false);
	hasMore = $state(false);
	selectedIssue = $state<Issue | null>(null);
	filters = $state<Record<string, string>>({});
	/** What was selected; read it through `selectedIds`, which leaves out rows hidden by nesting. */
	private selected = $state<Set<string>>(new Set());
	groupBy = $state<GroupByField>('status');
	/** Tells whether the issue list on screen nests sub-issues; set through `useNestedList`. */
	private nestedSource = $state<(() => boolean) | null>(null);
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
					// People first; an issue assigned only to agents groups under its agent.
					({ key, label } = assigneeGroup(issue));
					break;
				case 'project':
					key = issue.project_id || 'no-project';
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

	/** Category order of the workflow sort preference, or null when statuses sort by position only. */
	get statusCategoryOrder(): string[] | null {
		const teamId = this.filters.team;
		if (preferencesState.getWorkflowSortMode(this.currentSlug, teamId) === 'default') return null;
		return preferencesState.getWorkflowSortOrder(this.currentSlug, teamId);
	}

	/** True while the issue list on screen nests sub-issues under their parent. */
	get nestedList(): boolean {
		return this.nestedSource?.() ?? false;
	}

	/**
	 * The mounted issue list passes whether it nests sub-issues, read live. Call the returned
	 * function when the list unmounts; it only clears the source no other list has replaced.
	 */
	useNestedList(nested: () => boolean): () => void {
		this.nestedSource = nested;
		return () => {
			if (this.nestedSource === nested) this.nestedSource = null;
		};
	}

	/** The issues of each rendered list, one per group, in display order. */
	private listIssues = $derived<Issue[][]>(this.groupBy ? this.groupedIssues.map((group) => group.issues) : [this.issues]);

	/** Sub-issue trees of each list while the list nests them, built once per data change. */
	private listTrees = $derived<IssueTreeNode<Issue>[][] | null>(
		this.nestedList ? this.listIssues.map((issues) => buildIssueTree(issues)) : null
	);

	/** Flat issue order matching the visual rendering (respects grouping and nested, collapsed sub-issues). */
	private visibleOrder = $derived<Issue[]>(
		this.listTrees
			? this.listTrees.flatMap((nodes) => visibleTreeIssues(nodes, subIssueTreeState.expanded))
			: this.listIssues.flat()
	);

	/** Ids nested under a collapsed parent of the list on screen. */
	private hiddenByNesting = $derived.by(() => {
		const hidden = new Set<string>();
		for (const nodes of this.listTrees ?? []) {
			for (const id of hiddenTreeIds(nodes, subIssueTreeState.expanded)) hidden.add(id);
		}
		return hidden;
	});

	/** The selection without rows hidden by nesting; the same Set as `selected` when none is hidden. */
	private visibleSelection = $derived.by(() => {
		const selected = this.selected;
		if (selected.size === 0) return selected;
		const hidden = this.hiddenByNesting;
		if (hidden.size === 0 || ![...selected].some((id) => hidden.has(id))) return selected;
		return new Set([...selected].filter((id) => !hidden.has(id)));
	});

	/**
	 * Selected issue ids. A row hidden under a collapsed parent of a nested list is never part of
	 * it, so bulk actions only act on rows the user can see.
	 */
	get selectedIds(): Set<string> {
		return this.visibleSelection;
	}

	set selectedIds(ids: Set<string>) {
		this.selected = ids;
	}

	getAdjacentIdentifier(current: string, direction: 'prev' | 'next'): string | null {
		const idx = this.issueIdentifiers.indexOf(current);
		if (idx === -1) return null;
		const newIdx = direction === 'prev' ? idx - 1 : idx + 1;
		return this.issueIdentifiers[newIdx] ?? null;
	}

	toggleSelect(id: string) {
		// A row hidden under a collapsed parent cannot be selected, e.g. with `x` after collapsing it.
		if (this.hiddenByNesting.has(id)) return;
		const next = new Set(this.selectedIds);
		if (next.has(id)) {
			next.delete(id);
		} else {
			next.add(id);
		}
		this.selectedIds = next;
	}

	selectRange(fromId: string, toId: string) {
		// A hidden anchor starts the range at its nearest visible ancestor; see rangeSelection.
		const parents = new Map(this.issues.map((issue) => [issue.id, issue.parent_id]));
		const next = new Set(this.selectedIds);
		for (const id of rangeSelection(this.visibleOrder, fromId, toId, (id) => parents.get(id))) next.add(id);
		this.selectedIds = next;
	}

	/** Selects every row on screen; sub-issues under a collapsed parent stay unselected. */
	selectAll() {
		this.selectedIds = new Set(this.visibleOrder.map((issue) => issue.id));
	}

	/** Takes these ids out of the selection, e.g. the sub-issues of a parent that was just collapsed. */
	deselect(ids: Iterable<string>) {
		const next = new Set(this.selected);
		let changed = false;
		for (const id of ids) changed = next.delete(id) || changed;
		if (changed) this.selected = next;
	}

	clearSelection() {
		this.selectedIds = new Set();
	}

	clear() {
		this.loadRequestId++;
		this.loading = false;
		this.loadingMore = false;
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

	/**
	 * Pages are cut on the server, so a custom workflow group order has to be
	 * sent along: otherwise the first page holds the team's default-order groups
	 * (usually all Backlog) and the custom order only shows once more is loaded.
	 * Stored in the filters, so loadMore sends the same order.
	 */
	private withStatusCategoryOrder(slug: string, params?: Record<string, string>) {
		if (!params) return params;
		const rest = { ...params };
		delete rest.status_category_order;
		if (rest.group_by !== 'status') return rest;
		const team = rest.team || undefined;
		if (preferencesState.getWorkflowSortMode(slug, team) === 'default') return rest;
		return { ...rest, status_category_order: preferencesState.getWorkflowSortOrder(slug, team).join(',') };
	}

	async load(slug: string, params?: Record<string, string>, showLoading = true) {
		params = this.withStatusCategoryOrder(slug, params);
		this.beginLoad(slug, params, showLoading);
		this.loadError = false;
		const requestId = this.loadRequestId;
		try {
			const res = await issueApi.listIssues(slug, params);
			if (requestId !== this.loadRequestId) return;
			this.issues = res.data;
			this.totalCount = res.total_count;
			this.currentPage = res.page;
			this.hasMore = res.has_more;
		} catch {
			if (requestId === this.loadRequestId) {
				this.clear();
				this.loadError = true;
			}
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
		} catch {
			if (requestId === this.loadRequestId) {
				this.clear();
				this.loadError = true;
			}
		} finally {
			if (requestId === this.loadRequestId) {
				this.loadingMore = false;
			}
		}
	}

	async create(slug: string, req: CreateIssueRequest): Promise<Issue> {
		const requestId = this.loadRequestId;
		const issue = await issueApi.createIssue(slug, req);
		if (requestId !== this.loadRequestId || slug !== this.currentSlug) return issue;
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
		const requestId = this.loadRequestId;
		// Optimistic update
		const idx = this.issues.findIndex((i) => i.identifier === identifier);
		const original = idx >= 0 ? { ...this.issues[idx] } : null;

		if (idx >= 0) {
			this.issues[idx] = { ...this.issues[idx], ...req } as Issue;
		}

		try {
			const updated = await issueApi.updateIssue(slug, identifier, req);
			if (requestId !== this.loadRequestId || slug !== this.currentSlug) return updated;
			if (idx >= 0) {
				this.issues[idx] = updated;
			}
			if (this.selectedIssue?.identifier === identifier) {
				this.selectedIssue = updated;
			}
			return updated;
		} catch (err) {
			// Rollback
			if (requestId === this.loadRequestId && slug === this.currentSlug && idx >= 0 && original) {
				this.issues[idx] = original as Issue;
			}
			throw err;
		}
	}

	async remove(slug: string, identifier: string) {
		const requestId = this.loadRequestId;
		await issueApi.deleteIssue(slug, identifier);
		if (requestId !== this.loadRequestId || slug !== this.currentSlug) return;
		this.issues = this.issues.filter((i) => i.identifier !== identifier);
		this.totalCount--;
		if (this.selectedIssue?.identifier === identifier) {
			this.selectedIssue = null;
		}
	}

	/** Replaces the agents shown on a loaded issue after they changed. */
	setAgentAssignees(identifier: string, agents: IssueAgentAssignee[]) {
		const idx = this.issues.findIndex((i) => i.identifier === identifier);
		if (idx >= 0) {
			this.issues[idx] = { ...this.issues[idx], agent_assignees: agents };
		}
		if (this.selectedIssue?.identifier === identifier) {
			this.selectedIssue = { ...this.selectedIssue, agent_assignees: agents };
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

	async bulkUpdate(
		slug: string,
		updates: {
			status?: string;
			status_id?: string;
			priority?: number;
			assignee_id?: string;
			label_ids?: string[];
			cycle_id?: string;
			parent_id?: string;
			project_id?: string;
		}
	) {
		const issueIds = Array.from(this.selectedIds);
		if (issueIds.length === 0) return;

		const requestId = this.loadRequestId;
		const result = await issueApi.bulkUpdateIssues(slug, { issue_ids: issueIds, ...updates });
		if (requestId !== this.loadRequestId || slug !== this.currentSlug) return result;

		// Apply optimistic updates locally
		for (const issue of this.issues) {
			if (this.selectedIds.has(issue.id)) {
				if (updates.status_id) (issue as any).status_id = updates.status_id;
				if (updates.status) (issue as any).status = updates.status;
				if (updates.priority !== undefined) (issue as any).priority = updates.priority;
				if (updates.assignee_id) (issue as any).assignee_id = updates.assignee_id;
				if (updates.cycle_id !== undefined) (issue as any).cycle_id = updates.cycle_id || null;
				if (updates.parent_id !== undefined) (issue as any).parent_id = updates.parent_id || null;
				if (updates.project_id !== undefined) (issue as any).project_id = updates.project_id || null;
			}
		}
		this.clearSelection();
		return result;
	}

	select(issue: Issue | null) {
		this.selectedIssue = issue;
	}
}

export const issuesState = new IssuesState();
