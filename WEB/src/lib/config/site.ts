import contentModifiedDates from '$lib/data/content-modified.json';

/**
 * Centralized site configuration.
 * The single source of truth for canonical origin, default metadata,
 * and reusable SEO utilities. Import this from any page or component
 * that needs absolute URLs or shared defaults.
 */

export const ORIGIN = 'https://kuayle.com';

export const SITE = {
	name: 'Kuayle',
	shortName: 'Kuayle',
	description:
		'Kuayle is the keyboard-driven, self-hosted issue tracker with no paid tier. One public Apache 2.0 repository, with nothing behind a license key.',
	tagline: 'The issue tracker with no paid tier',
	origin: ORIGIN,
	defaultImage: `${ORIGIN}/social-card.png`,
	defaultImageAlt: 'Kuayle issue tracker board interface',
	ogImageWidth: 1200,
	ogImageHeight: 630,
	locale: 'en_US',
	themeColor: '#0b0b0f'
} as const;

type OpenGraphType = 'website' | 'article';

export interface PageMeta {
	title: string;
	description: string;
	canonical?: string;
	noindex?: boolean;
	image?: string;
	imageAlt?: string;
	imageWidth?: number;
	imageHeight?: number;
	ogType?: OpenGraphType;
	publishedAt?: string;
	modifiedAt?: string;
	jsonLd?: Record<string, unknown> | Record<string, unknown>[];
}

/**
 * Build absolute URL for a path (adds origin).
 */
export function url(path: string): string {
	const base = ORIGIN.replace(/\/+$/, '');
	const p = path.startsWith('/') ? path : `/${path}`;
	return `${base}${p}`;
}

export function contentModifiedAt(path: string): string {
	const modifiedAt = (contentModifiedDates as Record<string, string>)[path];
	if (!modifiedAt) throw new Error(`Missing content modification date for ${path}`);
	return modifiedAt;
}

/**
 * Return complete metadata including derived defaults and absolute URLs.
 */
export function resolveMeta(meta: PageMeta) {
	const canonical = meta.canonical ?? url('');
	const imageAbsolute = meta.image ? (meta.image.startsWith('http') ? meta.image : url(meta.image)) : SITE.defaultImage;

	return {
		title: meta.title,
		description: meta.description,
		canonical,
		noindex: meta.noindex ?? false,
		ogType: meta.ogType ?? 'website',
		image: imageAbsolute,
		imageAlt: meta.imageAlt ?? SITE.defaultImageAlt,
		imageWidth: meta.imageWidth ?? SITE.ogImageWidth,
		imageHeight: meta.imageHeight ?? SITE.ogImageHeight,
		publishedAt: meta.publishedAt,
		modifiedAt: meta.modifiedAt,
		jsonLd: meta.jsonLd
	};
}
