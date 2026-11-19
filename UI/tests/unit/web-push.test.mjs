import assert from 'node:assert/strict';
import test from 'node:test';
import { subscribeToPush } from '../../src/lib/features/notifications/push.ts';
import { showPushNotification, renewPushSubscription } from '../../src/lib/features/pwa/push-events.ts';
import { notificationTargetUrl } from '../../src/lib/features/pwa/notification-click.ts';

const subscription = {
	endpoint: 'https://push.example/sub',
	toJSON: () => ({ keys: { p256dh: 'browser-key', auth: 'secret' } })
};

test('subscribes with the VAPID key and serializes the browser subscription', async () => {
	const bodies = [];
	const manager = {
		getSubscription: async () => null,
		subscribe: async (options) => {
			assert.deepEqual(options, { userVisibleOnly: true, applicationServerKey: new Uint8Array([1, 2, 255]) });
			return subscription;
		}
	};
	const enroll = async (body) => {
		bodies.push(body);
		return { id: 'device' };
	};
	assert.equal(await subscribeToPush(manager, 'AQL_', 'test', enroll), 'device');
	manager.getSubscription = async () => subscription;
	manager.subscribe = () => assert.fail('must reuse the existing subscription');
	assert.equal(await subscribeToPush(manager, 'AQL_', 'test', enroll), 'device');
	assert.deepEqual(bodies, Array(2).fill({
		workspace_slug: 'test', endpoint: subscription.endpoint, p256dh: 'browser-key', auth: 'secret'
	}));
});

test('a push displays a system notification using the existing same-origin click target', async () => {
	const calls = [];
	const registration = { showNotification: async (...args) => { calls.push(args); } };
	const origin = 'https://kuayle.example';
	const url = '/test/assistant?conversation=123';
	await showPushNotification(registration, origin, {
		json: () => ({ title: 'Approval needed', body: 'Deploy', tag: 'approval_requested:123', data: { url } })
	});
	assert.deepEqual(calls, [['Approval needed', { body: 'Deploy', tag: 'approval_requested:123', renotify: false, data: { url }, icon: '/icons/icon-192.png' }]]);
	assert.equal(notificationTargetUrl(calls[0][1].data.url, origin), origin + url);
	await showPushNotification(registration, origin, { json: () => ({ data: { url: 'https://outside.example' } }) });
	assert.equal(calls[1][0], 'Kuayle');
	assert.equal(calls[1][1].data, undefined);
	await showPushNotification(registration, origin, { json: () => { throw new SyntaxError(); } });
	assert.equal(calls[2][0], 'Kuayle');
});

test('renews with the stored key and marks the device for re-enrollment on failure', async () => {
	let current = { id: 'old', userId: 'user', workspaceSlug: 'test', publicKey: 'AQL_' };
	let unsubscribed = false;
	const newSub = { ...subscription, unsubscribe: async () => { unsubscribed = true; return true; } };
	const manager = { subscribe: async ({ applicationServerKey }) => {
		assert.deepEqual(applicationServerKey, new Uint8Array([1, 2, 255]));
		return newSub;
	} };
	const storage = { read: async () => current, replace: async (_id, state) => { current = state; return true; } };
	const revoke = async (id) => assert.equal(id, 'new');
	await renewPushSubscription(manager, storage, async (body) => {
		assert.equal(body.replace_id, 'old');
		return { id: 'new' };
	}, revoke);
	assert.equal(current.id, 'new');
	await renewPushSubscription(manager, storage, async () => { throw new Error('offline'); }, revoke);
	assert.equal(unsubscribed, true);
	assert.equal(current.id, 'new');
	assert.equal(current.needsReenable, true);
});

test('drops the previous browser subscription if renewal cannot create its replacement', async () => {
	let device = { id: 'device', userId: 'user', workspaceSlug: 'test', publicKey: 'AQL_' };
	let unsubscribed = false;
	const oldSubscription = { unsubscribe: async () => { unsubscribed = true; return true; } };
	await renewPushSubscription(
		{ subscribe: async () => { throw new Error('denied'); } },
		{ read: async () => device, replace: async (_id, value) => { device = value; return true; } },
		() => assert.fail('must have a subscription before enrolling'),
		async (id) => assert.equal(id, 'device'),
		{ oldSubscription }
	);
	assert.equal(device.needsReenable, true);
	assert.equal(unsubscribed, true);
});

test('a renewal racing with revocation revokes its replacement', async () => {
	const device = { id: 'old', userId: 'user', workspaceSlug: 'test', publicKey: 'AQL_' };
	const storage = { read: async () => device, replace: async () => false };
	let unsubscribed = false;
	const replacement = { ...subscription, unsubscribe: async () => { unsubscribed = true; return true; } };
	const revoked = [];
	await renewPushSubscription(
		{ subscribe: async () => replacement },
		storage,
		async () => ({ id: 'new' }),
		async (id) => { revoked.push(id); }
	);
	assert.deepEqual(revoked, ['new']);
	assert.equal(unsubscribed, true);
});
