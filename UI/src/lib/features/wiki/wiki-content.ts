// Reads a wiki page's saved HTML for the side panel: its outline and the
// files it embeds. Free of DOM and $lib imports for the Node unit tests.
import { decodeEntities } from '../../components/shared/version-diff.ts';
import { safeFileUrl } from '../../security/safe-url.ts';

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

/**
 * Attachments and images the page embeds, each once, in document order.
 * Entries become links, so one whose address is not a web or API URL is left out.
 */
export function wikiFiles(html: string | null | undefined): PageFile[] {
	const files: PageFile[] = [];
	const seen = new Set<string>();
	for (const match of (html ?? '').matchAll(/<a\b[^>]*data-type="attachment"[^>]*>|<img\b[^>]*>/gi)) {
		const tag = match[0];
		const image = tag.toLowerCase().startsWith('<img');
		const href = safeFileUrl(attribute(tag, image ? 'src' : 'href'));
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

export interface RegistryEntry {
	/** "HH:MM" when the entry starts with a time. */
	time: string | null;
	/** The entry without its time, as editor HTML. */
	html: string;
}

const timePrefix = /^\s*(?:<strong>)?\s*(\d{1,2}:\d{2})\s*(?:<\/strong>)?\s*(?:[—–-]\s*)?/i;
const issueKey = /\b([A-Z][A-Z0-9]{1,9}-\d+)\b/g;
// The registry does not record which repository a PR belongs to, so pull
// requests render as reference chips rather than links.
const pullRequest = /\bPR #\d+\b/g;

/**
 * Turns plain issue keys such as KUA-20 into issue mentions, so entries that
 * agents write as text render as the usual issue chips, and marks "PR #12". Text inside links,
 * code and existing mentions is left alone.
 */
export function linkIssueKeys(html: string): string {
	let skip = 0;
	return html
		.split(/(<[^>]+>)/)
		.map((part) => {
			if (part.startsWith('<')) {
				if (/^<(a|code|pre)\b|^<span\b[^>]*data-type="mention"/i.test(part)) skip++;
				else if (/^<\/(a|code|pre)>/i.test(part) || (skip > 0 && /^<\/span>/i.test(part))) skip = Math.max(0, skip - 1);
				return part;
			}
			if (skip > 0) return part;
			return part
				.replace(pullRequest, (ref) => `<span class="registry-ref" data-kind="pull-request">${ref}</span>`)
				.replace(
					issueKey,
					(key) =>
						`<span data-type="mention" data-kind="issue" data-id="" data-label="${key}" data-identifier="${key}" class="mention mention-issue">${key}</span>`
				);
		})
		.join('');
}

/** A registry day's entries: one per list item, or per paragraph. */
export function registryEntries(html: string | null | undefined): RegistryEntry[] {
	const source = html ?? '';
	const items = /<li\b/i.test(source)
		? [...source.matchAll(/<li\b[^>]*>([\s\S]*?)<\/li>/gi)].map((match) =>
				match[1].replace(/^\s*<p\b[^>]*>([\s\S]*)<\/p>\s*$/i, '$1')
			)
		: [...source.matchAll(/<p\b[^>]*>([\s\S]*?)<\/p>/gi)].map((match) => match[1]);
	return items
		.filter((item) => text(item) !== '')
		.map((item) => {
			const match = timePrefix.exec(item);
			return {
				time: match ? match[1].padStart(5, '0') : null,
				html: linkIssueKeys(match ? item.slice(match[0].length) : item)
			};
		});
}
