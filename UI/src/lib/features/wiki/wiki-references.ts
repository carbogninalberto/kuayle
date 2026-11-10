// References to wiki pages from issues, comments, other pages and chat.
//
// A reference is stored as a mention chip that holds only the page ID
// (the server strips anything else). What a chip shows is resolved when it
// is rendered, through the reader's own access: a page they cannot see
// resolves to nothing but its ID and renders as a placeholder.
//
// Free of DOM, Svelte and $lib imports for the Node unit tests.
import { WIKI_PAGE_DRAG_TYPE } from './wiki-tree.ts';

export interface WikiReference {
	id: string;
	/** False for a page that is missing or that the reader cannot see. */
	accessible: boolean;
	title?: string;
	team_id?: string | null;
	project_id?: string | null;
	/** Name of the page's team or project. */
	scope_name?: string;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Where a reference opens, or null when the reader cannot open it. */
export function wikiReferenceHref(slug: string, reference: WikiReference): string | null {
	if (!reference.accessible) return null;
	const scope = reference.team_id
		? `teams/${reference.team_id}`
		: reference.project_id
			? `projects/${reference.project_id}`
			: null;
	return scope ? `/${slug}/${scope}/wiki?page=${reference.id}` : null;
}

/** The stored form of a reference; the same string the server writes. */
export function wikiMentionHtml(pageId: string): string {
	return `<span data-type="mention" data-kind="wiki" data-id="${pageId}" class="mention mention-wiki"></span>`;
}

const mentionSpan = /<span\b[^>]*\bdata-kind="wiki"[^>]*>[\s\S]*?<\/span>/gi;

function mentionId(tag: string): string | null {
	const id = /\bdata-id="([^"]*)"/i.exec(tag)?.[1] ?? '';
	return UUID.test(id) ? id.toLowerCase() : null;
}

/** Pages some HTML references, each once, in document order. */
export function wikiMentionIds(html: string | null | undefined): string[] {
	const ids = new Set<string>();
	for (const match of (html ?? '').matchAll(mentionSpan)) {
		const id = mentionId(match[0]);
		if (id) ids.add(id);
	}
	return [...ids];
}

function escapeHtml(value: string): string {
	return value
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');
}

/**
 * Renders the references of a published page for its reader, which has no
 * chips: a page of the same publication becomes a link, a page the reader
 * may know by title but that is not published with it becomes plain text,
 * and anything else an anonymous placeholder. Apply it to sanitized HTML.
 */
export function renderReaderReferences(
	html: string,
	options: {
		/** Titles of the pages this reader shows. */
		published: Map<string, string>;
		/** Titles of other pages the reader may know. */
		named: Map<string, string>;
		hrefFor: (id: string) => string;
		unavailable: string;
	}
): string {
	return html.replace(mentionSpan, (tag) => {
		const id = mentionId(tag);
		const published = id ? options.published.get(id) : undefined;
		if (id && published !== undefined)
			return `<a class="mention mention-wiki" href="${escapeHtml(options.hrefFor(id))}">${escapeHtml(published)}</a>`;
		const named = id ? options.named.get(id) : undefined;
		if (named !== undefined) return escapeHtml(named);
		return `<span class="mention mention-wiki mention-unavailable">${escapeHtml(options.unavailable)}</span>`;
	});
}

export interface ReferenceResolver {
	/** Never rejects: a page that cannot be resolved is inaccessible. */
	resolve(slug: string, pageId: string): Promise<WikiReference>;
	/** Forgets every answer, e.g. after pages were renamed or moved. */
	clear(): void;
}

/**
 * Resolves chips in batches: every page asked for in the same tick goes to
 * the server in one request, and answers are kept for a short while so a
 * re-render does not ask again. Failures are not kept.
 */
