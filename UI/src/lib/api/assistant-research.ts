import { api } from './client';

/** Settings → Assistant → Research. The search API key is never returned. */
export interface AssistantResearchSettings {
	search_provider: string;
	search_providers: string[];
	has_search_api_key: boolean;
	/** Empty: research sub-tasks use the conversation's model. */
	subagent_model: string;
	subagent_max_parallel: number;
	/** Owners and admins can change the settings; the server enforces it. */
	can_manage: boolean;
	updated_at?: string;
}

/** Fields left out are kept; `search_api_key: null` removes the key. */
export interface AssistantResearchUpdate {
	search_provider?: string;
	search_api_key?: string | null;
	subagent_model?: string;
	subagent_max_parallel?: number;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/assistant-research-settings`;

export const assistantResearch = {
	get: (slug: string) => api.get<AssistantResearchSettings>(base(slug)),
	update: (slug: string, update: AssistantResearchUpdate) => api.put<AssistantResearchSettings>(base(slug), update)
};
