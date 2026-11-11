import { expect, test, type Page } from '@playwright/test';

const workflowSortOrder = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];
const eventKeys = ['approval_requested', 'task_finished', 'review_needed', 'assigned', 'mentioned', 'comment', 'issue_updated'];

type Preferences = Record<string, unknown> & { notification_events: Record<string, boolean> };

function serverPreferences(): Preferences {
	return {
		font_size: 'default',
		pointer_cursors: true,
		theme_mode: 'dark',
		light_theme: 'light',
		dark_theme: 'dark',
		workflow_sort_mode: 'default',
		workflow_sort_order: workflowSortOrder,
		team_workflow_sort_overrides: {},
		recent_due_dates: [],
		issues_group_by: 'status',
		browser_notifications: true,
		notification_sound: 'chime',
		notification_events: Object.fromEntries(eventKeys.map((key) => [key, true]))
	};
}

// Stubs the two browser APIs the section drives, recording what the page does with them.
async function stubBrowserApis(page: Page, permission: 'default' | 'denied' | null) {
	await page.addInitScript((initial) => {
		const calls = { permissionRequests: 0, oscillators: 0, contexts: 0 };
		Object.defineProperty(window, '__notifyCalls', { value: calls });
		if (initial === null) {
			// A browser without the Notification API.
			Reflect.deleteProperty(window, 'Notification');
		} else {
			class FakeNotification {
				static permission: string = initial ?? 'default';
				static async requestPermission() {
					calls.permissionRequests++;
					FakeNotification.permission = 'granted';
					return 'granted';
				}
			}
			Object.defineProperty(window, 'Notification', { value: FakeNotification, configurable: true, writable: true });
		}
		const param = () => ({
			value: 0,
			setValueAtTime() {},
			linearRampToValueAtTime() {},
			exponentialRampToValueAtTime() {}
		});
		class FakeAudioContext {
			state = 'running';
			currentTime = 0;
			destination = {};
			constructor() {
				calls.contexts++;
			}
			resume() {
				return Promise.resolve();
			}
			createOscillator() {
				calls.oscillators++;
				return { type: 'sine', frequency: param(), connect() {}, start() {}, stop() {} };
			}
			createGain() {
				return { gain: param(), connect() {} };
			}
		}
		Object.defineProperty(window, 'AudioContext', { value: FakeAudioContext, configurable: true, writable: true });
	}, permission);
}

// Mocks the API with a preferences store that merges PATCHes the way the server does.
async function mockApi(page: Page) {
	const state = { preferences: serverPreferences(), patches: [] as Record<string, unknown>[] };
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: { id: 'user-1', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null }
			});
		}
		if (path === '/api/preferences' && request.method() === 'GET') return route.fulfill({ json: state.preferences });
		if (path === '/api/preferences' && request.method() === 'PATCH') {
			const patch = request.postDataJSON() as Record<string, unknown>;
			state.patches.push(patch);
			state.preferences = {
				...state.preferences,
				...patch,
				notification_events: {
					...state.preferences.notification_events,
					...((patch.notification_events as Record<string, boolean> | undefined) ?? {})
				}
			};
			return route.fulfill({ json: state.preferences });
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: {
					id: 'workspace-1',
					name: 'Test Workspace',
					slug: 'test',
					logo_url: null,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			});
		}
		if (
			path === '/api/workspaces/test/teams' ||
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === '/api/workspaces/test/views'
		) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({
			status: 404,
			json: { error: { code: 'UNHANDLED_TEST_ROUTE', message: `${request.method()} ${path}` } }
		});
	});
	return state;
}

const calls = (page: Page) =>
	page.evaluate(
		() => (window as unknown as { __notifyCalls: { permissionRequests: number; oscillators: number; contexts: number } }).__notifyCalls
	);

