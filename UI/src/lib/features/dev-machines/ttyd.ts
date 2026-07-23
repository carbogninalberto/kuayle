const encoder = new TextEncoder();
const decoder = new TextDecoder();

export const TTYD_PROTOCOL = 'ttyd.v1';

const CLIENT_INPUT = '0';
const CLIENT_RESIZE = '1';
const SERVER_OUTPUT = '0';
const SERVER_TITLE = '1';
const SERVER_PREFERENCES = '2';

interface TtydTerminalSize {
	columns: number;
	rows: number;
}

type TtydServerFrame =
	| { command: 'output'; data: Uint8Array | string }
	| { command: 'title'; title: string }
	| { command: 'preferences'; preferences: Record<string, unknown> }
	| { command: 'unknown'; code: string; data: Uint8Array | string };

export function encodeInitialTerminalMessage(size: TtydTerminalSize, authToken = ''): Uint8Array {
	return encoder.encode(JSON.stringify({ AuthToken: authToken, columns: size.columns, rows: size.rows }));
}

export function encodeInputFrame(data: string | Uint8Array): Uint8Array {
	return encodeCommandFrame(CLIENT_INPUT, data);
}

export function encodeResizeFrame(size: TtydTerminalSize): Uint8Array {
	return encodeCommandFrame(CLIENT_RESIZE, JSON.stringify(size));
}

export function decodeServerFrame(data: string | ArrayBuffer | Uint8Array): TtydServerFrame {
	const { code, payload } = splitFrame(data);
	switch (code) {
		case SERVER_OUTPUT:
			return { command: 'output', data: payload };
		case SERVER_TITLE:
			return { command: 'title', title: payloadToString(payload) };
		case SERVER_PREFERENCES:
			return { command: 'preferences', preferences: parsePreferences(payloadToString(payload)) };
		default:
			return { command: 'unknown', code, data: payload };
	}
}

function encodeCommandFrame(command: string, data: string | Uint8Array): Uint8Array {
	const payload = typeof data === 'string' ? encoder.encode(data) : data;
	const frame = new Uint8Array(payload.length + 1);
	frame[0] = command.charCodeAt(0);
	frame.set(payload, 1);
	return frame;
}

function splitFrame(data: string | ArrayBuffer | Uint8Array): { code: string; payload: Uint8Array | string } {
	if (typeof data === 'string') {
		return { code: data.charAt(0), payload: data.slice(1) };
	}
	const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
	return { code: String.fromCharCode(bytes[0]), payload: bytes.slice(1) };
}

function payloadToString(payload: Uint8Array | string): string {
	return typeof payload === 'string' ? payload : decoder.decode(payload);
}

function parsePreferences(raw: string): Record<string, unknown> {
	try {
		const parsed = JSON.parse(raw);
		return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
	} catch {
		return {};
	}
}
