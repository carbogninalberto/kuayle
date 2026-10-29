import { api } from './client';

/** One built-in assistant tool, as Settings → Assistant lists it. */
export interface AssistantCapability {
	name: string;
	/** The model-facing description, in English. */
	description: string;
	requires_approval: boolean;
	changes_data: boolean;
	/** The server wired the tool's dependencies. */
	available: boolean;
	/** The workspace has not turned the tool off. */
	enabled: boolean;
}

export interface AssistantCapabilities {
	tools: AssistantCapability[];
	/** Owners and admins can switch tools; the server enforces it. */
	can_manage: boolean;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/assistant-capabilities`;

export const assistantCapabilities = {
	list: (slug: string) => api.get<AssistantCapabilities>(base(slug)),
	set: (slug: string, name: string, enabled: boolean) =>
		api.put<AssistantCapability>(`${base(slug)}/${encodeURIComponent(name)}`, { enabled })
};
