// The class names stored rich text may carry: exactly what the editor and the
// app's own renderers emit. Anything else is removed, because a utility class
// on stored content can restyle the page around it (for example a link
// stretched invisibly over the whole viewport).
//
// The backend sanitizer keeps the same list in BE/pkg/sanitize/editor_classes.go.
// BE/pkg/sanitize/testdata/editor_html_contract.json is read by both test
// suites; change all three together.

/** Classes the editor puts on links. */
export const EDITOR_LINK_CLASS = 'text-[var(--app-accent-light)] underline';

export const EDITOR_CLASS_NAMES: readonly string[] = [
	'editor-attachment', // file attachment link
	'mention', // @user and issue mentions
	'mention-issue', // issue mentions
	'registry-ref', // pull request reference in the wiki registry
	...EDITOR_LINK_CLASS.split(' ')
];

/** Code blocks carry "language-<name>" so they can be highlighted. */
export const EDITOR_CLASS_PREFIXES: readonly string[] = ['language-'];

const SUFFIX_PATTERN = /^[A-Za-z0-9_+.#-]{1,40}$/;
const HTML_SPACE = /[ \t\n\f\r]+/;
const names = new Set(EDITOR_CLASS_NAMES);

function isEditorClass(name: string): boolean {
	if (names.has(name)) return true;
	return EDITOR_CLASS_PREFIXES.some((prefix) => name.startsWith(prefix) && SUFFIX_PATTERN.test(name.slice(prefix.length)));
}

/**
 * Keeps the allowed names of a class attribute value, in order and separated
 * by single spaces. Returns '' when none is allowed.
 */
export function filterEditorClasses(value: string): string {
	return value.split(HTML_SPACE).filter(isEditorClass).join(' ');
}
