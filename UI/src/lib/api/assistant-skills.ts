import { api } from './client';

export type AssistantSkillStatus = 'draft' | 'active';
export type AssistantSkillAuthor = 'user' | 'assistant';

/** A workspace skill for the assistant. `body` is present only when one skill is read. */
export interface AssistantSkill {
	id: string;
	workspace_id: string;
	team_id: string | null;
	team_key: string | null;
	team_name: string | null;
	name: string;
	description: string;
	body?: string;
	enabled: boolean;
	status: AssistantSkillStatus;
	/** The version in effect. */
	current_version: number;
	latest_version: number;
	/** A draft edit of an active skill waiting for activation. */
	pending_version: number | null;
	created_by_user_id: string | null;
	author_kind: AssistantSkillAuthor;
	created_at: string;
	updated_at: string;
}

export interface AssistantSkillVersion {
	skill_id: string;
	version: number;
	name: string;
	description: string;
	body: string;
	status: AssistantSkillStatus;
	author_kind: AssistantSkillAuthor;
	author_user_id: string | null;
	created_at: string;
}

export interface AssistantSkillList {
	skills: AssistantSkill[];
	/** Owners and admins can write skills; the server enforces it. */
	can_manage: boolean;
}

export interface AssistantSkillContent {
	name: string;
	description: string;
	body: string;
	/** Without a status, updates keep the skill's own; creates default to active. */
	status?: AssistantSkillStatus;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/assistant-skills`;
const one = (slug: string, id: string) => `${base(slug)}/${encodeURIComponent(id)}`;

export const assistantSkills = {
	list: (slug: string) => api.get<AssistantSkillList>(base(slug)),
	get: (slug: string, id: string) => api.get<AssistantSkill>(one(slug, id)),
	versions: (slug: string, id: string) =>
		api.get<{ versions: AssistantSkillVersion[] }>(`${one(slug, id)}/versions`),
	create: (slug: string, input: AssistantSkillContent & { team_id?: string | null }) =>
		api.post<AssistantSkill>(base(slug), input),
	update: (slug: string, id: string, input: AssistantSkillContent) => api.put<AssistantSkill>(one(slug, id), input),
	setEnabled: (slug: string, id: string, enabled: boolean) =>
		api.put<AssistantSkill>(`${one(slug, id)}/enabled`, { enabled }),
	/** Activates the given draft version, or the newest draft. */
	activate: (slug: string, id: string, version?: number) =>
		api.post<AssistantSkill>(`${one(slug, id)}/activate`, version === undefined ? {} : { version }),
	restore: (slug: string, id: string, version: number) =>
		api.post<AssistantSkill>(`${one(slug, id)}/versions/${version}/restore`),
	remove: (slug: string, id: string) => api.delete<void>(one(slug, id))
};
