import type { ViewLayout } from '$lib/types/view';

// Session-scoped memory of the last list/board layout, so returning to a
// view (e.g. browser back from an issue) reopens it in the same layout.
export const viewLayoutState = $state({ layout: 'list' as ViewLayout });
