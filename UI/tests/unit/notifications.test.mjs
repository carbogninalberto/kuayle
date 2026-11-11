import assert from 'node:assert/strict';
import test from 'node:test';
import {
	NOTIFICATION_EVENT_KEYS,
	NOTIFICATION_SOUNDS,
	decideNotification,
	eventKeyForType,
	normalizeNotificationEvents,
	normalizeNotificationSound,
	notificationSubject
} from '../../src/lib/features/notifications/notification-policy.ts';
import { notificationTypeMessageKey } from '../../src/lib/features/notifications/notification-labels.ts';
import { createOnScreenSubjects } from '../../src/lib/features/notifications/on-screen-subjects.ts';
import { readPermission, requestPermission } from '../../src/lib/features/notifications/permissions.ts';
import {
	buildBrowserNotification,
	notificationTag,
	notificationUrl,
	showBrowserNotification
} from '../../src/lib/features/notifications/browser-notifications.ts';
import {
	SOUND_PRESETS,
	SOUND_THROTTLE_MS,
	createNotificationSound
} from '../../src/lib/features/notifications/notification-sound.ts';
import { chooseWindowClient, notificationTargetUrl } from '../../src/lib/features/pwa/notification-click.ts';

const allOn = Object.fromEntries(NOTIFICATION_EVENT_KEYS.map((key) => [key, true]));
const preferences = (overrides = {}) => ({
	browserNotifications: true,
	sound: 'chime',
	events: { ...allOn },
	...overrides
});
const decide = (overrides = {}) =>
	decideNotification({
		payload: { type: 'assigned', actor_id: 'user-2' },
		preferences: preferences(),
		permission: 'granted',
		visibility: 'hidden',
		currentUserId: 'user-1',
		...overrides
	});

// ---- policy ---------------------------------------------------------------

test('maps every notification type to its preference key', () => {
	assert.deepEqual(NOTIFICATION_EVENT_KEYS, [
		'approval_requested',
		'task_finished',
		'review_needed',
		'assigned',
		'mentioned',
		'comment',
		'issue_updated'
	]);
	assert.equal(eventKeyForType('approval_requested'), 'approval_requested');
	assert.equal(eventKeyForType('agent_task_finished'), 'task_finished');
	assert.equal(eventKeyForType('review_needed'), 'review_needed');
	assert.equal(eventKeyForType('assigned'), 'assigned');
	assert.equal(eventKeyForType('mentioned'), 'mentioned');
	assert.equal(eventKeyForType('commented'), 'comment');
	for (const type of [
		'status_changed',
		'priority_changed',
		'issue_created',
		'issue_updated',
		'due_date_changed',
		'label_added',
		'cycle_changed',
		'issues_deleted',
		'something_new',
		undefined
	]) {
		assert.equal(eventKeyForType(type), 'issue_updated', String(type));
	}
});

test('a hidden tab with permission gets a browser notification and a sound', () => {
	assert.deepEqual(decide(), { showBrowser: true, playSound: true, eventKey: 'assigned' });
});

test('a visible tab only gets the sound', () => {
	assert.deepEqual(decide({ visibility: 'visible' }), { showBrowser: false, playSound: true, eventKey: 'assigned' });
});

test('browser notifications need the preference and a granted permission', () => {
	for (const permission of ['default', 'denied', 'unsupported']) {
		assert.deepEqual(decide({ permission }), { showBrowser: false, playSound: true, eventKey: 'assigned' }, permission);
	}
	assert.equal(decide({ preferences: preferences({ browserNotifications: false }) }).showBrowser, false);
});

test('sound off silences the event but keeps the browser notification', () => {
	assert.deepEqual(decide({ preferences: preferences({ sound: 'off' }) }), {
		showBrowser: true,
		playSound: false,
		eventKey: 'assigned'
	});
});

test('a muted event key does nothing, and a missing key counts as enabled', () => {
	const muted = preferences({ events: { ...allOn, assigned: false } });
	assert.deepEqual(decide({ preferences: muted }), { showBrowser: false, playSound: false, eventKey: 'assigned' });
	// Muting one key leaves the others alone.
	assert.equal(decide({ preferences: muted, payload: { type: 'mentioned' } }).showBrowser, true);
	assert.deepEqual(decide({ preferences: preferences({ events: {} }) }), {
		showBrowser: true,
		playSound: true,
		eventKey: 'assigned'
	});
});

