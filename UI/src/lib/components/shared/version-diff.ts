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

/** Part of a changed line; `changed` marks words that differ from its pair. */
export interface DiffSegment {
	text: string;
	changed: boolean;
}

/** One line of a git-style diff with its old and new line numbers. */
export interface DiffRow {
	kind: 'same' | 'add' | 'del';
	text: string;
	oldNo?: number;
	newNo?: number;
	/** Present on a removed or added line paired with its counterpart. */
	segments?: DiffSegment[];
}

export type DiffBlock = { kind: 'rows'; rows: DiffRow[] } | { kind: 'folded'; rows: DiffRow[] };

const wordPattern = /\s+|[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]/gu;

/** Words that differ between a removed line and the line that replaced it. */
export function wordSegments(before: string, after: string): { before: DiffSegment[]; after: DiffSegment[] } {
	const a = before.match(wordPattern) ?? [];
	const b = after.match(wordPattern) ?? [];
	// Tokens never contain line breaks, so the line diff compares them one each.
	const steps = diffLines(a.join('\n'), b.join('\n'));
	const left: DiffSegment[] = [];
	const right: DiffSegment[] = [];
	const push = (into: DiffSegment[], text: string, changed: boolean) => {
		const last = into.at(-1);
		if (last && last.changed === changed) last.text += text;
		else into.push({ text, changed });
	};
	for (const step of steps) {
		if (step.kind !== 'add') push(left, step.text, step.kind === 'del');
		if (step.kind !== 'del') push(right, step.text, step.kind === 'add');
	}
	return { before: left, after: right };
}

/**
 * Numbered rows of a diff between two versions. Within each run of removed
 * lines followed by added lines, lines are paired in order for word
 * highlighting, as git does with --word-diff.
 */
export function diffRows(
	before: string | null | undefined,
	after: string | null | undefined,
	html: boolean
): DiffRow[] {
	const rows: DiffRow[] = [];
	let oldNo = 0;
	let newNo = 0;
	for (const line of diffVersions(before, after, html)) {
		if (line.kind === 'same') rows.push({ kind: 'same', text: line.text, oldNo: ++oldNo, newNo: ++newNo });
		else if (line.kind === 'del') rows.push({ kind: 'del', text: line.text, oldNo: ++oldNo });
		else rows.push({ kind: 'add', text: line.text, newNo: ++newNo });
	}
	for (let i = 0; i < rows.length; ) {
		if (rows[i].kind !== 'del') {
			i++;
			continue;
		}
		let dels = i;
		while (dels < rows.length && rows[dels].kind === 'del') dels++;
		let adds = dels;
		while (adds < rows.length && rows[adds].kind === 'add') adds++;
		for (let k = 0; k < Math.min(dels - i, adds - dels); k++) {
			const { before: left, after: right } = wordSegments(rows[i + k].text, rows[dels + k].text);
			rows[i + k].segments = left;
			rows[dels + k].segments = right;
		}
		i = adds;
	}
	return rows;
}

/** Folds unchanged runs longer than twice `context`, keeping context lines around changes. */
export function foldRows(rows: DiffRow[], context = 3): DiffBlock[] {
	const blocks: DiffBlock[] = [];
	const visible = (rows: DiffRow[]) => {
		const last = blocks.at(-1);
		if (last?.kind === 'rows') last.rows.push(...rows);
		else if (rows.length) blocks.push({ kind: 'rows', rows });
	};
	let i = 0;
	while (i < rows.length) {
		if (rows[i].kind !== 'same') {
			visible([rows[i++]]);
			continue;
		}
		let end = i;
		while (end < rows.length && rows[end].kind === 'same') end++;
		const keepBefore = i === 0 ? 0 : context;
		const keepAfter = end === rows.length ? 0 : context;
		if (end - i > keepBefore + keepAfter) {
			visible(rows.slice(i, i + keepBefore));
			blocks.push({ kind: 'folded', rows: rows.slice(i + keepBefore, end - keepAfter) });
			visible(rows.slice(end - keepAfter, end));
		} else visible(rows.slice(i, end));
		i = end;
	}
	return blocks;
}

/** Side-by-side pairs: unchanged lines on both sides, changes aligned in order. */
export function splitPairs(rows: DiffRow[]): { left?: DiffRow; right?: DiffRow }[] {
	const pairs: { left?: DiffRow; right?: DiffRow }[] = [];
	for (let i = 0; i < rows.length; ) {
		if (rows[i].kind === 'same') {
			pairs.push({ left: rows[i], right: rows[i] });
			i++;
			continue;
		}
		const dels: DiffRow[] = [];
		const adds: DiffRow[] = [];
		while (i < rows.length && rows[i].kind === 'del') dels.push(rows[i++]);
		while (i < rows.length && rows[i].kind === 'add') adds.push(rows[i++]);
		for (let k = 0; k < Math.max(dels.length, adds.length); k++) pairs.push({ left: dels[k], right: adds[k] });
	}
	return pairs;
}
