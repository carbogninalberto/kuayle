import { Node, mergeAttributes } from '@tiptap/core';

function formatFileSize(size: number): string {
	if (!size) return '';
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
	return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

export const Attachment = Node.create({
	name: 'attachment',
	group: 'inline',
	inline: true,
	atom: true,
	selectable: true,

	addAttributes() {
		return {
			href: { default: null },
			filename: {
				default: 'Attachment',
				parseHTML: (element) => element.getAttribute('data-filename') ?? element.textContent
			},
			size: {
				default: 0,
				parseHTML: (element) => Number(element.getAttribute('data-size') ?? 0)
			}
		};
	},

	parseHTML() {
		return [{ tag: 'a[data-type="attachment"]' }];
	},

	renderHTML({ HTMLAttributes }) {
		const size = formatFileSize(Number(HTMLAttributes.size));
		const filename = String(HTMLAttributes.filename || 'Attachment');
		return [
			'a',
			mergeAttributes({
				class: 'editor-attachment',
				'data-type': 'attachment',
				'data-filename': filename,
				'data-size': String(HTMLAttributes.size || 0),
				href: HTMLAttributes.href,
				download: filename,
				target: '_blank',
				rel: 'noopener noreferrer'
			}),
			size ? `${filename} (${size})` : filename
		];
	}
});