test('events the current user triggered do nothing', () => {
	assert.deepEqual(decide({ payload: { type: 'commented', actor_id: 'user-1' } }), {
		showBrowser: false,
		playSound: false,
		eventKey: 'comment'
	});
	// No actor and no signed-in user must not be treated as "own".
	assert.equal(decide({ payload: { type: 'commented' }, currentUserId: undefined }).showBrowser, true);
});

test('an approval asked by the conversation on screen is quiet', () => {
	const payload = { type: 'approval_requested', approval_id: 'a1', conversation_id: 'c1' };
	const quiet = { showBrowser: false, playSound: false, eventKey: 'approval_requested' };
	assert.deepEqual(decide({ payload, subjectOnScreen: true, visibility: 'visible' }), quiet);
	// Whatever the announcing tab's own visibility is: another tab shows the chat.
	assert.deepEqual(decide({ payload, subjectOnScreen: true }), quiet);
	// The chat is not on screen: announced as before.
	assert.deepEqual(decide({ payload, subjectOnScreen: false }), { ...quiet, showBrowser: true, playSound: true });
	assert.deepEqual(decide({ payload }), { ...quiet, showBrowser: true, playSound: true });
	// Only an approval is ever quietened this way.
	assert.equal(decide({ payload: { type: 'assigned', conversation_id: 'c1' }, subjectOnScreen: true }).playSound, true);
});

test('only an approval asked in a conversation has an on-screen subject', () => {
	assert.equal(notificationSubject({ type: 'approval_requested', conversation_id: 'c1' }), 'conversation:c1');
	assert.equal(
		notificationSubject({ type: 'approval_requested', conversation_id: 'c1', issue_id: 'i1' }),
		'conversation:c1'
	);
	assert.equal(notificationSubject({ type: 'approval_requested', approval_id: 'a1' }), null);
	assert.equal(notificationSubject({ type: 'approval_requested', conversation_id: '' }), null);
	assert.equal(notificationSubject({ type: 'agent_task_finished', conversation_id: 'c1' }), null);
	assert.equal(notificationSubject({ type: 'commented', issue_id: 'i1' }), null);
});

test('subjects are on screen while something shows them', () => {
	const subjects = createOnScreenSubjects();
	assert.equal(subjects.has('conversation:c1'), false);
	const first = subjects.show('conversation:c1');
	const second = subjects.show('conversation:c1');
	assert.equal(subjects.has('conversation:c1'), true);
	assert.equal(subjects.has('conversation:c2'), false);
	first();
	first(); // Releasing twice does not release the other holder.
	assert.equal(subjects.has('conversation:c1'), true);
	second();
	assert.equal(subjects.has('conversation:c1'), false);
});

// ---- labels ---------------------------------------------------------------

test('every stored notification type has a label, shared by the inbox and the browser notification', () => {
	assert.equal(notificationTypeMessageKey('approval_requested'), 'notify.title.approval_requested');
	assert.equal(notificationTypeMessageKey('agent_task_finished'), 'notify.title.agent_task_finished');
	assert.equal(notificationTypeMessageKey('review_needed'), 'notify.title.review_needed');
	assert.equal(notificationTypeMessageKey('issues_deleted'), 'notify.title.issues_deleted');
	for (const type of [
		'status_changed',
		'assigned',
		'commented',
		'mentioned',
		'priority_changed',
		'issue_created',
		'issue_updated',
		'due_date_changed',
		'label_added',
		'cycle_changed'
	]) {
		assert.equal(notificationTypeMessageKey(type), `inbox.type.${type}`);
	}
	// Unknown types, and names inherited from Object, have no label.
	assert.equal(notificationTypeMessageKey('something_new'), null);
	assert.equal(notificationTypeMessageKey('toString'), null);
	assert.equal(notificationTypeMessageKey(''), null);
});

test('preference values from the server are normalised defensively', () => {
	assert.deepEqual(normalizeNotificationEvents(undefined), allOn);
	assert.deepEqual(normalizeNotificationEvents({ comment: false, bogus: false, assigned: 'no' }), {
		...allOn,
		comment: false
	});
	assert.deepEqual(NOTIFICATION_SOUNDS, ['off', 'chime', 'bell', 'pop', 'drop', 'pulse']);
	assert.equal(normalizeNotificationSound('bell'), 'bell');
	assert.equal(normalizeNotificationSound('off'), 'off');
	assert.equal(normalizeNotificationSound('klaxon'), 'chime');
	assert.equal(normalizeNotificationSound(undefined), 'chime');
});

// ---- permission -----------------------------------------------------------

