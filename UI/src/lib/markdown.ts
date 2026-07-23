import MarkdownIt from 'markdown-it';
import { sanitizeHtml } from './security/sanitize';

const options = {
	html: false,
	linkify: true,
	breaks: false,
	typographer: false
};

const md = new MarkdownIt(options);

export function renderMarkdown(source: string, validateLink?: (url: string) => boolean): string {
	if (!validateLink) return sanitizeHtml(md.render(source));

	const restricted = new MarkdownIt(options);
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
