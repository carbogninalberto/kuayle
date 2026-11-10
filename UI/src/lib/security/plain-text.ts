// Reads text out of stored HTML without ever making that HTML live.
// Assigning it to an element's innerHTML, even a detached one, loads its
// images and runs their handlers. A DOMParser document is inert: it has no
// browsing context, so nothing is fetched and no script or handler runs.
// Free of $lib imports for the Node unit tests.
import { decodeEntities } from '../components/shared/version-diff.ts';

/** The HTML as an inert document, or null where there is no DOM (server, Node). */
export function parseInertHtml(html: string): Document | null {
	if (typeof DOMParser === 'undefined') return null;
	return new DOMParser().parseFromString(html, 'text/html');
}

/** The text of stored HTML on one line, with runs of whitespace collapsed. */
export function htmlToPlainText(html: string | null | undefined): string {
	if (!html) return '';
	const parsed = parseInertHtml(html);
	const text = parsed ? (parsed.body.textContent ?? '') : decodeEntities(html.replace(/<[^>]*>/g, ' '));
	return text.replace(/\s+/g, ' ').trim();
}

/** Decodes character references in text; any markup in it stays literal text. */
export function decodeHtmlEntities(text: string): string {
	const parsed = parseInertHtml(`<div>${text.replace(/</g, '&lt;')}</div>`);
	return parsed ? (parsed.body.textContent ?? '') : decodeEntities(text);
}
