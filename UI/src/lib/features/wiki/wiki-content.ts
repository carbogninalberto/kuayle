// Reads a wiki page's saved HTML for the side panel: its outline and the
// files it embeds. Free of DOM and $lib imports for the Node unit tests.
import { decodeEntities } from '../../components/shared/version-diff.ts';

export interface OutlineItem {
	level: number;
	text: string;
	/** Position among the page's headings, to find the rendered element. */
	index: number;
}

export interface PageFile {
	kind: 'file' | 'image';
	href: string;
	name: string;
	size: number;
}

function text(html: string): string {
	return decodeEntities(html.replace(/<[^>]*>/g, ''))
		.replace(/\s+/g, ' ')
		.trim();
}

function attribute(tag: string, name: string): string | null {
	const match = new RegExp(`\\s${name}="([^"]*)"`, 'i').exec(tag);
	return match ? decodeEntities(match[1]) : null;
}

/** Headings in document order. Empty headings stay counted but are not listed. */
export function wikiOutline(html: string | null | undefined): OutlineItem[] {
	const items: OutlineItem[] = [];
	let index = 0;
	for (const match of (html ?? '').matchAll(/<h([1-6])\b[^>]*>([\s\S]*?)<\/h\1>/gi)) {
		const label = text(match[2]);
		if (label) items.push({ level: Number(match[1]), text: label, index });
		index++;
	}
	return items;
}

function fileName(href: string): string {
	const path = href.split(/[?#]/)[0];
	return decodeURIComponent(path.slice(path.lastIndexOf('/') + 1)) || href;
}

/** Attachments and images the page embeds, each once, in document order. */
export function wikiFiles(html: string | null | undefined): PageFile[] {
	const files: PageFile[] = [];
	const seen = new Set<string>();
	for (const match of (html ?? '').matchAll(/<a\b[^>]*data-type="attachment"[^>]*>|<img\b[^>]*>/gi)) {
		const tag = match[0];
		const image = tag.toLowerCase().startsWith('<img');
		const href = attribute(tag, image ? 'src' : 'href');
		if (!href || seen.has(href)) continue;
		seen.add(href);
		files.push({
			kind: image ? 'image' : 'file',
			href,
			name: (image ? attribute(tag, 'alt') : attribute(tag, 'data-filename')) || fileName(href),
			size: image ? 0 : Number(attribute(tag, 'data-size') ?? 0) || 0
		});
	}
	return files;
}
