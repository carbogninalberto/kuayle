// The column-width markup the editor's resizable tables persist. Shared by
// the storage sanitizer (./sanitize.ts) and the editor's import normalization
// (./../components/shared/table-column-widths.ts); the Go sanitizer in
// BE/pkg/sanitize/html.go must keep accepting exactly these forms, and its
// tests read the shared contract (testdata/editor_html_contract.json).

// A first-row cell's per-column widths, comma-separated across a colspan;
// a 0 entry is the editor's placeholder for the spanned columns.
export const COLWIDTH = /^\d{1,4}(,\d{1,4})*$/;
// A bare integer (px optional): the editor reads the attribute with parseInt.
export const COL_WIDTH = /^[1-9]\d{0,3}(px)?$/;

const WIDTH_PROPERTY = /^(width|min-width)$/i;
const WIDTH_VALUE = /^\d{1,4}(?:\.\d+)?px$/i;

function splitDeclaration(declaration: string): { property: string; value: string } | null {
	const colon = declaration.indexOf(':');
	if (colon < 0) return null;
	return {
		property: declaration.slice(0, colon).trim(),
		value: declaration.slice(colon + 1).trim().replace(/!important$/i, '').trim()
	};
}

/**
 * The width declarations resizable tables carry: pixel width and min-width,
 * nothing broader. Property names and values match case-insensitively and a
 * kept declaration is normalized to `property: value`, exactly like the
 * backend's sanitizer, so both layers store the same markup.
 */
export function filterColumnWidthStyles(style: string): string {
	return style
		.split(';')
		.map((declaration) => {
			const parsed = splitDeclaration(declaration);
			if (parsed && WIDTH_PROPERTY.test(parsed.property) && WIDTH_VALUE.test(parsed.value)) {
				return `${parsed.property}: ${parsed.value}`;
			}
			return null;
		})
		.filter((declaration): declaration is string => declaration !== null)
		.join('; ');
}

/**
 * The pixel width a `width: NNNpx` style declaration carries, rounded for the
 * colwidth attribute, or null when the style has no readable pixel width (a
 * percentage, another unit, or no width declaration at all).
 */
export function columnStyleWidth(style: string): number | null {
	for (const declaration of style.split(';')) {
		const parsed = splitDeclaration(declaration);
		if (!parsed || parsed.property.toLowerCase() !== 'width') continue;
		const match = /^(\d{1,4}(?:\.\d+)?)px$/i.exec(parsed.value);
		return match ? Math.round(Number(match[1])) : null;
	}
	return null;
}
