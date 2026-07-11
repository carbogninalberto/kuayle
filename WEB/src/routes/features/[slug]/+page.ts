import { HUBS } from '$lib/data/routes';

export function entries() {
	return HUBS.features.children.map((c) => ({ slug: c.slug }));
}

export const prerender = true;
