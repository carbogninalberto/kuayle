import { api } from './client';
import type { WorkspaceMember } from '$lib/types/workspace';

export function listMembers(slug: string): Promise<WorkspaceMember[]> {
	return api.get<WorkspaceMember[]>(`/api/workspaces/${slug}/members`);
}
