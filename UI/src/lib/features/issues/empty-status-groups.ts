// Status groups with no issues, so an issue can be dragged to a status nobody uses yet.
// Pure and opt-in: the issue lists that want them wrap `issuesState.groupedIssues`.

export interface GroupLike<I = unknown> {
	key: string;
	label: string;
	issues: I[];
}

export interface EmptyGroupStatus {
	id: string;
	team_id: string;
	name: string;
	category: string;
	position: number;
}

export interface EmptyGroupOptions {
	/** Only these teams get empty groups: the scope's team, else the teams that have issues in the list. */
	teamIds: string[];
	/** Category order of the workflow sort preference; null keeps the plain position order. */
	categoryOrder?: string[] | null;
	/** Status ids of the first issue of each group, to rank groups the way the list does. */
	statusOf?: (group: GroupLike) => { category?: string; position?: number } | undefined;
}

/**
 * Adds a group for every status of `options.teamIds` that has none yet, in the order
 * the existing groups use (category order if set, then position). With several teams a
 * status whose name (case-insensitive) already has a group is skipped, because dropping
 * on that group already maps to the issue's own team status by name.
 */
export function withEmptyStatusGroups<I>(
	groups: GroupLike<I>[],
	statuses: EmptyGroupStatus[],
	options: EmptyGroupOptions
): GroupLike<I>[] {
	const teams = new Set(options.teamIds);
	if (teams.size === 0) return groups;
	const byId = new Map(statuses.map((status) => [status.id, status]));
	const keys = new Set(groups.map((group) => group.key));
	const names = new Set(groups.map((group) => group.label.trim().toLowerCase()));
	const added: GroupLike<I>[] = [];
	const rank = new Map<string, { category?: string; position?: number }>();
	for (const status of [...statuses].sort((a, b) => a.position - b.position)) {
		if (!teams.has(status.team_id) || keys.has(status.id)) continue;
		const name = status.name.trim().toLowerCase();
		if (teams.size > 1 && names.has(name)) continue;
		names.add(name);
		keys.add(status.id);
		added.push({ key: status.id, label: status.name, issues: [] });
		rank.set(status.id, status);
	}
	if (added.length === 0) return groups;

	const order = options.categoryOrder ?? null;
	const info = (group: GroupLike<I>) => rank.get(group.key) ?? byId.get(group.key) ?? options.statusOf?.(group);
	const categoryRank = (category?: string) => {
		const index = category ? (order?.indexOf(category) ?? -1) : -1;
		return index === -1 ? 999 : index;
	};
	return [...groups, ...added]
		.map((group, index) => ({ group, index, status: info(group) }))
		.sort((a, b) => {
			if (order) {
				const diff = categoryRank(a.status?.category) - categoryRank(b.status?.category);
				if (diff) return diff;
			}
			return (a.status?.position ?? 999) - (b.status?.position ?? 999) || a.index - b.index;
		})
		.map(({ group }) => group);
}

/** Whether empty groups make sense: no status filter hides them, and no further page may hold their issues. */
export function showEmptyStatusGroups(
	groupBy: string | null | undefined,
	filters: Record<string, string | undefined>,
	hasMore: boolean
): boolean {
	if (groupBy !== 'status' || hasMore) return false;
	return !filters.status && !filters.status_type;
}