test('notification preferences ask for permission, preview sounds and persist', async ({ page }) => {
	await stubBrowserApis(page, 'default');
	const api = await mockApi(page);
	await page.goto('/test/settings/preferences');

	const section = page.getByTestId('notification-preferences');
	const permission = section.getByTestId('notification-permission');
	await expect(permission).toHaveAttribute('data-permission', 'default');

	// Loading the page neither asks for permission nor touches audio.
	expect(await calls(page)).toEqual({ permissionRequests: 0, oscillators: 0, contexts: 0 });

	// Permission is requested only from the button.
	await section.getByRole('button', { name: 'Enable' }).click();
	await expect(permission).toHaveAttribute('data-permission', 'granted');
	await expect(permission).toHaveText('Browser notifications are allowed.');
	await expect(section.getByRole('button', { name: 'Enable' })).toHaveCount(0);
	expect((await calls(page)).permissionRequests).toBe(1);

	// The preview button plays the selected preset through Web Audio.
	await section.getByRole('button', { name: 'Preview sound' }).click();
	await expect.poll(async () => (await calls(page)).oscillators).toBe(2);

	// Choosing a preset saves it and previews it.
	const soundSelect = section.getByRole('combobox', { name: 'Notification sound' });
	await soundSelect.click();
	await expect(page.getByRole('option')).toHaveText(['Off', 'Chime', 'Bell', 'Pop', 'Drop', 'Pulse']);
	await page.getByRole('option', { name: 'Bell' }).click();
	await expect(soundSelect).toContainText('Bell');
	await expect.poll(async () => (await calls(page)).oscillators).toBe(5);
	await expect.poll(() => api.preferences.notification_sound).toBe('bell');
	expect((await calls(page)).contexts).toBe(1);

	// Toggles are saved through PATCH /api/preferences.
	await section.getByRole('switch', { name: 'Browser notifications' }).click();
	await expect.poll(() => api.preferences.browser_notifications).toBe(false);
	await section.getByRole('switch', { name: 'New comments' }).click();
	await expect.poll(() => api.preferences.notification_events.comment).toBe(false);
	expect(api.preferences.notification_events.mentioned).toBe(true);
	expect(api.patches.length).toBeGreaterThanOrEqual(3);

	// Everything survives a reload, served from the stored preferences.
	await expect
		.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('kuayle-preferences') ?? '{}').localDirty))
		.toBe(false);
	await page.reload();
	const reloaded = page.getByTestId('notification-preferences');
	await expect(reloaded.getByRole('combobox', { name: 'Notification sound' })).toContainText('Bell');
	await expect(reloaded.getByRole('switch', { name: 'Browser notifications' })).toHaveAttribute('aria-checked', 'false');
	await expect(reloaded.getByRole('switch', { name: 'New comments' })).toHaveAttribute('aria-checked', 'false');
	await expect(reloaded.getByRole('switch', { name: 'You are mentioned' })).toHaveAttribute('aria-checked', 'true');
	await expect(reloaded.getByRole('switch')).toHaveCount(8);
});

test('sound off disables the preview and plays nothing', async ({ page }) => {
	await stubBrowserApis(page, 'default');
	const api = await mockApi(page);
	api.preferences.notification_sound = 'off';
	await page.goto('/test/settings/preferences');

	const section = page.getByTestId('notification-preferences');
	await expect(section.getByRole('combobox', { name: 'Notification sound' })).toContainText('Off');
	await expect(section.getByRole('button', { name: 'Preview sound' })).toBeDisabled();
	expect((await calls(page)).oscillators).toBe(0);
});

for (const [name, permission, text] of [
	['denied', 'denied', /blocked.*site settings/],
	['unsupported', null, 'This browser does not support notifications.']
] as const) {
	test(`${name} browser permission is explained and the rest keeps working`, async ({ page }) => {
		const errors: string[] = [];
		page.on('pageerror', (error) => errors.push(error.message));
		await stubBrowserApis(page, permission);
		const api = await mockApi(page);
		await page.goto('/test/settings/preferences');

		const section = page.getByTestId('notification-preferences');
		await expect(section.getByTestId('notification-permission')).toHaveAttribute('data-permission', name);
		await expect(section.getByTestId('notification-permission')).toHaveText(text);
		await expect(section.getByRole('button', { name: 'Enable' })).toHaveCount(0);

		// Sound and event settings do not depend on the permission.
		await section.getByRole('switch', { name: 'An issue is assigned to you' }).click();
		await expect.poll(() => api.preferences.notification_events.assigned).toBe(false);
		await section.getByRole('button', { name: 'Preview sound' }).click();
		await expect.poll(async () => (await calls(page)).oscillators).toBe(2);
		expect((await calls(page)).permissionRequests).toBe(0);
		expect(errors).toEqual([]);
	});
}
