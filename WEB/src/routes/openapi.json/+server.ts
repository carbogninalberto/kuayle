import spec from '$lib/data/openapi.json';
import { FALLBACK_VERSION } from '$lib/release.svelte';

export const prerender = true;

/**
 * The OpenAPI document for the current release, as a static file at
 * /openapi.json. An instance serves the document of its own release at
 * /api/openapi.json.
 */
export function GET() {
	const document = { ...spec, info: { ...spec.info, version: FALLBACK_VERSION.replace(/^v/, '') } };
	return new Response(JSON.stringify(document), {
		headers: { 'Content-Type': 'application/json; charset=utf-8' }
	});
}
