import assert from 'node:assert/strict';
import test from 'node:test';
import {
	anyTabVisible,
	createNotificationCoordinator,
	createSharedThrottleStore,
	electWinner,
	notificationEventId,
	openCoordinationChannel
} from '../../src/lib/features/notifications/notification-coordinator.ts';
import { countsAsUnread } from '../../src/lib/features/notifications/notification-policy.ts';
import { createNotificationSound } from '../../src/lib/features/notifications/notification-sound.ts';
import {
	NOTIFICATION_NAVIGATE,
	NOTIFICATION_NAVIGATE_ACK,
	notificationNavigatePath,
	openNotificationTarget,
	requestClientNavigation
} from '../../src/lib/features/pwa/notification-click.ts';

// --- Unread count --------------------------------------------------------

test('a live-only event does not count as an unread inbox notification', () => {
	assert.equal(countsAsUnread({ type: 'approval_requested', live_only: 'true' }), false);
	assert.equal(countsAsUnread({ type: 'approval_requested', live_only: true }), false);
	assert.equal(countsAsUnread({ type: 'approval_requested' }), true);
	assert.equal(countsAsUnread({ type: 'assigned', live_only: 'false' }), true);
	// Older servers and odd payloads keep the previous behaviour: count it.
	assert.equal(countsAsUnread(undefined), true);
	assert.equal(countsAsUnread('assigned'), true);
});

// --- Election ------------------------------------------------------------

test('the event id is the backend one, else derived from what the event is about', () => {
	assert.equal(notificationEventId({ type: 'assigned', event_id: 'e-1', issue_id: 'i-1' }), 'e-1');
	const derived = notificationEventId({ type: 'assigned', issue_id: 'i-1', title: 'You were assigned' });
	assert.equal(derived, notificationEventId({ type: 'assigned', issue_id: 'i-1', title: 'You were assigned' }));
	assert.notEqual(derived, notificationEventId({ type: 'assigned', issue_id: 'i-2', title: 'You were assigned' }));
	assert.notEqual(derived, notificationEventId({ type: 'mentioned', issue_id: 'i-1', title: 'You were assigned' }));
	assert.equal(typeof notificationEventId({ type: 'cycle_changed' }), 'string');
});

test('a visible tab wins, then one that can play audio, then the lowest tab id', () => {
	const claim = (tabId, visible = false, audible = true) => ({ tabId, visible, audible });
	assert.equal(electWinner([]), null);
	assert.equal(electWinner([claim('b'), claim('a'), claim('c')]), 'a');
	assert.equal(electWinner([claim('a'), claim('c', true), claim('b')]), 'c');
	assert.equal(electWinner([claim('b', true), claim('c', true), claim('a')]), 'b');
	assert.equal(electWinner([claim('a', false, false), claim('b', false, true)]), 'b');
	assert.equal(electWinner([claim('a', false, true), claim('b', true, false)]), 'b', 'visibility outranks audio');
	// The order claims arrived in does not matter, so every tab agrees.
	const claims = [claim('d'), claim('b', true), claim('a'), claim('c', true)];
	assert.equal(electWinner(claims), electWinner([...claims].reverse()));
	assert.equal(anyTabVisible(claims), true);
	assert.equal(anyTabVisible([claim('a'), claim('b')]), false);
	assert.equal(anyTabVisible([]), false);
});

/** An in-memory BroadcastChannel: a message reaches every other open port. */
function fakeBus() {
	const ports = new Set();
	return {
		open() {
			const port = {
				onmessage: null,
				postMessage(message) {
					const data = structuredClone(message);
					for (const other of ports) if (other !== port) other.onmessage?.({ data });
				},
				close() {
					ports.delete(port);
				}
			};
			ports.add(port);
			return port;
		}
	};
}

/** Election windows that close when the test says so. */
function manualWindows() {
	const waiting = [];
	return {
		wait: () => new Promise((resolve) => waiting.push(resolve)),
		closeAll() {
			for (const resolve of waiting.splice(0)) resolve();
		}
	};
}

