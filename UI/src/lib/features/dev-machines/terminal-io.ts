// Browser-terminal I/O helpers kept free of DOM and xterm imports so they can be unit tested.

const encoder = new TextEncoder();

export interface TerminalSize {
	columns: number;
	rows: number;
}

interface TerminalSink {
	write(data: Uint8Array, callback?: () => void): void;
}

type ScheduleHandle = unknown;

export interface TerminalOutputWriterOptions {
	/** Runs the flush once per frame; defaults to requestAnimationFrame in visible pages. */
	schedule?: (callback: () => void) => ScheduleHandle;
	cancel?: (handle: ScheduleHandle) => void;
	sendPause: () => void;
	sendResume: () => void;
	/** Bytes written between acknowledged writes; also the most output held before a flush. */
	limit?: number;
	/** Unacknowledged writes after which ttyd is asked to stop reading the PTY. */
	highWater?: number;
	/** Unacknowledged writes below which ttyd is asked to read the PTY again. */
	lowWater?: number;
}

/**
 * Coalesces WebSocket output into one xterm write per animation frame and applies ttyd's
 * flow control: every `limit` bytes a write carries a parse callback, and when too many of
 * those are outstanding ttyd pauses the PTY until xterm catches up. Neither this queue nor
 * xterm's write buffer can grow without bound while a TUI floods output.
 */
export class TerminalOutputWriter {
	#terminal: TerminalSink;
	#schedule: (callback: () => void) => ScheduleHandle;
	#cancel: (handle: ScheduleHandle) => void;
	#sendPause: () => void;
	#sendResume: () => void;
	#limit: number;
	#highWater: number;
	#lowWater: number;
	#queue: Uint8Array[] = [];
	#queuedBytes = 0;
	#scheduled: ScheduleHandle | undefined;
	#written = 0;
	#pending = 0;
	#paused = false;
	#disposed = false;

	constructor(terminal: TerminalSink, options: TerminalOutputWriterOptions) {
		this.#terminal = terminal;
		this.#schedule = options.schedule ?? scheduleFrame;
		this.#cancel = options.cancel ?? cancelFrame;
		this.#sendPause = options.sendPause;
		this.#sendResume = options.sendResume;
		this.#limit = options.limit ?? 100_000;
		this.#highWater = options.highWater ?? 10;
		this.#lowWater = options.lowWater ?? 4;
	}

