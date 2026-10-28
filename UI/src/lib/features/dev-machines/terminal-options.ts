import type { ITerminalOptions } from '@xterm/xterm';

export const TERMINAL_UNICODE_VERSION = '11';

const FONT_SIZE = 13;
const BUNDLED_FONT = 'JetBrains Mono';

/** Bundled faces (see TerminalSession) that must load before xterm measures its cells. */
export const TERMINAL_FONT_FACES = [`400 ${FONT_SIZE}px "${BUNDLED_FONT}"`, `700 ${FONT_SIZE}px "${BUNDLED_FONT}"`];

export const TERMINAL_OPTIONS = {
	allowProposedApi: true,
	convertEol: false,
	cursorBlink: true,
	// Box-drawing, block and Powerline glyphs are drawn by xterm (WebGL renderer) so borders
	// join; other symbols and emoji fall back through the system stack.
	customGlyphs: true,
	rescaleOverlappingGlyphs: true,
	fontFamily: `"${BUNDLED_FONT}", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", monospace`,
	fontSize: FONT_SIZE,
	fontWeight: 'normal',
	fontWeightBold: 'bold',
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