function tabs(bus, windows, specs) {
	return specs.map(([tabId, visible, audible = true]) =>
		createNotificationCoordinator({
			tabId,
			channel: bus.open(),
			isVisible: () => visible,
			isAudible: () => audible,
			wait: windows.wait
		})
	);
}

test('exactly one of several tabs announces an event, and all know a visible tab exists', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	const [a, b, c] = tabs(bus, windows, [
		['a', false],
		['b', true],
		['c', false]
	]);
	const results = [a.coordinate('e-1'), b.coordinate('e-1'), c.coordinate('e-1')];
	windows.closeAll();
	assert.deepEqual(await Promise.all(results), [
		{ act: false, visible: true },
		{ act: true, visible: true },
		{ act: false, visible: true }
	]);
});

test('hidden tabs elect one of them, which reports that no tab is visible', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	const [b, a] = tabs(bus, windows, [
		['b', false],
		['a', false]
	]);
	const results = [b.coordinate('e-1'), a.coordinate('e-1')];
	windows.closeAll();
	assert.deepEqual(await Promise.all(results), [
		{ act: false, visible: false },
		{ act: true, visible: false }
	]);
});

test('the winner can differ from one event to the next, but is always one tab', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	let visible = 'a';
	const make = (tabId) =>
		createNotificationCoordinator({ tabId, channel: bus.open(), isVisible: () => visible === tabId, wait: windows.wait });
	const a = make('a');
	const b = make('b');
	let results = [a.coordinate('e-1'), b.coordinate('e-1')];
	windows.closeAll();
	assert.deepEqual((await Promise.all(results)).map((result) => result.act), [true, false]);
	visible = 'b';
	results = [a.coordinate('e-2'), b.coordinate('e-2')];
	windows.closeAll();
	assert.deepEqual((await Promise.all(results)).map((result) => result.act), [false, true]);
});

test('a tab whose copy of the event arrives after another tab announced it stays quiet', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	const [hidden, visible] = tabs(bus, windows, [
		['a', false],
		['b', true]
	]);
	const first = hidden.coordinate('e-1');
	windows.closeAll();
	assert.deepEqual(await first, { act: true, visible: false });
	// The visible tab would have won the election, but the event is done.
	const late = visible.coordinate('e-1');
	windows.closeAll();
	assert.deepEqual(await late, { act: false, visible: true });
});

test('a duplicate delivery of one event to the same tab is announced once', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	const [only] = tabs(bus, windows, [['a', true]]);
	const results = [only.coordinate('e-1'), only.coordinate('e-1')];
	windows.closeAll();
	assert.deepEqual((await Promise.all(results)).map((result) => result.act), [true, false]);
	assert.equal((await only.coordinate('e-1')).act, false);
	const next = only.coordinate('e-2');
	windows.closeAll();
	assert.equal((await next).act, true);
});

test('tabs that did not receive the event are not part of its election', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	// "other" is a visible tab of another workspace: it never claims e-1.
	const [hidden] = tabs(bus, windows, [
		['a', false],
		['other', true]
	]);
	const result = hidden.coordinate('e-1');
	windows.closeAll();
	assert.deepEqual(await result, { act: true, visible: false });
});

test('without BroadcastChannel a tab acts alone, immediately and without errors', async (t) => {
	const errors = t.mock.method(console, 'error', () => {});
	const alone = createNotificationCoordinator({
		tabId: 'a',
		channel: null,
		isVisible: () => false,
		wait: () => assert.fail('no election window without a channel')
	});
	assert.deepEqual(await alone.coordinate('e-1'), { act: true, visible: false });

	// A channel that throws on use degrades the same way.
	const broken = createNotificationCoordinator({
		tabId: 'b',
		channel: {
			onmessage: null,
			postMessage() {
				throw new Error('closed');
			}
		},
		isVisible: () => true,
		wait: async () => {}
	});
	assert.deepEqual(await broken.coordinate('e-1'), { act: true, visible: true });
	broken.close();

	const scope = /** @type {any} */ (globalThis);
	const original = scope.BroadcastChannel;
	t.after(() => {
		scope.BroadcastChannel = original;
	});
	scope.BroadcastChannel = undefined;
	assert.equal(openCoordinationChannel(), null);
	scope.BroadcastChannel = class {
		constructor() {
			throw new Error('blocked');
		}
	};
	assert.equal(openCoordinationChannel(), null);
	assert.equal(errors.mock.callCount(), 0);
});