	push(chunk: Uint8Array | string) {
		if (this.#disposed) return;
		const bytes = typeof chunk === 'string' ? encoder.encode(chunk) : chunk;
		if (bytes.length === 0) return;
		this.#queue.push(bytes);
		this.#queuedBytes += bytes.length;
		if (this.#queuedBytes > this.#limit) {
			this.flush();
			return;
		}
		if (this.#scheduled === undefined) {
			this.#scheduled = this.#schedule(() => {
				this.#scheduled = undefined;
				this.flush();
			});
		}
	}

	flush() {
		if (this.#scheduled !== undefined) {
			this.#cancel(this.#scheduled);
			this.#scheduled = undefined;
		}
		if (this.#disposed || this.#queue.length === 0) return;
		const data = concat(this.#queue, this.#queuedBytes);
		this.#queue = [];
		this.#queuedBytes = 0;
		this.#written += data.length;
		if (this.#written <= this.#limit) {
			this.#terminal.write(data);
			return;
		}
		this.#written = 0;
		this.#pending += 1;
		this.#terminal.write(data, () => this.#acknowledge());
		if (!this.#paused && this.#pending > this.#highWater) {
			this.#paused = true;
			this.#sendPause();
		}
	}

	dispose() {
		this.#disposed = true;
		if (this.#scheduled !== undefined) this.#cancel(this.#scheduled);
		this.#scheduled = undefined;
		this.#queue = [];
		this.#queuedBytes = 0;
	}

	#acknowledge() {
		if (this.#disposed) return;
		this.#pending = Math.max(this.#pending - 1, 0);
		if (this.#paused && this.#pending < this.#lowWater) {
			this.#paused = false;
			this.#sendResume();
		}
	}
}

function concat(chunks: Uint8Array[], length: number): Uint8Array {
	if (chunks.length === 1) return chunks[0];
	const data = new Uint8Array(length);
	let offset = 0;
	for (const chunk of chunks) {
		data.set(chunk, offset);
		offset += chunk.length;
	}
	return data;
}

// Hidden pages do not run animation frames; a timer keeps output parsing there.
function scheduleFrame(callback: () => void): ScheduleHandle {
	if (typeof requestAnimationFrame === 'function' && typeof document !== 'undefined' && !document.hidden) {
		return { frame: requestAnimationFrame(callback) };
	}
	return { timer: setTimeout(callback, 16) };
}

function cancelFrame(handle: ScheduleHandle) {
	const scheduled = handle as { frame?: number; timer?: ReturnType<typeof setTimeout> };
	if (scheduled.frame !== undefined) cancelAnimationFrame(scheduled.frame);
	if (scheduled.timer !== undefined) clearTimeout(scheduled.timer);
}

interface Timers {
	setTimeout: (callback: () => void, delay: number) => unknown;
	clearTimeout: (handle: unknown) => void;
}

export interface DebouncedTask {
	(): void;
	/** Runs a pending call now. */
	flush(): void;
	cancel(): void;
}

/** Trailing debounce: a burst of calls runs `task` once, `delay` ms after the last call. */
export function createDebouncedTask(
	task: () => void,
	delay: number,
	timers: Timers = {
		setTimeout: (callback, ms) => setTimeout(callback, ms),
		clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>)
	}
): DebouncedTask {
	let handle: unknown;
	let queued = false;
	const run = () => {
		handle = undefined;
		queued = false;
		task();
	};
	const debounced = (() => {
		if (queued) timers.clearTimeout(handle);
		queued = true;
		handle = timers.setTimeout(run, delay);
	}) as DebouncedTask;
	debounced.cancel = () => {
		if (queued) timers.clearTimeout(handle);
		handle = undefined;
		queued = false;
	};
	debounced.flush = () => {
		if (!queued) return;
		timers.clearTimeout(handle);
		run();
	};
	return debounced;
}

/** Sends a resize to the PTY only when the grid actually changes, so tmux redraws once. */
export class TerminalResizeForwarder {
	#send: (size: TerminalSize) => void;
	#last: TerminalSize | undefined;

	constructor(send: (size: TerminalSize) => void) {
		this.#send = send;
	}

	/** Records the size the server already knows, for example from the connect message. */
	reset(size?: TerminalSize) {
		this.#last = size ? { ...size } : undefined;
	}

	forward(size: TerminalSize) {
		if (!(size.columns > 0 && size.rows > 0)) return;
		if (this.#last && this.#last.columns === size.columns && this.#last.rows === size.rows) return;
		this.#last = { ...size };
		this.#send({ columns: size.columns, rows: size.rows });
	}
}

interface FontLoader {
	load(font: string): Promise<unknown>;
}

/**
 * Waits for the bundled terminal faces so xterm measures cells with the real font rather than a
 * fallback. Resolves false when fonts are unavailable or slow; the terminal then opens anyway.
 */
export async function waitForTerminalFonts(
	fonts: FontLoader | undefined,
	faces: readonly string[],
	timeoutMs: number
): Promise<boolean> {
	if (!fonts) return false;
	let timer: ReturnType<typeof setTimeout> | undefined;
	const timeout = new Promise<boolean>((resolve) => {
		timer = setTimeout(() => resolve(false), timeoutMs);
	});
	const loaded = Promise.all(faces.map((face) => fonts.load(face))).then(
		() => true,
		() => false
	);
	try {
		return await Promise.race([loaded, timeout]);
	} finally {
		clearTimeout(timer);
	}
}
