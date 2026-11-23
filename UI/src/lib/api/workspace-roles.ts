import { api } from './client';

export interface WorkspaceRole {
	key: string;
	name: string;
	description: string;
	permissions: string[];
	member_count: number;
	team_assignments: number;
}

export interface PermissionGroup {
	area: string;
	permissions: { code: string; hint: string }[];
}

export interface TeamRole {
	team_id: string;
	role: string;
}

export function listRoles(slug: string) {
	return api.get<WorkspaceRole[]>(`/api/workspaces/${slug}/roles`);
}

export function listRolePermissions(slug: string) {
	return api.get<PermissionGroup[]>(`/api/workspaces/${slug}/roles/permissions`);
}

export function saveRole(
	slug: string,
	key: string | undefined,
	values: {
		name: string;
		description: string;
		permissions: string[];
		template?: string;
	}
) {
	const path = `/api/workspaces/${slug}/roles`;
	return key ? api.put<WorkspaceRole>(`${path}/${key}`, values) : api.post<WorkspaceRole>(path, values);
}

export function deleteRole(slug: string, key: string) {
	return api.delete<void>(`/api/workspaces/${slug}/roles/${key}`);
}

export function listTeamRoles(slug: string, userId: string) {
	return api.get<TeamRole[]>(`/api/workspaces/${slug}/members/${userId}/team-roles`);
}

export function setTeamRole(slug: string, userId: string, teamId: string, role: string | null) {
	const path = `/api/workspaces/${slug}/members/${userId}/team-roles/${teamId}`;
	return role === null ? api.delete<void>(path) : api.put<void>(path, { role });
}
