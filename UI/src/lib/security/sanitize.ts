import DOMPurify from 'dompurify';

const ALLOWED_TAGS = [
	'p', 'br', 'strong', 'b', 'em', 'i', 's', 'del', 'code', 'pre',
	'a', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
	'ul', 'ol', 'li', 'blockquote', 'hr',
	'input', 'span', 'div', 'label',
	'img'
];

const ALLOWED_ATTR = [
	'class', 'data-type', 'data-checked', 'data-id', 'data-label',
	'href', 'target', 'rel',
	'type', 'checked', 'disabled',
	'src', 'alt', 'width', 'height'
];

const SAFE_URL_PATTERN = /^(https?:|mailto:)/i;

function createPurify() {
	const purify = DOMPurify;

	purify.addHook('afterSanitizeAttributes', (node) => {
		if (node.tagName === 'A') {
			node.setAttribute('rel', 'noopener noreferrer nofollow');
			const href = node.getAttribute('href') ?? '';
			if (href && !SAFE_URL_PATTERN.test(href)) {
				node.removeAttribute('href');
			}
		}
		if (node.tagName === 'IMG') {
			const src = node.getAttribute('src') ?? '';
			if (src && !src.startsWith('/uploads/') && !SAFE_URL_PATTERN.test(src)) {
				node.removeAttribute('src');
			}
		}
	});

	return purify;
}

const purify = createPurify();

export function sanitizeHtml(dirty: string): string {
	return purify.sanitize(dirty, {
		ALLOWED_TAGS,
		ALLOWED_ATTR,
		ALLOW_DATA_ATTR: false
	});
}

export function sanitizeEditorOutput(dirty: string): string {
	return sanitizeHtml(dirty);
}
