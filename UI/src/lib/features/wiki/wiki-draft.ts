// Content normalization for wiki autosave. Kept free of Svelte and
// $lib imports so the Node unit tests can load it directly.

/** The editor reports an empty document as an empty paragraph. */
export function normalizeWikiContent(content: string | null | undefined): string {
	const trimmed = (content ?? '').trim();
	return trimmed === '<p></p>' ? '' : trimmed;
}
