// Text lines of editor HTML for version diffs. Kept free of DOM and $lib
// imports so Node unit tests load it directly; the result is only ever
// rendered as text.
import { diffLines, type SkillDiffLine } from '../../features/assistant/skill-diff.ts';

const blockEnd = /<\/(p|h[1-6]|li|blockquote|pre|div|tr)>|<br\s*\/?>/gi;
const entities: Record<string, string> = {
	'&amp;': '&',
	'&lt;': '<',
	'&gt;': '>',
	'&quot;': '"',
	'&#39;': "'",
	'&nbsp;': ' '
};

/** Decodes the entities the editor emits. */
export function decodeEntities(text: string): string {
	return text
		.replace(/&(amp|lt|gt|quot|#39|nbsp);/g, (entity) => entities[entity])
		.replace(/&#(\d+);/g, (_, code) => String.fromCodePoint(Number(code)));
}

/** One line per block of editor HTML, without markup; list items keep a dash. */
export function htmlToTextLines(html: string | null | undefined): string[] {
	if (!html) return [];
	const marked = html.replace(/<li\b[^>]*>/gi, '<li>- ').replace(blockEnd, '\n');
	return decodeEntities(marked.replace(/<[^>]*>/g, ''))
		.split('\n')
		.map((line) => line.replace(/\s+/g, ' ').trim())
		.filter((line) => line !== '');
}

/** Line diff of two versions of editor HTML (or plain text). */
export function diffVersions(
	before: string | null | undefined,
	after: string | null | undefined,
	html: boolean
): SkillDiffLine[] {
	const lines = (value: string | null | undefined) =>
		html ? htmlToTextLines(value) : (value ?? '').split('\n').filter(Boolean);
	return diffLines(lines(before).join('\n'), lines(after).join('\n'));
}
