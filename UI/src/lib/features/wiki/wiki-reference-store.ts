import { resolveWikiReferences } from '$lib/api/wiki';
import { createReferenceResolver } from './wiki-references';

/** The app's one resolver for reference chips; see createReferenceResolver. */
export const wikiReferences = createReferenceResolver(resolveWikiReferences);

// A page was renamed, moved or deleted somewhere: ask again next time.
if (typeof window !== 'undefined') window.addEventListener('ws:wiki-changed', () => wikiReferences.clear());
