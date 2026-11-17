import type { ImportMapping } from '$lib/api/imports';

export interface MappingTargets {
	projectTeams: Record<string, string>;
	statusTargets: Record<string, string>;
	userTargets: Record<string, string>;
	priorities: Record<string, number>;
	epicMode: 'project' | 'parent';
	sprintMode: 'cycle' | 'ignore';
}

export function emptyMappingTargets(): MappingTargets {
	return {
		projectTeams: {}, statusTargets: {}, userTargets: {}, priorities: {},
		epicMode: 'project', sprintMode: 'cycle'
	};
}

// The UI binds primitive Select values; the API takes a nested mapping. Convert
// at the boundary so an empty selection becomes an explicit placeholder rather
// than a dangling reference.
export function buildImportMapping(t: MappingTargets): ImportMapping {
	const mappedTeams = new Set(Object.values(t.projectTeams).filter(Boolean));
	const built: ImportMapping = {
		projects: {}, statuses: {}, users: {}, priorities: { ...t.priorities },
		epic_mode: t.epicMode, sprint_mode: t.sprintMode
	};
	for (const [key, teamId] of Object.entries(t.projectTeams)) {
		if (teamId) built.projects[key] = { team_id: teamId, project_id: null };
	}
	for (const [key, statusId] of Object.entries(t.statusTargets)) {
		if (!statusId) continue;
		const sep = key.indexOf(':');
		if (sep < 0) continue;
		const teamId = key.slice(0, sep);
		if (!mappedTeams.has(teamId)) continue;
		built.statuses[key] = { team_id: teamId, status_id: statusId };
	}
	for (const [key, userId] of Object.entries(t.userTargets)) {
		built.users[key] = { user_id: userId || null };
	}
	return built;
}

export function mappingToTargets(mapping: ImportMapping | null | undefined): MappingTargets {
	const targets = emptyMappingTargets();
	targets.priorities = { ...(mapping?.priorities ?? {}) };
	for (const [key, value] of Object.entries(mapping?.projects ?? {})) targets.projectTeams[key] = value.team_id;
	for (const [key, value] of Object.entries(mapping?.statuses ?? {})) targets.statusTargets[key] = value.status_id;
	for (const [key, value] of Object.entries(mapping?.users ?? {})) targets.userTargets[key] = value.user_id ?? '';
	targets.epicMode = mapping?.epic_mode ?? 'project';
	targets.sprintMode = mapping?.sprint_mode ?? 'cycle';
	return targets;
}