test('malformed and foreign channel messages are ignored', async () => {
	const bus = fakeBus();
	const windows = manualWindows();
	const [only] = tabs(bus, windows, [['b', false]]);
	const intruder = bus.open();
	for (const message of [null, 'claim', { kind: 'claim' }, { kind: 'other', eventId: 'e-1', tabId: 'a' }, { eventId: 'e-1' }]) {
		intruder.postMessage(message);
	}
	const result = only.coordinate('e-1');
	windows.closeAll();
	assert.deepEqual(await result, { act: true, visible: false });
});

// --- Cross-tab sound throttle ---------------------------------------------

function fakeStorage() {
	const values = new Map();
	return {
		values,
		getItem: (key) => (values.has(key) ? values.get(key) : null),
		setItem: (key, value) => void values.set(key, String(value)),
		removeItem: (key) => void values.delete(key)
	};
}

function fakeAudio() {
	const param = { setValueAtTime() {}, linearRampToValueAtTime() {}, exponentialRampToValueAtTime() {} };
	let oscillators = 0;
	return {
		get oscillators() {
			return oscillators;
		},
		create: () => ({
			state: 'running',
			currentTime: 0,
			destination: {},
			resume: async () => {},
			createOscillator() {
				oscillators++;
				return { type: 'sine', frequency: param, connect() {}, start() {}, stop() {} };
			},
			createGain: () => ({ gain: param, connect() {} })
		})
	};
}

test('the five second sound throttle holds across tabs sharing storage', async () => {
	const storage = fakeStorage();
	let now = 10_000;
	const player = () => {
		const audio = fakeAudio();
		const sound = createNotificationSound({
			createContext: audio.create,
			now: () => now,
			canStart: () => true,
			throttleStore: createSharedThrottleStore(storage)
		});
		return { audio, sound };
	};
	const first = player();
	const second = player();

	assert.equal(await first.sound.play('pop', 'comment'), true);
	now += 2_000;
	assert.equal(await second.sound.play('pop', 'comment'), false, 'another tab played this key 2 s ago');
	assert.equal(second.audio.oscillators, 0);
	assert.equal(await second.sound.play('pop', 'mentioned'), true, 'another key is independent');
	now += 3_000;
	assert.equal(await second.sound.play('pop', 'comment'), true, 'allowed again after five seconds');
	now += 4_999;
	assert.equal(await first.sound.play('pop', 'comment'), false, 'and the first tab sees the second tab play');
	now += 1;
	assert.equal(await first.sound.play('pop', 'comment'), true);
});

test('a sound that could not play frees the shared throttle window', async () => {
	const storage = fakeStorage();
	const blocked = createNotificationSound({
		createContext: () => null,
		now: () => 1_000,
		canStart: () => true,
		throttleStore: createSharedThrottleStore(storage)
	});
	assert.equal(await blocked.play('pop', 'comment'), false);
	assert.equal(storage.values.size, 0);
	const audio = fakeAudio();
	const other = createNotificationSound({
		createContext: audio.create,
		now: () => 1_500,
		canStart: () => true,
		throttleStore: createSharedThrottleStore(storage)
	});
	assert.equal(await other.play('pop', 'comment'), true);
});

