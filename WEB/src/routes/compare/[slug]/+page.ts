import { error } from '@sveltejs/kit';
import { MOVED_COMPARE_PAGES } from '$lib/data/moved-pages';

export const prerender = true;

export function entries() {
	return Object.keys(MOVED_COMPARE_PAGES).map((slug) => ({ slug }));
}

export function load({ params }) {
	const moved = MOVED_COMPARE_PAGES[params.slug];
	if (!moved) error(404, 'Not found');
	return { moved };
}
