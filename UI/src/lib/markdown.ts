import MarkdownIt from 'markdown-it';
import { sanitizeHtml } from './security/sanitize';

const md = new MarkdownIt({
	html: false,
	linkify: true,
	breaks: false,
	typographer: false
});

export function renderMarkdown(source: string): string {
	return sanitizeHtml(md.render(source));
}
