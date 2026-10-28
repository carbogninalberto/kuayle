import assert from 'node:assert/strict';
import test from 'node:test';
import {
	TerminalOutputWriter,
	TerminalResizeForwarder,
	createDebouncedTask,
	waitForTerminalFonts
} from '../../src/lib/features/dev-machines/terminal-io.ts';

function manualScheduler() {
	const queued = [];
	return {
		schedule: (callback) => {
			queued.push(callback);
			return queued.length;
		},
		cancel: (handle) => {
			queued[handle - 1] = undefined;
		},
		run: () => {
			const callbacks = queued.splice(0);
			for (const callback of callbacks) callback?.();
		},
		get pending() {
			return queued.filter(Boolean).length;
		}
	};
}

function fakeTerminal() {
	const writes = [];
	return {
		writes,
		write: (data, callback) => writes.push({ data, callback })
	};
}

test('coalesces output chunks into one terminal write per frame', () => {
	const scheduler = manualScheduler();
	const terminal = fakeTerminal();
	const writer = new TerminalOutputWriter(terminal, { ...scheduler, sendPause() {}, sendResume() {} });

	writer.push(new Uint8Array([104, 105]));
	writer.push('!');
	writer.push(new Uint8Array([10]));
	assert.equal(terminal.writes.length, 0);
	assert.equal(scheduler.pending, 1);

	scheduler.run();
	assert.equal(terminal.writes.length, 1);
	assert.deepEqual([...terminal.writes[0].data], [104, 105, 33, 10]);
});

test('pauses ttyd when xterm falls behind and resumes after it catches up', () => {
	const scheduler = manualScheduler();
	const terminal = fakeTerminal();
	const signals = [];
	const writer = new TerminalOutputWriter(terminal, {
		...scheduler,
		limit: 4,
		highWater: 2,
		lowWater: 2,
		sendPause: () => signals.push('pause'),
		sendResume: () => signals.push('resume')
	});

	for (let index = 0; index < 4; index += 1) {
		writer.push(new Uint8Array(5));
		scheduler.run();
	}
	const acknowledged = terminal.writes.filter((write) => write.callback);
	assert.equal(acknowledged.length, 4);
	// Pause is sent once, even though the backlog keeps growing.
	assert.deepEqual(signals, ['pause']);

	for (const write of acknowledged.slice(0, 3)) write.callback();
	assert.deepEqual(signals, ['pause', 'resume']);
	acknowledged[3].callback();
	assert.deepEqual(signals, ['pause', 'resume']);
});

test('flushes immediately instead of buffering beyond the flow-control limit', () => {
	const scheduler = manualScheduler();
	const terminal = fakeTerminal();
	const writer = new TerminalOutputWriter(terminal, { ...scheduler, limit: 8, sendPause() {}, sendResume() {} });

	writer.push(new Uint8Array(6));
	writer.push(new Uint8Array(6));
	assert.equal(terminal.writes.length, 1);
	assert.equal(terminal.writes[0].data.length, 12);
	assert.equal(scheduler.pending, 0);
});

test('drops queued output and ignores late callbacks after disposal', () => {
	const scheduler = manualScheduler();
	const terminal = fakeTerminal();
	const signals = [];
	const writer = new TerminalOutputWriter(terminal, {
		...scheduler,
		limit: 4,
		highWater: 0,
		sendPause: () => signals.push('pause'),
		sendResume: () => signals.push('resume')
	});
	writer.push(new Uint8Array(5));
	scheduler.run();
	assert.deepEqual(signals, ['pause']);
	writer.push(new Uint8Array(2));
	writer.dispose();
	scheduler.run();
	writer.push(new Uint8Array(2));
	scheduler.run();
	terminal.writes[0].callback();
	assert.equal(terminal.writes.length, 1);
	assert.deepEqual(signals, ['pause']);
});

test('debounces bursts of resize notifications into one trailing fit', () => {
	const timers = [];
	let fits = 0;
	const fit = createDebouncedTask(() => (fits += 1), 40, {
		setTimeout: (callback, delay) => timers.push({ callback, delay }) && timers.length,
		clearTimeout: (handle) => {
			timers[handle - 1].cancelled = true;
		}
	});
	fit();
	fit();
	fit();
	const live = timers.filter((timer) => !timer.cancelled);
	assert.equal(live.length, 1);
	assert.equal(live[0].delay, 40);
	live[0].callback();
	assert.equal(fits, 1);

	fit();
	fit.cancel();
	assert.equal(timers.filter((timer) => !timer.cancelled).length, 1);
	fit.flush();
	assert.equal(fits, 1);
});

test('forwards only real size changes to the PTY', () => {
	const sent = [];
	const forwarder = new TerminalResizeForwarder((size) => sent.push(size));
	forwarder.reset({ columns: 80, rows: 24 });
	forwarder.forward({ columns: 80, rows: 24 });
	forwarder.forward({ columns: 120, rows: 40 });
	forwarder.forward({ columns: 120, rows: 40 });
	forwarder.forward({ columns: 0, rows: 40 });
	forwarder.reset({ columns: 100, rows: 30 });
	forwarder.forward({ columns: 120, rows: 40 });
	assert.deepEqual(sent, [
		{ columns: 120, rows: 40 },
		{ columns: 120, rows: 40 }
	]);
});

test('waits for the terminal font faces but never longer than the timeout', async () => {
	const requested = [];
	const loaded = await waitForTerminalFonts(
		{ load: async (font) => requested.push(font) && [] },
		['400 13px "JetBrains Mono"', '700 13px "JetBrains Mono"'],
		1000
	);
	assert.equal(loaded, true);
	assert.deepEqual(requested, ['400 13px "JetBrains Mono"', '700 13px "JetBrains Mono"']);

	const timedOut = await waitForTerminalFonts({ load: () => new Promise(() => {}) }, ['13px x'], 5);
	assert.equal(timedOut, false);
	const failed = await waitForTerminalFonts({ load: () => Promise.reject(new Error('no font')) }, ['13px x'], 1000);
	assert.equal(failed, false);
	assert.equal(await waitForTerminalFonts(undefined, ['13px x'], 1000), false);
});
