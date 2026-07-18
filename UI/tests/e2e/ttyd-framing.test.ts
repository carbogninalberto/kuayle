import { expect, test } from '@playwright/test';
import {
	decodeServerFrame,
	encodeInitialTerminalMessage,
	encodeInputFrame,
	encodeResizeFrame
} from '../../src/lib/features/dev-machines/ttyd';

const decoder = new TextDecoder();

test('encodes ttyd v1 client frames', () => {
	expect(JSON.parse(decoder.decode(encodeInitialTerminalMessage({ columns: 120, rows: 32 })))).toEqual({
		AuthToken: '',
		columns: 120,
		rows: 32
	});
	expect([...encodeInputFrame('ls\r')]).toEqual([48, 108, 115, 13]);
	expect(decoder.decode(encodeResizeFrame({ columns: 88, rows: 24 }))).toBe('1{"columns":88,"rows":24}');
});

test('decodes ttyd v1 server frames from binary and text data', () => {
	expect(decodeServerFrame(new Uint8Array([48, 111, 107]).buffer)).toEqual({ command: 'output', data: new Uint8Array([111, 107]) });
	expect(decodeServerFrame('1Dev shell')).toEqual({ command: 'title', title: 'Dev shell' });
	expect(decodeServerFrame(`2${JSON.stringify({ cursorBlink: false, fontSize: 14 })}`)).toEqual({
		command: 'preferences',
		preferences: { cursorBlink: false, fontSize: 14 }
	});
});
