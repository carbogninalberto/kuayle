// Syntax highlighting for a file preview (KUA-285): the language comes from
// the file's name, never from guessing at its content, and lowlight's tree is
// flattened into one list of tokens per line for Code Block's numbered lines.
// The content is untrusted repository text; tokens are only rendered as text.

/** Pages longer than this many characters are shown plain: highlighting them would stall the page. */
export const highlightCap = 100_000;

export interface Token {
	content: string;
	class?: string;
}

// The parts of lowlight this module uses (its own types need hast's).
interface HighlightNode {
	type: string;
	value?: string;
	properties?: { className?: string[] | string };
	children?: HighlightNode[];
}
export interface Highlighter {
	registered(alias: string): boolean;
	highlight(language: string, value: string): HighlightNode;
}

const byName: Record<string, string> = {
	makefile: 'makefile',
	gnumakefile: 'makefile'
};
const byExtension: Record<string, string> = {
	svelte: 'xml',
	vue: 'xml',
	html: 'xml',
	htm: 'xml',
	svg: 'xml',
	mjs: 'javascript',
	cjs: 'javascript',
	jsx: 'javascript',
	ts: 'typescript',
	tsx: 'typescript',
	mts: 'typescript',
	cts: 'typescript',
	toml: 'ini',
	jsonc: 'json',
	yml: 'yaml',
	sh: 'bash',
	zsh: 'bash',
	md: 'markdown'
};

/** The lowlight language for a path, or undefined to show it plain. */
export function languageFor(path: string, lowlight: Pick<Highlighter, 'registered'>): string | undefined {
	const name = path.slice(path.lastIndexOf('/') + 1).toLowerCase();
	if (byName[name]) return byName[name];
	const dot = name.lastIndexOf('.');
	if (dot <= 0) return undefined;
	const extension = name.slice(dot + 1);
	const language = byExtension[extension] ?? extension;
	return lowlight.registered(language) ? language : undefined;
}

/** Flattens a highlighted tree into lines of tokens; a token spanning lines is split into each. */
export function tokenLines(tree: HighlightNode): Token[][] {
	const lines: Token[][] = [[]];
	const walk = (node: HighlightNode, classes: string[]) => {
		if (node.type === 'text') {
			const parts = (node.value ?? '').split('\n');
			parts.forEach((content, index) => {
				if (index > 0) lines.push([]);
				if (content) lines[lines.length - 1].push(classes.length ? { content, class: classes.join(' ') } : { content });
			});
			return;
		}
		const own = node.properties?.className;
		const next = own ? [...classes, ...(Array.isArray(own) ? own : [own])] : classes;
		for (const child of node.children ?? []) walk(child, next);
	};
	walk(tree, []);
	// The final newline ends the last line; it does not start another.
	if (lines.length > 1 && lines[lines.length - 1].length === 0) lines.pop();
	return lines;
}

let loading: Promise<Highlighter> | undefined;
/** lowlight with its common languages, loaded on first use so the panel's chunk stays small. */
export function loadHighlighter(): Promise<Highlighter> {
	loading ??= import('lowlight').then(({ common, createLowlight }) => createLowlight(common) as unknown as Highlighter);
	return loading;
}
