import MarkdownIt from 'markdown-it';
import { sanitizeHtml } from './security/sanitize';

const options = {
	html: false,
	linkify: true,
	breaks: false,
	typographer: false
};

const md = new MarkdownIt(options);
// Model-authored images must never initiate network requests automatically.
// Render attachments separately through their authorized upload records.
const textOnlyMedia = new MarkdownIt(options).disable('image');

export function renderMarkdown(
	source: string,
	validateLink?: (url: string) => boolean,
	{ allowImages = true }: { allowImages?: boolean } = {}
): string {
	if (!validateLink) return sanitizeHtml((allowImages ? md : textOnlyMedia).render(source));

	const restricted = new MarkdownIt(options);
	if (!allowImages) restricted.disable('image');
	const tokens = restricted.parse(source, {});
	for (const token of tokens) {
		if (!token.children) continue;
		const allowedLinks: boolean[] = [];
		for (const child of token.children) {
			if (child.type === 'link_open') {
				const allowed = validateLink(child.attrGet('href') ?? '');
				allowedLinks.push(allowed);
				if (!allowed) {
					child.tag = 'span';
					child.attrs = [];
				}
			} else if (child.type === 'link_close' && allowedLinks.pop() === false) {
				child.tag = 'span';
			} else if (child.type === 'image' && !validateLink(child.attrGet('src') ?? '')) {
				child.type = 'text';
				child.tag = '';
				child.attrs = [];
			}
		}
	}
	return sanitizeHtml(restricted.renderer.render(tokens, restricted.options, {}));
}
