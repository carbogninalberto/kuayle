import assert from 'node:assert/strict';
import test from 'node:test';
import {
	decodeServerFrame,
	encodeInitialTerminalMessage,
	encodeInputFrame,
	encodePauseFrame,
	encodeResizeFrame,
	encodeResumeFrame
} from '../../src/lib/features/dev-machines/ttyd.ts';

const decoder = new TextDecoder();

test('encodes ttyd v1 client frames', () => {
	assert.deepEqual(JSON.parse(decoder.decode(encodeInitialTerminalMessage({ columns: 120, rows: 32 }))), {
		AuthToken: '',
		columns: 120,
		rows: 32
	});
	assert.deepEqual([...encodeInputFrame('ls\r')], [48, 108, 115, 13]);
	assert.equal(decoder.decode(encodeResizeFrame({ columns: 88, rows: 24 })), '1{"columns":88,"rows":24}');
});

test('decodes ttyd v1 server frames from binary and text data', () => {
	assert.deepEqual(decodeServerFrame(new Uint8Array([48, 111, 107]).buffer), {
		command: 'output',
		data: new Uint8Array([111, 107])
	});
	assert.deepEqual(decodeServerFrame('1Dev shell'), { command: 'title', title: 'Dev shell' });
	assert.deepEqual(decodeServerFrame(`2${JSON.stringify({ cursorBlink: false, fontSize: 14 })}`), {
		command: 'preferences',
		preferences: { cursorBlink: false, fontSize: 14 }
	});
});

test('encodes ttyd flow-control frames', () => {
	assert.deepEqual([...encodePauseFrame()], [50]);
	assert.deepEqual([...encodeResumeFrame()], [51]);
});

test('decodes binary output without copying the payload', () => {
	const bytes = new Uint8Array([48, 1, 2, 3]);
	const frame = decodeServerFrame(bytes);
	assert.equal(frame.command, 'output');
	assert.equal(frame.data.buffer, bytes.buffer);
	assert.deepEqual([...frame.data], [1, 2, 3]);
});
