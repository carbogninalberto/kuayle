import { api } from './client';
import type { AISettings, UpdateAISettingsRequest } from '$lib/types/ai-settings';

export function getAISettings(slug: string): Promise<AISettings> {
	return api.get<AISettings>(`/api/workspaces/${slug}/ai-settings`);
}

export function updateAISettings(slug: string, req: UpdateAISettingsRequest): Promise<AISettings> {
	return api.patch<AISettings>(`/api/workspaces/${slug}/ai-settings`, req);
}
