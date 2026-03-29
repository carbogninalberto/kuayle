import type { Role } from './roles';

const ROLE_PERMISSIONS: Record<Role, string[]> = {
	owner: [
		'workspace:manage',
		'team:manage',
		'issue:create',
		'issue:read',
		'issue:update',
		'issue:delete',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'member:invite',
		'cycle:manage',
		'view:manage'
	],
	admin: [
		'team:manage',
		'issue:create',
		'issue:read',
		'issue:update',
		'issue:delete',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'member:invite',
		'cycle:manage',
		'view:manage'
	],
	member: [
		'issue:create',
		'issue:read',
		'issue:update',
		'issue:delete_own',
		'project:manage',
		'label:manage',
		'cycle:manage',
		'view:manage'
	],
	guest: ['issue:read']
};

export function hasPermission(role: Role, permission: string): boolean {
	return ROLE_PERMISSIONS[role]?.includes(permission) ?? false;
}
