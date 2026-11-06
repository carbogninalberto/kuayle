import { error } from '@sveltejs/kit';
import { alternativeSlug } from '$lib/data/competitor-types';
import { alternativePages, competitorBySlug } from '$lib/data/alternative-pages';

export const prerender = true;

export function entries() {
	return alternativePages.map((page) => ({ slug: alternativeSlug({ slug: page.competitor }) }));
}

export function load({ params }) {
	const content = alternativePages.find((page) => alternativeSlug({ slug: page.competitor }) === params.slug);
	if (!content) error(404, 'Not found');
	const competitor = competitorBySlug(content.competitor);
	const others = content.others.map(competitorBySlug);
	return { content, competitor, others };
}
