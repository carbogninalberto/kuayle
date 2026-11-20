import { COL_WIDTH, columnStyleWidth } from '../../security/column-widths.ts';

// Tiptap reads a column's width only from a first-row cell's colwidth
// attribute (falling back to parseInt on the matching <col width>), but it
// writes widths as <col style="width: NNNpx"> without reading that form back.
// Moving widths into colwidth before the editor parses stored markup keeps
// them; a width the parser would corrupt (25% parsed as 25px) is dropped.

function normalizeTable(table: Element): boolean {
	const cols = Array.from(table.querySelectorAll(':scope > colgroup > col'));
	if (cols.length === 0) return false;
	let changed = false;
	const widths: (number | null)[] = [];
	for (const col of cols) {
		const attribute = col.getAttribute('width');
		// Tiptap parseInt's the width attribute, so 25% would load as 25px.
		if (attribute !== null && !COL_WIDTH.test(attribute)) {
			col.removeAttribute('width');
			changed = true;
		}
		widths.push(
			columnStyleWidth(col.getAttribute('style') ?? '')
			?? (attribute !== null && COL_WIDTH.test(attribute) ? parseInt(attribute, 10) : null)
		);
	}
	const row = table.querySelector(':scope > thead > tr, :scope > tbody > tr, :scope > tfoot > tr');
	if (!row) return changed;
	let column = 0;
	for (const cell of Array.from(row.children)) {
		if (cell.tagName !== 'TH' && cell.tagName !== 'TD') continue;
		const span = /^\d{1,2}$/.test(cell.getAttribute('colspan') ?? '') ? parseInt(cell.getAttribute('colspan')!, 10) : 1;
		if (cell.getAttribute('colwidth') === null) {
			const cellWidths = widths.slice(column, column + span);
			if (cellWidths.length === span && cellWidths.every((width) => width !== null)) {
				cell.setAttribute('colwidth', cellWidths.join(','));
				changed = true;
			}
		}
		column += span;
	}
	return changed;
}

/**
 * Stored HTML with column widths where the editor can read them, so a
 * description created through the API round-trips without losing them.
 * Content without colgroup columns is returned unchanged.
 */
export function normalizeTableColumnWidths(html: string): string {
	if (!/<col[\s>]/i.test(html)) return html;
	const doc = new DOMParser().parseFromString(html, 'text/html');
	let changed = false;
	for (const table of doc.querySelectorAll('table')) {
		if (normalizeTable(table)) changed = true;
	}
	return changed ? doc.body.innerHTML : html;
}
