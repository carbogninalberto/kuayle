export type Role = 'owner' | 'admin' | 'member' | 'guest';

export const ROLE_HIERARCHY: Record<Role, number> = {
	owner: 4,
	admin: 3,
	member: 2,
	guest: 1
};
