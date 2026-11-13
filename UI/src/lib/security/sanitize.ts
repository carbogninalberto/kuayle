import DOMPurify from 'dompurify';
import { filterEditorClasses } from './editor-classes';
import { deferRemoteImage } from './remote-images';

const ALLOWED_TAGS = [
	'p', 'br', 'strong', 'b', 'em', 'i', 's', 'del', 'code', 'pre',
	'a', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
	'ul', 'ol', 'li', 'blockquote', 'hr',
	'input', 'span', 'div', 'label',
	'img',
	'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td'
];

const ALLOWED_ATTR = [
	'class', 'data-type', 'data-checked', 'data-id', 'data-label', 'data-kind', 'data-identifier',
	'data-filename', 'data-size',
	'href', 'target', 'rel', 'download',
	'type', 'checked', 'disabled',
	'src', 'alt', 'width', 'height',
	'align', 'colspan', 'rowspan'
];

const TABLE_ALIGNMENTS = new Set(['left', 'center', 'right']);

const SAFE_URL_PATTERN = /^(https?:|mailto:)/i;

function isSafeResourceUrl(value: string): boolean {
	return value.startsWith('/api/workspaces/') || value.startsWith('/api/public/assets/') || SAFE_URL_PATTERN.test(value);
}

// Set for the duration of one sanitize call; see sanitizeHtml.
let loadRemoteImages = false;

function createPurify() {
	const purify = DOMPurify;

	purify.addHook('afterSanitizeAttributes', (node) => {
		// Stored content keeps only the classes the editor emits; see editor-classes.ts.
		const classes = node.getAttribute('class');
		if (classes !== null) {
			const kept = filterEditorClasses(classes);
			if (kept) node.setAttribute('class', kept);
			else node.removeAttribute('class');
		}
		// Table column alignment uses the inert align attribute, never style:
		// model output must not be able to restyle the page.
		if (node.tagName === 'TH' || node.tagName === 'TD') {
			const align = node.getAttribute('align')?.toLowerCase();
			if (align && TABLE_ALIGNMENTS.has(align)) node.setAttribute('align', align);
			else node.removeAttribute('align');
			for (const name of ['colspan', 'rowspan']) {
				const span = node.getAttribute(name);
				if (span !== null && !/^\d{1,3}$/.test(span)) node.removeAttribute(name);
			}
		} else {
			node.removeAttribute('align');
		}
		if (node.tagName === 'A') {
			node.setAttribute('rel', 'noopener noreferrer nofollow');
			const href = node.getAttribute('href') ?? '';
			if (href && !isSafeResourceUrl(href)) {
				node.removeAttribute('href');
			}
		}
		if (node.tagName === 'IMG') {
			const src = node.getAttribute('src') ?? '';
			if (src && !isSafeResourceUrl(src)) {
				node.removeAttribute('src');
			}
			// Images from other hosts are loaded by the reader, not by rendering.
			if (!loadRemoteImages) deferRemoteImage(node, globalThis.location?.origin ?? '');
		}
	});

	return purify;
}

const purify = createPurify();

function sanitize(dirty: string, remoteImages: boolean): string {
	loadRemoteImages = remoteImages;
	try {
		return purify.sanitize(dirty, {
			ALLOWED_TAGS,
			ALLOWED_ATTR,
			ALLOW_DATA_ATTR: false
		});
	} finally {
		loadRemoteImages = false;
	}
}

/**
 * HTML that is safe to render. An image from another host comes out without a
 * src (see remote-images.ts) and is loaded when the reader asks; never store
 * this output. `remoteImages: 'load'` is for content the app's publisher wrote
 * and whose addresses the caller has already restricted (release notes).
 */
export function sanitizeHtml(dirty: string, { remoteImages = 'defer' }: { remoteImages?: 'defer' | 'load' } = {}): string {
	return sanitize(dirty, remoteImages === 'load');
}

/** HTML the editor produced, as it is stored: remote images keep their src. */
export function sanitizeEditorOutput(dirty: string): string {
	return sanitize(dirty, true);
}
