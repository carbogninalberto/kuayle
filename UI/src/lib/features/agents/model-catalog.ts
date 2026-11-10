import type { AgentModelCatalog, CatalogModel } from '$lib/types/dev-machine';

// The model catalog is what a model field suggests for a coding agent. The
// server decides what is in it (per agent) and which efforts each model
// allows; nothing here knows an agent or a model by name. It only helps:
// whatever is typed is still accepted and validated when it is saved.

/** The models whose id or name contains every word typed, in catalog order. */
export function filterModels(models: CatalogModel[] | undefined, query: string): CatalogModel[] {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	return (models ?? []).filter((model) => {
		const text = `${model.id} ${model.name}`.toLowerCase();
		return words.every((word) => text.includes(word));
	});
}

/**
 * What was typed, offered as a value of its own: trimmed, and empty when it is
 * blank or already the id of a suggestion.
 */
export function typedChoice(models: CatalogModel[] | undefined, query: string): string {
	const typed = query.trim();
	return typed && !(models ?? []).some((model) => model.id === typed) ? typed : '';
}

/**
 * The effort levels to offer for a model: the ones the catalog says it
 * allows, or every level when the model is not in the catalog (typed, or no
 * list) or its efforts are not known.
 */
export function allowedEfforts<T extends string>(
	models: CatalogModel[] | undefined,
	model: string,
	levels: readonly T[]
): T[] {
	const efforts: readonly string[] | undefined = (models ?? []).find((item) => item.id === model)?.efforts;
	if (!efforts?.length) return [...levels];
	return levels.filter((level) => efforts.includes(level));
}

/**
 * The effort to keep after the model changed. An effort the new model does
 * not allow goes back to the agent default (''), and `reset` names the level
 * that was dropped so the form can say so.
 */
export function effortAfterModelChange(
	models: CatalogModel[] | undefined,
	model: string,
	effort: string
): { effort: string; reset: string } {
	if (!effort || allowedEfforts(models, model, [effort]).length) return { effort, reset: '' };
	return { effort: '', reset: effort };
}

export type CatalogStatus = 'loading' | 'unavailable' | 'stale' | 'empty' | 'ready';

/** What to tell the user about the list beside its suggestions. */
export function catalogStatus(catalog: AgentModelCatalog | null, loading: boolean, failed: boolean): CatalogStatus {
	if (loading) return 'loading';
	if (failed || catalog?.unavailable) return 'unavailable';
	if (catalog?.stale) return 'stale';
	return catalog?.models.length ? 'ready' : 'empty';
}