export function createReferenceResolver(
	fetchMany: (slug: string, ids: string[]) => Promise<WikiReference[]>,
	options: { ttlMs?: number; now?: () => number; batchSize?: number } = {}
): ReferenceResolver {
	const ttl = options.ttlMs ?? 60_000;
	const now = options.now ?? (() => Date.now());
	const batchSize = options.batchSize ?? 200;
	const cache = new Map<string, { at: number; reference: WikiReference }>();
	const waiting = new Map<string, Map<string, { promise: Promise<WikiReference>; settle: (r: WikiReference) => void }>>();

	async function flush(slug: string) {
		const batch = waiting.get(slug);
		waiting.delete(slug);
		if (!batch) return;
		const ids = [...batch.keys()];
		for (let start = 0; start < ids.length; start += batchSize) {
			const chunk = ids.slice(start, start + batchSize);
			let found = new Map<string, WikiReference>();
			let failed = false;
			try {
				found = new Map((await fetchMany(slug, chunk)).map((reference) => [reference.id, reference]));
			} catch {
				failed = true;
			}
			for (const id of chunk) {
				const reference = found.get(id) ?? { id, accessible: false };
				if (!failed) cache.set(`${slug}/${id}`, { at: now(), reference });
				batch.get(id)?.settle(reference);
			}
		}
	}

	return {
		resolve(slug, pageId) {
			const id = pageId.toLowerCase();
			const cached = cache.get(`${slug}/${id}`);
			if (cached && now() - cached.at <= ttl) return Promise.resolve(cached.reference);
			let batch = waiting.get(slug);
			if (!batch) {
				batch = new Map();
				waiting.set(slug, batch);
				queueMicrotask(() => void flush(slug));
			}
			const pending = batch.get(id);
			if (pending) return pending.promise;
			let settle!: (reference: WikiReference) => void;
			const promise = new Promise<WikiReference>((resolve) => (settle = resolve));
			batch.set(id, { promise, settle });
			return promise;
		},
		clear() {
			cache.clear();
		}
	};
}

/** The page a drag carries when it started in a wiki tree, else null. */
export function wikiPageDragData(
	transfer: { types: readonly string[]; getData(type: string): string } | null | undefined
): string | null {
	if (!transfer || !Array.from(transfer.types).includes(WIKI_PAGE_DRAG_TYPE)) return null;
	const id = transfer.getData(WIKI_PAGE_DRAG_TYPE);
	return UUID.test(id) ? id : null;
}

/** Whether a drag started in a wiki tree; readable during dragover, when the data is not. */
export function isWikiPageDrag(transfer: { types: readonly string[] } | null | undefined): boolean {
	return !!transfer && Array.from(transfer.types).includes(WIKI_PAGE_DRAG_TYPE);
}

/**
 * The @-query being typed at the caret of a plain text field, as the rich
 * editor detects it: an @ at the start of a line or after a space, with no
 * line break up to the caret.
 */
export function mentionQueryAt(text: string, caret: number): { from: number; query: string } | null {
	const before = text.slice(0, caret);
	const from = before.lastIndexOf('@');
	if (from < 0) return null;
	if (from > 0 && !/\s/.test(before[from - 1])) return null;
	const query = before.slice(from + 1);
	if (query.includes('\n') || query.length > 50) return null;
	return { from, query };
}

// Chat messages are plain text, so a page is referenced by a token the
// assistant can read (title, page ID, team or project) and the chat renders
// as a chip.
const chatReference = /@\[[^\]\n]*\]\(wiki:([0-9a-f-]{36})[^)\n]*\)/gi;

export function chatReferenceToken(reference: WikiReference): string {
	const plain = (value: string, open: string, close: string) =>
		value.replace(/[\[(]/g, open).replace(/[\])]/g, close).replace(/\s+/g, ' ').trim();
	const title = plain(reference.title ?? '', '(', ')');
	const scope = plain(reference.scope_name ?? '', '', '');
	const kind = reference.project_id ? 'project' : 'team';
	return `@[${title}](wiki:${reference.id}${scope ? ` ${kind} ${scope}` : ''})`;
}

/** Splits message text into plain parts and page references. */
export function chatReferenceParts(text: string): Array<{ text: string; pageId?: string }> {
	const parts: Array<{ text: string; pageId?: string }> = [];
	let last = 0;
	for (const match of text.matchAll(chatReference)) {
		if (!UUID.test(match[1])) continue;
		if (match.index > last) parts.push({ text: text.slice(last, match.index) });
		parts.push({ text: match[0], pageId: match[1].toLowerCase() });
		last = match.index + match[0].length;
	}
	if (last < text.length || parts.length === 0) parts.push({ text: text.slice(last) });
	return parts;
}
