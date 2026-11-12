import type { Project } from '../../types/project';
import type { Team } from '../../types/team';

type Scoped = Pick<Project, 'team_id'>;

/**
 * The projects every one of `issues` can join, mirroring the backend rule: an
 * issue and its project share a team, or neither team is private (a project
 * without a team is workspace-wide). A team missing from `teams` counts as
 * private, so nothing the server would refuse is offered.
 */
export function projectsForIssues<T extends Scoped>(
	projects: T[],
	issues: { team_id: string }[],
	teams: Pick<Team, 'id' | 'is_private'>[]
): T[] {
	const publicTeams = new Set(teams.filter((team) => !team.is_private).map((team) => team.id));
	const isPrivate = (teamId: string | null) => teamId !== null && !publicTeams.has(teamId);
	const issueTeams = [...new Set(issues.map((issue) => issue.team_id))];
	return projects.filter((project) =>
		issueTeams.every(
			(teamId) => teamId === project.team_id || (!isPrivate(teamId) && !isPrivate(project.team_id))
		)
	);
}

/** Projects arranged for a picker: the issue's own team first, then every other team, then workspace-wide ones. */
export interface ProjectGroups<T extends Scoped, K extends Pick<Team, 'id' | 'name'>> {
	/** Projects of the team the issue(s) belong to, or none when there is no single such team. */
	own: T[];
	/** Other teams that have at least one project, in the order of `teams`. */
	teams: { team: K; projects: T[] }[];
	/** Workspace-wide projects (no team), offered under a "Workspace" entry beside the teams. */
	workspace: T[];
}

/**
 * Splits `projects` for a picker. `ownTeamId` is the team of the issue (for a
 * selection of issues: their common team, else null). A project whose team is
 * not in `teams` is left out here: `projectsForIssues` already treats such a
 * team as private, so the picker never offers it to an outside issue anyway.
 */
export function groupProjectsByTeam<T extends Scoped, K extends Pick<Team, 'id' | 'name'>>(
	projects: T[],
	teams: K[],
	ownTeamId: string | null
): ProjectGroups<T, K> {
	const own = ownTeamId ? projects.filter((project) => project.team_id === ownTeamId) : [];
	const workspace = projects.filter((project) => project.team_id === null);
	const otherTeams = teams
		.filter((team) => team.id !== ownTeamId)
		.map((team) => ({ team, projects: projects.filter((project) => project.team_id === team.id) }))
		.filter((group) => group.projects.length > 0);
	return { own, teams: otherTeams, workspace };
}

/** The team every one of `issues` belongs to, else null (no issues, or several teams). */
export function commonTeamId(issues: { team_id: string }[]): string | null {
	const first = issues[0]?.team_id;
	return first !== undefined && issues.every((issue) => issue.team_id === first) ? first : null;
}

/** The projects of `groups` in display order, for keyboard navigation over a flat list. */
export function flattenProjectGroups<T extends Scoped, K extends Pick<Team, 'id' | 'name'>>(
	groups: ProjectGroups<T, K>
): T[] {
	return [...groups.own, ...groups.teams.flatMap((group) => group.projects), ...groups.workspace];
}
