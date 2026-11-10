import spec from '$lib/data/openapi.json';
import { buildApiReference } from '$lib/data/api-reference';

export const prerender = true;

/** Shaped on the server so the browser receives the reference, not the raw document. */
export function load() {
	return { reference: buildApiReference(spec) };
}
