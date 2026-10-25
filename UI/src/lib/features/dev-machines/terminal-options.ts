import type { ITerminalOptions } from '@xterm/xterm';

export const TERMINAL_UNICODE_VERSION = '11';

export const TERMINAL_OPTIONS = {
	allowProposedApi: true,
	convertEol: false,
	cursorBlink: true,
	fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
	fontSize: 13,
	letterSpacing: 0,
	lineHeight: 1,
	scrollback: 5000,
	theme: {
		background: '#09090b',
		foreground: '#e4e4e7',
		cursor: '#fafafa',
		selectionBackground: '#3f3f46'
	}
} satisfies ITerminalOptions;