test('reads the permission state and reports unsupported browsers', async () => {
	assert.equal(readPermission({}), 'unsupported');
	assert.equal(readPermission({ Notification: { permission: 'granted' } }), 'granted');
	assert.equal(readPermission({ Notification: { permission: 'denied' } }), 'denied');
	assert.equal(readPermission({ Notification: { permission: 'default' } }), 'default');
	assert.equal(readPermission({ Notification: { permission: 'weird' } }), 'default');
	assert.equal(await requestPermission({}), 'unsupported');
});

test('requesting permission returns the answer and never throws', async () => {
	const scope = {
		Notification: {
			permission: 'default',
			requestPermission: async () => {
				scope.Notification.permission = 'granted';
				return 'granted';
			}
		}
	};
	assert.equal(await requestPermission(scope), 'granted');
	const failing = {
		Notification: {
			permission: 'default',
			requestPermission: () => {
				throw new Error('blocked');
			}
		}
	};
	assert.equal(await requestPermission(failing), 'default');
});

// ---- browser notification -------------------------------------------------

test('tags dedupe per type and subject', () => {
	assert.equal(notificationTag({ type: 'assigned', issue_id: 'i1', approval_id: 'a1', task_id: 't1' }), 'assigned:i1');
	assert.equal(notificationTag({ type: 'approval_requested', approval_id: 'a1', task_id: 't1' }), 'approval_requested:a1');
	assert.equal(notificationTag({ type: 'agent_task_finished', task_id: 't1' }), 'agent_task_finished:t1');
	assert.equal(notificationTag({ type: 'issues_deleted' }), 'issues_deleted:workspace');
});

test('an approval deep links to the conversation where it is decided, then the issue, then the inbox', () => {
	assert.equal(
		notificationUrl('acme', {
			type: 'approval_requested',
			approval_id: 'a1',
			issue_id: 'i1',
			issue_identifier: 'KUA-7',
			conversation_id: 'c1'
		}),
		'/acme/assistant?conversation=c1'
	);
	assert.equal(
		notificationUrl('acme', { type: 'approval_requested', approval_id: 'a1', issue_id: 'i1', issue_identifier: 'KUA-7' }),
		'/acme/issue/KUA-7'
	);
	// Other types with both keep opening the issue.
	assert.equal(
		notificationUrl('acme', { type: 'agent_task_finished', issue_identifier: 'KUA-7', conversation_id: 'c1' }),
		'/acme/issue/KUA-7'
	);
});

test('deep links prefer the issue, then the conversation, then the inbox', () => {
	assert.equal(
		notificationUrl('acme', { type: 'assigned', issue_id: 'i1', issue_identifier: 'KUA-7', conversation_id: 'c1' }),
		'/acme/issue/KUA-7'
	);
	assert.equal(notificationUrl('acme', { type: 'assigned', issue_id: 'i1' }), '/acme/issue/i1');
	assert.equal(
		notificationUrl('acme', { type: 'approval_requested', approval_id: 'a1', conversation_id: 'c 1' }),
		'/acme/assistant?conversation=c%201'
	);
	assert.equal(notificationUrl('acme', { type: 'approval_requested', approval_id: 'a1' }), '/acme/inbox');
	// A deleted issue has no page to open.
	assert.equal(notificationUrl('acme', { type: 'issues_deleted', issue_id: 'i1' }), '/acme/inbox');
});

test('builds silent, non-renotifying options with the payload title as body', () => {
	const built = buildBrowserNotification({
		payload: { type: 'mentioned', issue_id: 'i1', issue_identifier: 'KUA-7', title: 'Fix login' },
		slug: 'acme',
		title: 'You were mentioned'
	});
	assert.equal(built.title, 'You were mentioned');
	assert.deepEqual(built.options, {
		body: 'KUA-7 · Fix login',
		tag: 'mentioned:i1',
		data: { url: '/acme/issue/KUA-7' },
		renotify: false,
		silent: true,
		icon: '/icons/icon-192.png'
	});
	const bare = buildBrowserNotification({ payload: { type: 'review_needed' }, slug: 'acme', title: 'Review needed' });
	assert.equal('body' in bare.options, false);
	assert.equal(bare.options.tag, 'review_needed:workspace');
});

const built = { title: 'Assigned to you', options: { tag: 'assigned:i1', data: { url: '/acme/issue/KUA-7' }, silent: true } };

test('uses the service worker registration when there is one', async () => {
	const shown = [];
	let constructed = 0;
	const env = {
		serviceWorker: {
			getRegistration: async () => ({ showNotification: async (title, options) => shown.push([title, options]) })
		},
		Notification: class {
			constructor() {
				constructed++;
			}
		}
	};
	assert.equal(await showBrowserNotification(built, env), 'service-worker');
	assert.deepEqual(shown, [[built.title, built.options]]);
	assert.equal(constructed, 0);
});

