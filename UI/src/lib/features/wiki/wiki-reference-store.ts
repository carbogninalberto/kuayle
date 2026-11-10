import { resolveWikiReferences } from '$lib/api/wiki';
import { createReferenceResolver } from './wiki-references';

/** The app's one resolver for reference chips; see createReferenceResolver. */
export const wikiReferences = createReferenceResolver(resolveWikiReferences);

// A page was renamed, moved or deleted somewhere: ask again next time.
// A workspace with a private team gets a generic refresh instead.
if (typeof window !== 'undefined') {
	for (const name of ['ws:wiki-changed', 'app:refresh']) window.addEventListener(name, () => wikiReferences.clear());
}
