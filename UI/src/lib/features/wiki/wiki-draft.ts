// Unsaved-change detection for the wiki editor. Kept free of Svelte and
// $lib imports so the Node unit tests can load it directly.

export interface WikiDraft {
	title: string;
	content: string;
}

/** The editor reports an empty document as an empty paragraph. */
export function normalizeWikiContent(content: string): string {
	const trimmed = content.trim();
	return trimmed === '<p></p>' ? '' : trimmed;
}

/** Whether leaving now would lose something the user typed. */
export function draftChanged(saved: WikiDraft, draft: WikiDraft): boolean {
	return (
		saved.title.trim() !== draft.title.trim() ||
		normalizeWikiContent(saved.content) !== normalizeWikiContent(draft.content)
	);
}
