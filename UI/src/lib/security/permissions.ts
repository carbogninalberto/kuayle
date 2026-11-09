import type { Role } from './roles';

const ROLE_PERMISSIONS: Record<Role, string[]> = {
	owner: [
		'workspace:manage',
		'team:manage',
		'issue:create',
		'issues:read',
		'wiki:read',
		'issue:update',
		'issue:delete',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'member:invite',
		'cycle:manage',
		'view:manage',
		'wiki:write'
	],
	admin: [
		'team:manage',
		'issue:create',
		'issues:read',
		'wiki:read',
		'issue:update',
		'issue:delete',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'member:invite',
		'cycle:manage',
		'view:manage',
		'wiki:write'
	],
	member: [
		'issue:create',
		'issues:read',
		'wiki:read',
		'issue:update',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'cycle:manage',
		'view:manage',
		'wiki:write'
	],
	guest: ['issues:read', 'wiki:read']
};

export function hasPermission(role: Role, permission: string): boolean {
	return ROLE_PERMISSIONS[role]?.includes(permission) ?? false;
}
