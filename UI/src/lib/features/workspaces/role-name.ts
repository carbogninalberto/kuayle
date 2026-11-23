import { m } from '$lib/features/i18n/messages';
import { ROLE_HIERARCHY, type Role } from '$lib/security/roles';

export function isBuiltinRole(key: string): key is Role {
	return Object.hasOwn(ROLE_HIERARCHY, key);
}

export function roleName(key: string, name?: string) {
	return isBuiltinRole(key) ? m[`common.role.${key}`]() : name || m['settings.roles.custom']();
}