test('the throttle store falls back to the tab when storage is missing, broken or holds junk', () => {
	for (const storage of [
		null,
		undefined,
		{
			getItem() {
				throw new Error('denied');
			},
			setItem() {
				throw new Error('quota');
			},
			removeItem() {
				throw new Error('denied');
			}
		}
	]) {
		const store = createSharedThrottleStore(storage);
		assert.equal(store.get('comment'), undefined);
		store.set('comment', 5);
		assert.equal(store.get('comment'), 5);
		store.delete('comment');
		assert.equal(store.get('comment'), undefined);
	}
	const storage = fakeStorage();
	const store = createSharedThrottleStore(storage);
	store.set('comment', 7);
	const [key] = storage.values.keys();
	storage.setItem(key, 'not a number');
	assert.equal(createSharedThrottleStore(storage).get('comment'), undefined);
	assert.equal(store.get('comment'), 7, 'the tab still remembers its own play');
});

test('the player reports whether it may start audio and never throws doing so', () => {
	assert.equal(createNotificationSound({ canStart: () => true }).canStart(), true);
	assert.equal(createNotificationSound({ canStart: () => false }).canStart(), false);
	const failing = createNotificationSound({
		canStart: () => {
			throw new Error('no navigator');
		}
	});
	assert.equal(failing.canStart(), false);
});

// --- Notification click: route in the open page ----------------------------

const origin = 'https://kuayle.example';
const target = `${origin}/acme/issue/KUA-7`;

test('a page follows only same-origin navigate messages', () => {
	const message = (url, type = NOTIFICATION_NAVIGATE) => ({ type, url });
	assert.equal(notificationNavigatePath(message(`${target}?tab=activity#c1`), origin), '/acme/issue/KUA-7?tab=activity#c1');
	assert.equal(notificationNavigatePath(message('/acme/inbox'), origin), '/acme/inbox');
	assert.equal(notificationNavigatePath(message('https://evil.example/acme/inbox'), origin), null);
	assert.equal(notificationNavigatePath(message('//evil.example/acme'), origin), null);
	assert.equal(notificationNavigatePath(message('javascript:alert(1)'), origin), null);
	assert.equal(notificationNavigatePath(message(target, 'SKIP_WAITING'), origin), null);
	assert.equal(notificationNavigatePath(message(undefined), origin), null);
	assert.equal(notificationNavigatePath(null, origin), null);
	assert.equal(notificationNavigatePath('NOTIFICATION_NAVIGATE', origin), null);
});

/** A MessageChannel whose page side answers (or not) when it receives the port. */
function fakeChannelEnv({ timeoutMs = 1_000 } = {}) {
	const timers = [];
	const env = {
		timeoutMs,
		created: [],
		cleared: 0,
		createChannel() {
			const port1 = { onmessage: null, closed: false, close: () => void (port1.closed = true) };
			const port2 = { postMessage: (data) => port1.onmessage?.({ data }) };
			env.created.push(port1);
			return { port1, port2 };
		},
		setTimeout(callback, ms) {
			timers.push({ callback, ms });
			return timers.length;
		},
		clearTimeout() {
			env.cleared++;
		},
		expire() {
			for (const timer of timers.splice(0)) timer.callback();
		},
		timers
	};
	return env;
}

test('the worker asks the page to navigate and resolves on its acknowledgement', async () => {
	const env = fakeChannelEnv();
	const posted = [];
	const page = {
		postMessage(message, transfer) {
			posted.push(message);
			transfer[0].postMessage({ type: NOTIFICATION_NAVIGATE_ACK });
		}
	};
	assert.equal(await requestClientNavigation(page, target, env), true);
	assert.deepEqual(posted, [{ type: NOTIFICATION_NAVIGATE, url: target }]);
	assert.equal(env.timers[0].ms, 1_000);
	assert.equal(env.cleared, 1);
	assert.equal(env.created[0].closed, true);
});

test('a page that does not acknowledge in time, answers something else or cannot be messaged is not trusted', async () => {
	const silent = fakeChannelEnv({ timeoutMs: 250 });
	const pending = requestClientNavigation({ postMessage() {} }, target, silent);
	assert.equal(silent.timers[0].ms, 250);
	silent.expire();
	assert.equal(await pending, false);

	const wrong = fakeChannelEnv();
	const wrongAnswer = requestClientNavigation(
		{ postMessage: (_message, transfer) => transfer[0].postMessage({ type: 'SOMETHING_ELSE' }) },
		target,
		wrong
	);
	wrong.expire();
	assert.equal(await wrongAnswer, false);

	const throwing = {
		postMessage() {
			throw new Error('client gone');
		}
	};
	assert.equal(await requestClientNavigation(throwing, target, fakeChannelEnv()), false);
	const noChannel = {
		createChannel() {
			throw new Error('unsupported');
		},
		setTimeout() {}
	};
	assert.equal(await requestClientNavigation({ postMessage() {} }, target, noChannel), false);
});

