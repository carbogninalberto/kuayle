import MarkdownIt from 'markdown-it';
import type { Token } from 'markdown-it';
import { isRichFence } from './rich-blocks.ts';

// The parser the chat renders with (see $lib/markdown): no raw HTML, bare URLs
// are links and images stay text, so the copied text matches what is read.
const md = new MarkdownIt({ html: false, linkify: true, breaks: false, typographer: false }).disable('image');

function inlineText(children: Token[] | null): string {
	let out = '';
	const links: { href: string; start: number }[] = [];
	for (const child of children ?? []) {
		switch (child.type) {
			case 'softbreak':
			case 'hardbreak':
				out += '\n';
				break;
			case 'link_open':
				links.push({ href: child.attrGet('href') ?? '', start: out.length });
				break;
			case 'link_close': {
				const link = links.pop();
				// A link whose text is not its address keeps the address after it.
				if (link && link.href && !child.markup && out.slice(link.start) !== link.href) out += ` (${link.href})`;
				break;
			}
			default:
				out += child.content;
		}
	}
	return out;
}

const indent = (text: string, first: string, rest: string) =>
	text
		.split('\n')
		.map((line, index) => (index === 0 ? first + line : line ? rest + line : line))
		.join('\n');

// Renders the blocks from `start` up to the token of type `close` and returns
// them with the index after it. Tight lists keep their items on adjacent lines.
function blockTexts(tokens: Token[], start: number, close?: string): { blocks: string[]; next: number; tight: boolean } {
	const blocks: string[] = [];
	let tight = true;
	let i = start;
	for (; i < tokens.length; i++) {
		const token = tokens[i];
		if (token.type === close) break;
		switch (token.type) {
			case 'paragraph_open':
				if (!token.hidden) tight = false;
				break;
			case 'inline':
				blocks.push(inlineText(token.children));
				break;
			case 'fence':
				// Rich blocks render as a chart or a widget, not as text.
				if (isRichFence(token)) break;
				blocks.push(token.content.replace(/\n$/, ''));
				break;
			case 'code_block':
				blocks.push(token.content.replace(/\n$/, ''));
				break;
			case 'hr':
				blocks.push('---');
				break;
			case 'blockquote_open': {
				const inner = blockTexts(tokens, i + 1, 'blockquote_close');
				blocks.push(
					inner.blocks
						.join('\n\n')
						.split('\n')
						.map((line) => (line ? `> ${line}` : '>'))
						.join('\n')
				);
				i = inner.next;
				break;
			}
			case 'bullet_list_open':
			case 'ordered_list_open': {
				const items: string[] = [];
				let loose = false;
				let j = i + 1;
				for (; j < tokens.length && tokens[j].type === 'list_item_open'; j++) {
					const marker = token.type === 'ordered_list_open' ? `${tokens[j].info}${tokens[j].markup} ` : '- ';
					const item = blockTexts(tokens, j + 1, 'list_item_close');
					loose ||= !item.tight;
					items.push(indent(item.blocks.join(item.tight ? '\n' : '\n\n'), marker, ' '.repeat(marker.length)));
					j = item.next;
				}
				blocks.push(items.join(loose ? '\n\n' : '\n'));
				i = j;
				break;
			}
			case 'table_open': {
				const rows: string[] = [];
				let cells: string[] = [];
				let j = i + 1;
				for (; j < tokens.length && tokens[j].type !== 'table_close'; j++) {
					if (tokens[j].type === 'inline') cells.push(inlineText(tokens[j].children).replace(/\n/g, ' '));
					else if (tokens[j].type === 'tr_close') {
						rows.push(cells.join('\t'));
						cells = [];
					}
				}
				blocks.push(rows.join('\n'));
				i = j;
				break;
			}
		}
	}
	return { blocks, next: i, tight };
}

/**
 * The readable text of a Markdown message, for the clipboard: the words
 * without their markup, code blocks and line breaks unchanged, and link
 * addresses kept. Nothing is shortened.
 */
export function markdownToPlainText(source: string): string {
	return blockTexts(md.parse(source, {}), 0).blocks.join('\n\n');
}

// Pages served without a secure context (a self-hosted instance over plain
// HTTP) have no async clipboard: a selection copy still works there.
function selectionCopy(text: string): boolean {
	if (typeof document === 'undefined') return false;
	const focused = document.activeElement as HTMLElement | null;
	const area = document.createElement('textarea');
	area.value = text;
	area.setAttribute('readonly', '');
	area.setAttribute('aria-hidden', 'true');
	Object.assign(area.style, { position: 'fixed', top: '0', left: '0', opacity: '0' });
	document.body.appendChild(area);
	try {
		area.select();
		area.setSelectionRange(0, text.length);
		return document.execCommand('copy');
	} catch {
		return false;
	} finally {
		area.remove();
		focused?.focus?.();
	}
}

/** Puts the whole text on the clipboard; false when the browser refused. */
export async function copyText(text: string): Promise<boolean> {
	try {
		if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(text);
			return true;
		}
	} catch {
		// Denied or unavailable: try the selection copy below.
	}
	return selectionCopy(text);
}