test('falls back to the Notification constructor and wires the click to the deep link', async () => {
	const created = [];
	class FakeNotification {
		constructor(title, options) {
			this.title = title;
			this.options = options;
			this.closed = false;
			created.push(this);
		}
		close() {
			this.closed = true;
		}
	}
	const opened = [];
	const onClick = (url) => opened.push(url);
	for (const serviceWorker of [
		undefined,
		{ getRegistration: async () => undefined },
		{
			getRegistration: async () => {
				throw new Error('insecure');
			}
		},
		{
			getRegistration: async () => ({
				showNotification: async () => {
					throw new Error('no permission');
				}
			})
		}
	]) {
		created.length = 0;
		assert.equal(await showBrowserNotification(built, { serviceWorker, Notification: FakeNotification, onClick }), 'window');
		assert.equal(created.length, 1);
		assert.equal(created[0].title, built.title);
		assert.deepEqual(created[0].options, built.options);
	}
	created[0].onclick();
	assert.deepEqual(opened, ['/acme/issue/KUA-7']);
	assert.equal(created[0].closed, true);
});

test('never throws when no notification can be shown', async () => {
	assert.equal(await showBrowserNotification(built, {}), 'none');
	assert.equal(
		await showBrowserNotification(built, {
			Notification: class {
				constructor() {
					throw new TypeError('Illegal constructor');
				}
			}
		}),
		'none'
	);
});

// ---- sound ----------------------------------------------------------------

function fakeAudio({ state = 'running', resume } = {}) {
	const audio = { contexts: 0, oscillators: [], resumes: 0 };
	const param = () => ({
		value: 0,
		setValueAtTime() {},
		linearRampToValueAtTime() {},
		exponentialRampToValueAtTime() {}
	});
	audio.create = () => {
		audio.contexts++;
		return {
			state,
			currentTime: 0,
			destination: {},
			resume: () => {
				audio.resumes++;
				return resume ? resume() : Promise.resolve();
			},
			createOscillator() {
				const oscillator = { type: 'sine', frequency: param(), connect() {}, start() {}, stop() {} };
				audio.oscillators.push(oscillator);
				return oscillator;
			},
			createGain: () => ({ gain: param(), connect() {} })
		};
	};
	return audio;
}

test('defines five synthesised presets and no tones for off', () => {
	assert.deepEqual(Object.keys(SOUND_PRESETS), ['chime', 'bell', 'pop', 'drop', 'pulse']);
	const signatures = new Set();
	for (const [name, tones] of Object.entries(SOUND_PRESETS)) {
		assert.ok(tones.length > 0, name);
		for (const tone of tones) {
			assert.ok(tone.frequency > 0 && tone.duration > 0 && tone.start >= 0, name);
			assert.ok(tone.gain > 0 && tone.gain <= 0.3, `${name} stays quiet`);
			assert.ok(tone.start + tone.duration <= 1.5, `${name} stays short`);
		}
		signatures.add(JSON.stringify(tones));
	}
	assert.equal(signatures.size, 5, 'presets differ from each other');
});

test('plays the selected preset lazily and respects off', async () => {
	const audio = fakeAudio();
	const sound = createNotificationSound({ createContext: audio.create, now: () => 0 });
	assert.equal(audio.contexts, 0, 'no context until a sound is needed');
	assert.equal(await sound.play('off', 'assigned'), false);
	assert.equal(await sound.preview('off'), false);
	assert.equal(audio.contexts, 0);
	assert.equal(await sound.play('bell', 'assigned'), true);
	assert.equal(audio.contexts, 1);
	assert.equal(audio.oscillators.length, SOUND_PRESETS.bell.length);
	assert.equal(await sound.play('nonsense', 'mentioned'), false);
});

test('throttles to one sound per five seconds per event key, and previews bypass it', async () => {
	const audio = fakeAudio();
	let now = 1_000;
	const sound = createNotificationSound({ createContext: audio.create, now: () => now });
	assert.equal(SOUND_THROTTLE_MS, 5_000);
	assert.equal(await sound.play('pop', 'comment'), true);
	now += 4_999;
	assert.equal(await sound.play('pop', 'comment'), false, 'same key inside the window');
	assert.equal(await sound.play('pop', 'mentioned'), true, 'another key is independent');
	assert.equal(await sound.preview('pop'), true, 'preview ignores the throttle');
	assert.equal(await sound.preview('pop'), true);
	assert.equal(await sound.play('pop', 'comment'), false, 'previews do not reset the window either way');
	now += 1;
	assert.equal(await sound.play('pop', 'comment'), true, 'allowed again after five seconds');
	assert.equal(audio.contexts, 1, 'one shared context');
	assert.equal(audio.oscillators.length, 5 * SOUND_PRESETS.pop.length);
});