function windowClient(path, overrides = {}) {
	const calls = [];
	return {
		url: origin + path,
		calls,
		postMessage() {},
		async focus() {
			calls.push('focus');
		},
		async navigate(url) {
			calls.push(`navigate ${url}`);
			return this;
		},
		...overrides
	};
}

function open(clients, requestNavigation = async () => true) {
	const log = [];
	return openNotificationTarget({
		clients,
		target,
		origin,
		requestNavigation: async (client, url) => {
			log.push(`request ${client.url} -> ${url}`);
			return requestNavigation(client, url);
		},
		openWindow: async (url) => {
			log.push(`open ${url}`);
		}
	}).then((outcome) => ({ outcome, log }));
}

test('a window already on the target is only focused', async () => {
	const there = windowClient('/acme/issue/KUA-7');
	assert.deepEqual(await open([windowClient('/acme/inbox'), there]), { outcome: 'focused', log: [] });
	assert.deepEqual(there.calls, ['focus']);
});

test('an open window elsewhere is focused and routes in-app, without a page load', async () => {
	const elsewhere = windowClient('/acme/inbox');
	assert.deepEqual(await open([elsewhere]), { outcome: 'routed', log: [`request ${origin}/acme/inbox -> ${target}`] });
	assert.deepEqual(elsewhere.calls, ['focus'], 'navigate() is never called');
});

test('a window that does not acknowledge is reloaded onto the target', async () => {
	const elsewhere = windowClient('/acme/inbox');
	const { outcome, log } = await open([elsewhere], async () => false);
	assert.equal(outcome, 'navigated');
	assert.deepEqual(log, [`request ${origin}/acme/inbox -> ${target}`]);
	assert.deepEqual(elsewhere.calls, ['focus', `navigate ${target}`, 'focus']);

	// A failing request is the same as no acknowledgement.
	const other = windowClient('/acme/inbox');
	const failed = await open([other], async () => {
		throw new Error('channel failed');
	});
	assert.equal(failed.outcome, 'navigated');
});

test('a new window opens when no window can be reused', async () => {
	const openLog = [`open ${target}`];
	assert.deepEqual(await open([]), { outcome: 'opened', log: openLog });
	assert.deepEqual(await open([{ ...windowClient(''), url: 'https://other.example/acme' }]), { outcome: 'opened', log: openLog });

	// An uncontrolled window: no acknowledgement, and navigate() rejects.
	const uncontrolled = windowClient('/acme/inbox', {
		async navigate() {
			throw new TypeError('not controlled');
		}
	});
	const { outcome, log } = await open([uncontrolled], async () => false);
	assert.equal(outcome, 'opened');
	assert.deepEqual(log, [`request ${origin}/acme/inbox -> ${target}`, `open ${target}`]);

	// navigate() resolving to null (the window went elsewhere) also opens one.
	const gone = windowClient('/acme/inbox', { navigate: async () => null });
	assert.equal((await open([gone], async () => false)).outcome, 'opened');

	// A window that refuses focus is not asked to route unseen.
	const unfocusable = windowClient('/acme/issue/KUA-7', {
		async focus() {
			throw new Error('not allowed');
		}
	});
	assert.deepEqual(await open([unfocusable]), { outcome: 'opened', log: openLog });
});

test('opening never throws, even when no window can be opened', async () => {
	const outcome = await openNotificationTarget({
		clients: [],
		target,
		origin,
		requestNavigation: async () => false,
		openWindow: async () => {
			throw new Error('blocked');
		}
	});
	assert.equal(outcome, 'none');
});
