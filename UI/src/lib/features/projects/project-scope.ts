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
