import MarkdownIt from 'markdown-it';
import type { Token } from 'markdown-it';

// Rich fences are model output. They never reach the sanitizer: a chart is
// drawn from validated JSON on a canvas, and an html widget runs only inside a
// sandboxed, CSP-restricted frame. Anything invalid stays an ordinary code
// block, so the reader always sees the source.
export const CHART_MAX_BYTES = 16384;
export const HTML_MAX_BYTES = 65536;
export const MAX_RICH_BLOCKS = 4;

const MAX_STRING_LENGTH = 200;
const MAX_CATEGORIES = 100;
const MAX_SERIES = 5;
const CHART_TYPES = ['bar', 'line', 'area', 'pie'] as const;
const SPEC_KEYS = new Set(['type', 'title', 'unit', 'categories', 'series']);
const SERIES_KEYS = new Set(['name', 'data']);

export type ChartType = (typeof CHART_TYPES)[number];

export interface ChartSeries {
	name: string;
	data: number[];
}

export interface ChartSpec {
	type: ChartType;
	title?: string;
	unit?: string;
	categories: string[];
	series: ChartSeries[];
}

export type RichSegment =
	| { kind: 'markdown'; source: string }
	| { kind: 'chart'; spec: ChartSpec; fence: string }
	| { kind: 'html'; html: string; fence: string };

// The parser the chat renders with (see $lib/markdown): no raw HTML, bare URLs
// are links and images stay text.
const md = new MarkdownIt({ html: false, linkify: true, breaks: false, typographer: false }).disable('image');

const encoder = new TextEncoder();

function byteLength(text: string): number {
	return encoder.encode(text).length;
}

function isObject(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isText(value: unknown): value is string {
	return typeof value === 'string' && value.length <= MAX_STRING_LENGTH;
}

function isNumberArray(value: unknown, length: number): value is number[] {
	return (
		Array.isArray(value) &&
		value.length === length &&
		value.every((entry) => typeof entry === 'number' && Number.isFinite(entry))
	);
}

/** Strict JSON series spec: unknown keys, wrong lengths and bad types are refused. */
export function parseChartSpec(json: string): ChartSpec | null {
	let value: unknown;
	try {
		value = JSON.parse(json);
	} catch {
		return null;
	}
	if (!isObject(value)) return null;
	if (Object.keys(value).some((key) => !SPEC_KEYS.has(key))) return null;
	if (typeof value.type !== 'string' || !CHART_TYPES.includes(value.type as ChartType)) return null;
	if (value.title !== undefined && !isText(value.title)) return null;
	if (value.unit !== undefined && !isText(value.unit)) return null;
	if (!Array.isArray(value.categories) || value.categories.length < 1 || value.categories.length > MAX_CATEGORIES)
		return null;
	if (!value.categories.every(isText)) return null;
	if (!Array.isArray(value.series) || value.series.length < 1 || value.series.length > MAX_SERIES) return null;

	const series: ChartSeries[] = [];
	for (const entry of value.series) {
		if (!isObject(entry)) return null;
		if (Object.keys(entry).some((key) => !SERIES_KEYS.has(key))) return null;
		if (!isText(entry.name) || !isNumberArray(entry.data, value.categories.length)) return null;
		series.push({ name: entry.name, data: entry.data });
	}
	if (value.type === 'pie' && series.length !== 1) return null;

	return {
		type: value.type as ChartType,
		...(value.title !== undefined ? { title: value.title } : {}),
		...(value.unit !== undefined ? { unit: value.unit } : {}),
		categories: value.categories,
		series
	};
}

function fenceInfo(token: Token): 'chart' | 'html' | null {
	if (token.type !== 'fence' || token.level !== 0) return null;
	const info = token.info.trim();
	if (info === 'chart') {
		return byteLength(token.content) <= CHART_MAX_BYTES && parseChartSpec(token.content) !== null ? 'chart' : null;
	}
	if (info === 'html') return byteLength(token.content) <= HTML_MAX_BYTES ? 'html' : null;
	return null;
}

/** Whether a Markdown token is a top-level rich block that the chat renders. */
export function isRichFence(token: Token): boolean {
	return fenceInfo(token) !== null;
}

function acceptedFences(tokens: Token[]): Token[] {
	const accepted: Token[] = [];
	for (const token of tokens) {
		if (accepted.length >= MAX_RICH_BLOCKS) break;
		if (isRichFence(token)) accepted.push(token);
	}
	return accepted;
}

function cleanMarkdown(source: string): string {
	return source.replace(/^\n+/, '').replace(/\n+$/, '');
}

/**
 * Splits a message into Markdown and rich blocks. The source lines of each
 * accepted fence are kept so a rendering failure can show the original code.
 */
export function richSegments(source: string): RichSegment[] {
	const tokens = md.parse(source, {});
	const accepted = acceptedFences(tokens);
	if (accepted.length === 0) return source.trim() ? [{ kind: 'markdown', source }] : [];
	const lines = source.split('\n');
	const segments: RichSegment[] = [];
	let cursor = 0;
	const pushMarkdown = (end: number) => {
		const text = cleanMarkdown(lines.slice(cursor, end).join('\n'));
		if (text) segments.push({ kind: 'markdown', source: text });
	};
	for (const token of accepted) {
		const map = token.map;
		if (!map) continue;
		pushMarkdown(map[0]);
		const fence = lines.slice(map[0], map[1]).join('\n');
		const info = token.info.trim();
		const spec = info === 'chart' ? parseChartSpec(token.content) : null;
		if (spec) segments.push({ kind: 'chart', spec, fence });
		else segments.push({ kind: 'html', html: token.content, fence });
		cursor = map[1];
	}
	pushMarkdown(lines.length);
	return segments;
}

/**
 * Removes the fences the chat renders as rich blocks, for the Markdown-source
 * copy path. Fences that fall back to code stay in the text.
 */
export function stripRichBlocks(source: string): string {
	const tokens = md.parse(source, {});
	const accepted = acceptedFences(tokens);
	if (accepted.length === 0) return source.replace(/\n{3,}/g, '\n\n').trim();
	const lines = source.split('\n');
	const removed = new Set<number>();
	for (const token of accepted) {
		const map = token.map;
		if (!map) continue;
		for (let line = map[0]; line < map[1]; line++) removed.add(line);
	}
	return lines
		.filter((_, index) => !removed.has(index))
		.join('\n')
		.replace(/\n{3,}/g, '\n\n')
		.trim();
}

/** The complete document loaded into the widget frame, before any model markup. */
export function widgetDocument(html: string): string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; form-action 'none'; base-uri 'none'"><style>html,body{margin:0;font:14px system-ui}</style></head><body>${html}</body></html>`;
}
