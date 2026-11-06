import { alternatives } from '$lib/data/alternatives';

export function entries() {
	return Object.keys(alternatives).map((slug) => ({ slug }));
}

export const prerender = true;
