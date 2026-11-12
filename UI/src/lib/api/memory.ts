import { api } from './client';

// Workspace memory (KUA-134): short lessons per repository. Agents, reviews
// and members only propose; an owner or admin approves, edits or drops each
// proposal, and only approved lessons reach an agent. The server decides who
// reads and who decides.

export type LessonState = 'proposed' | 'approved' | 'retired' | 'dropped';

export interface Lesson {
	id: string;
	repository: string;
	team_id: string | null;
	state: LessonState;
	/** What agents read once the lesson is approved. */
	text: string;
	/** What was proposed, before a person edited it; owners and admins only. */
	proposed_text?: string;
	source_event: 'task' | 'review' | 'manual';
	proposer: 'agent' | 'external' | 'person';
	source_task_id: string | null;
	proposed_by: string | null;
	decided_by: string | null;
	decided_at: string | null;
	position: number;
	created_at: string;
	updated_at: string;
}

export interface LessonList {
	lessons: Lesson[];
	can_manage: boolean;
	limits: { lesson_characters: number; approved_per_repository: number; prompt_bytes: number };
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/memory/lessons`;
const one = (slug: string, id: string) => `${base(slug)}/${encodeURIComponent(id)}`;

export const memory = {
	list: (slug: string, state?: LessonState) => api.get<LessonList>(`${base(slug)}${state ? `?state=${state}` : ''}`),
	/** Approved at once for an owner or admin, a proposal for any other member. */
	add: (slug: string, repository: string, text: string) => api.post<Lesson>(base(slug), { repository, text }),
	/** Approves a proposal, with the text as edited when given. */
	approve: (slug: string, id: string, text?: string) =>
		api.post<Lesson>(`${one(slug, id)}/approve`, text === undefined ? {} : { text }),
	edit: (slug: string, id: string, text: string) => api.put<Lesson>(one(slug, id), { text }),
	retire: (slug: string, id: string) => api.post<Lesson>(`${one(slug, id)}/retire`, {}),
	drop: (slug: string, id: string) => api.post<Lesson>(`${one(slug, id)}/drop`, {}),
	/** ids names every approved lesson of the repository once, in prompt order. */
	reorder: (slug: string, repository: string, ids: string[]) =>
		api.put<{ lessons: Lesson[] }>(`${base(slug)}/order`, { repository, ids })
};
