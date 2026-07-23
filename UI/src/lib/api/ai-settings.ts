import { api } from './client';
import type { AISettings, UpdateAISettingsRequest } from '$lib/types/ai-settings';

export function getAISettings(slug: string): Promise<AISettings> {
	return api.get<AISettings>(`/api/workspaces/${slug}/ai-settings`);
}

export function getIssueCopyPrompt(slug: string): Promise<Pick<AISettings, 'issue_copy_prompt' | 'default_issue_copy_prompt'>> {
	return api.get(`/api/workspaces/${slug}/ai-settings/issue-copy-prompt`);
}

export function updateAISettings(slug: string, req: UpdateAISettingsRequest): Promise<AISettings> {
	return api.patch<AISettings>(`/api/workspaces/${slug}/ai-settings`, req);
}
