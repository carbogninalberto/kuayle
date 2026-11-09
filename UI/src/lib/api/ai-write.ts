import { api } from './client';

/** What the AI writes: a SKILL.md, an agent profile's instructions or a workflow step's prompt. */
export type AiWriteKind = 'skill' | 'instructions' | 'step_prompt';

export interface AiWriteRequest {
	kind: AiWriteKind;
	/** generate writes the whole text; rewrite replaces only the selection. */
	mode: 'generate' | 'rewrite';
	instruction: string;
	source: string;
	selection?: string;
}

/** Asks the workspace's AI model (Settings → AI) to write; nothing is saved. Owners and admins only. */
export const aiWrite = (slug: string, input: AiWriteRequest) =>
	api.post<{ text: string }>(`/api/workspaces/${encodeURIComponent(slug)}/ai/write`, input);
