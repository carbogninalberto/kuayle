// The conversation list's project filter: every conversation ('all'), those
// without a visible project ('none'), or one project's id. Projects only tag
// and filter the person's own conversations; nothing is shared through them.
export type ProjectFilter = 'all' | 'none' | (string & {});

/** The `project_id` list query for a filter; undefined lists everything. */
export function listQuery(filter: ProjectFilter): string | undefined {
	return filter === 'all' ? undefined : filter;
}

/** A new chat started while one project is selected belongs to it. */
export function newChatProject(filter: ProjectFilter): string | null {
	return filter === 'all' || filter === 'none' ? null : filter;
}

/** A remembered filter, kept only while its project is still visible. */
export function resolveProjectFilter(saved: unknown, projects: readonly { id: string }[]): ProjectFilter {
	if (saved === 'none') return 'none';
	if (typeof saved === 'string' && projects.some((project) => project.id === saved)) return saved;
	return 'all';
}

export function matchesProjectFilter(conversation: { project?: { id: string } | null }, filter: ProjectFilter) {
	if (filter === 'all') return true;
	const project = conversation.project?.id ?? null;
	return filter === 'none' ? project === null : project === filter;
}
