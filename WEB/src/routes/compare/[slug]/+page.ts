import { HUBS } from '$lib/data/routes';

export function entries() {
	return HUBS.compare.children.map((c) => ({ slug: c.slug }));
}

export const prerender = true;
