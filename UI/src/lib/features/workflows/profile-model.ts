import type { AgentProfileInput, AgentProfileList, ModelFormat } from '$lib/api/workflows';

// Which coding agents take a model and an effort is the server's rule: the
// profile list names them in model_harnesses, and in model_formats how each
// writes its model names when that is not a bare name. Nothing here knows an
// agent by name.

/**
 * Whether a profile for this coding agent can set a model and an effort. A
 * server that does not send the list restricts nothing here; it still
 * validates the profile when it is saved.
 */
export function takesModel(list: Pick<AgentProfileList, 'model_harnesses'>, harness: string): boolean {
	return !list.model_harnesses || list.model_harnesses.includes(harness);
}

/**
 * The profile as it is saved: trimmed, and without a model or effort left
 * over from a coding agent that took them.
 */
export function profileSubmission(
	draft: AgentProfileInput,
	list: Pick<AgentProfileList, 'model_harnesses'>
): AgentProfileInput {
	const keep = takesModel(list, draft.harness);
	return {
		...draft,
		name: draft.name.trim(),
		model: keep ? draft.model.trim() : '',
		effort: keep ? draft.effort : ''
	};
}

/**
 * The form this coding agent's model names take (`provider/model`) with an
 * example, for the model field's placeholder and hint; undefined for a bare
 * name or a field that is off.
 */
export function modelFormat(
	list: Pick<AgentProfileList, 'model_harnesses' | 'model_formats'>,
	harness: string
): ModelFormat | undefined {
	return takesModel(list, harness) ? list.model_formats?.[harness] : undefined;
}
