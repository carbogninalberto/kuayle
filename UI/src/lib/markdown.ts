import MarkdownIt from 'markdown-it';
import { sanitizeHtml } from './security/sanitize';

const options = {
	html: false,
	linkify: true,
	breaks: false,
	typographer: false
};

// markdown-it emits column alignment as style="text-align:..."; keep it as the
// inert align attribute so the sanitizer never has to allow untrusted style.
function cellAlignAttribute(md: MarkdownIt) {
	md.core.ruler.push('cell_align', (state) => {
		for (const token of state.tokens) {
			if (token.type !== 'th_open' && token.type !== 'td_open') continue;
			const match = /^text-align:(left|center|right)$/.exec(token.attrGet('style') ?? '');
			if (!match) continue;
			token.attrs = (token.attrs ?? []).filter(([name]) => name !== 'style');
			token.attrSet('align', match[1]);
		}
	});
}

const md = new MarkdownIt(options).use(cellAlignAttribute);
// Model-authored images must never initiate network requests automatically.
// Render attachments separately through their authorized upload records.
const textOnlyMedia = new MarkdownIt(options).use(cellAlignAttribute).disable('image');

export function renderMarkdown(
	source: string,
	validateLink?: (url: string) => boolean,
	{ allowImages = true }: { allowImages?: boolean } = {}
): string {
	if (!validateLink) return sanitizeHtml((allowImages ? md : textOnlyMedia).render(source));

	const restricted = new MarkdownIt(options).use(cellAlignAttribute);
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
	// Images that passed the caller's check load at once; any other use waits for the reader.
	return sanitizeHtml(restricted.renderer.render(tokens, restricted.options, {}), { remoteImages: 'load' });
}
