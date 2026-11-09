import { getWorkspace } from '$lib/api/workspaces';
import { hasPermission } from '$lib/security/permissions';
import type { Role } from '$lib/security/roles';

/** Whether to offer wiki editing in this workspace. The API enforces access. */
export async function canWriteWiki(slug: string): Promise<boolean> {
	try {
		const workspace = await getWorkspace(slug);
		return hasPermission(workspace.current_user_role as Role, 'wiki:write');
	} catch {
		return false;
	}
}