test('degrades silently when audio is missing, blocked or throws', async (t) => {
	const errors = t.mock.method(console, 'error', () => {});
	const warnings = t.mock.method(console, 'warn', () => {});

	const missing = createNotificationSound({ createContext: () => null, now: () => 0 });
	assert.equal(await missing.play('chime', 'assigned'), false);
	assert.equal(await missing.preview('chime'), false);

	const throwing = createNotificationSound({
		createContext: () => {
			throw new Error('NotSupportedError');
		},
		now: () => 0
	});
	assert.equal(await throwing.play('chime', 'assigned'), false);

	const blocked = fakeAudio({ state: 'suspended', resume: () => Promise.reject(new Error('NotAllowedError')) });
	const blockedSound = createNotificationSound({ createContext: blocked.create, now: () => 0 });
	assert.equal(await blockedSound.play('chime', 'assigned'), false);
	assert.equal(blocked.resumes, 1);
	assert.equal(blocked.oscillators.length, 0);

	// Before the first user gesture nothing is even attempted, so the browser logs no autoplay warning.
	const early = fakeAudio();
	const earlySound = createNotificationSound({ createContext: early.create, now: () => 0, canStart: () => false });
	assert.equal(await earlySound.play('chime', 'assigned'), false);
	assert.equal(early.contexts, 0);
	assert.equal(await earlySound.preview('chime'), true, 'a preview is itself a user gesture');

	assert.equal(errors.mock.callCount(), 0);
	assert.equal(warnings.mock.callCount(), 0);
});

test('a sound that could not play does not use up the throttle window', async () => {
	let allowed = false;
	const audio = fakeAudio();
	const sound = createNotificationSound({ createContext: audio.create, now: () => 0, canStart: () => allowed });
	assert.equal(await sound.play('chime', 'assigned'), false);
	allowed = true;
	assert.equal(await sound.play('chime', 'assigned'), true);
});

test('a context that resumes late does not play a stale sound', async () => {
	let now = 0;
	let release;
	const audio = fakeAudio({ state: 'suspended', resume: () => new Promise((resolve) => (release = resolve)) });
	const sound = createNotificationSound({ createContext: audio.create, now: () => now });
	const pending = sound.play('chime', 'assigned');
	now = 60_000;
	release();
	assert.equal(await pending, false);
	assert.equal(audio.oscillators.length, 0);
});

// ---- notification click (service worker) ----------------------------------

const origin = 'https://kuayle.example';
const client = (path) => ({ url: origin + path });

test('only same-origin notification targets are accepted', () => {
	assert.equal(notificationTargetUrl('/acme/issue/KUA-7', origin), `${origin}/acme/issue/KUA-7`);
	assert.equal(notificationTargetUrl(`${origin}/acme/inbox?x=1`, origin), `${origin}/acme/inbox?x=1`);
	assert.equal(notificationTargetUrl('https://evil.example/acme/inbox', origin), null);
	assert.equal(notificationTargetUrl('//evil.example/acme', origin), null);
	assert.equal(notificationTargetUrl('javascript:alert(1)', origin), null);
	assert.equal(notificationTargetUrl(undefined, origin), null);
	assert.equal(notificationTargetUrl(42, origin), null);
});

test('focuses a window already on the target, else navigates a same-origin one, else opens a new one', () => {
	const target = `${origin}/acme/issue/KUA-7`;
	const onTarget = client('/acme/issue/KUA-7');
	const elsewhere = client('/acme/inbox');
	const foreign = { url: 'https://other.example/acme/issue/KUA-7' };
	assert.deepEqual(chooseWindowClient([elsewhere, onTarget], target, origin), { client: onTarget, navigate: false });
	assert.deepEqual(chooseWindowClient([foreign, elsewhere], target, origin), { client: elsewhere, navigate: true });
	assert.equal(chooseWindowClient([foreign], target, origin), null);
	assert.equal(chooseWindowClient([], target, origin), null);
	// A focused window is preferred when several could be navigated.
	const focused = { ...client('/acme/my-issues'), focused: true };
	assert.deepEqual(chooseWindowClient([elsewhere, focused], target, origin), { client: focused, navigate: true });
});
