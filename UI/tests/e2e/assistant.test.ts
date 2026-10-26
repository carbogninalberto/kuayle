import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
const conversation = {
	id: 'chat-1',
	title: 'A persistent conversation',
	workspace_id: workspace.id,
	creator_id: 'user-1',
	created_at: '2026-10-26T10:00:00Z',
	updated_at: '2026-10-26T10:00:00Z'
};
const userMessage = {
	id: 'message-1',
	conversation_id: 'chat-1',
	sequence: 1,
	role: 'user',
	content: 'Hello assistant',
	status: 'completed',
	provider: '',
	model: '',
	created_at: conversation.created_at
};
const reply = {
	...userMessage,
	id: 'message-2',
	sequence: 2,
	role: 'assistant',
	content: '',
	status: 'streaming',
	provider: 'openai_compatible',
	model: 'configured-model'
};

async function fixture(page: Page) {
	const state = {
		messages: [] as (typeof userMessage)[],
		cursor: 0,
		events: [] as Array<{ cursor: number; [key: string]: unknown }>,
		sends: [] as string[],
		sockets: [] as WebSocketRoute[],
		denied: false,
		failAfterSend: false,
		failNextHistory: false,
		sendDelay: null as Promise<void> | null,
		replayDelay: null as Promise<void> | null,
		replayRequested: false,
		olderDelay: null as Promise<void> | null,
		olderRequested: false
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', (socket) => {
		state.sockets.push(socket);
	});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test' }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, provider: 'openai_compatible', model: 'configured-model' } });
		if (path.endsWith('/conversations'))
			return route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation] });
		if (path.includes('/conversations/chat-1')) {
			if (state.denied) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (path.endsWith('/messages')) {
				state.sends.push(route.request().postDataJSON().content);
				if (state.sendDelay) await state.sendDelay;
				if (state.failAfterSend) state.failNextHistory = true;
				state.messages = [userMessage, reply];
				return route.fulfill({ status: 202, json: reply });
			}
			if (path.endsWith('/events')) {
				state.replayRequested = true;
				if (state.replayDelay) await state.replayDelay;
				return route.fulfill({
					json: state.events.filter((event) => event.cursor > Number(url.searchParams.get('after') ?? 0))
				});
			}
			if (path.endsWith('/cancel')) {
				state.messages = state.messages.map((m) => (m.status === 'streaming' ? { ...m, status: 'cancelled' } : m));
				return route.fulfill({ status: 204 });
			}
			if (state.failNextHistory) {
				state.failNextHistory = false;
				return route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } });
			}
			const before = Number(url.searchParams.get('before') ?? 0);
			if (before && state.olderDelay) {
				state.olderRequested = true;
				await state.olderDelay;
			}
			const messages = state.messages.filter((m) => !before || m.sequence < before);
			return route.fulfill({
				json: { conversation, messages: messages.slice(-50), cursor: state.cursor, has_more: messages.length > 50 }
			});
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

test('streams real events, preserves history and deduplicates reconnect replay', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Hello assistant');
	await composer.press('Enter');
	await expect.poll(() => state.sends).toEqual(['Hello assistant']);
	await expect.poll(() => state.sockets.length).toBeGreaterThanOrEqual(2);
	const event = {
		id: 'event-3',
		cursor: 3,
		conversation_id: 'chat-1',
		type: 'message.delta',
		payload: { message_id: 'message-2', delta: 'Live partial answer' }
	};
	state.sockets.at(-1)!.send(JSON.stringify(event));
	await expect(page.getByText('Live partial answer', { exact: true })).toBeVisible();
	state.sockets.at(-1)!.send(JSON.stringify(event));
	await expect(page.getByText('Live partial answer', { exact: true })).toHaveCount(1);
	const socketCount = state.sockets.length;
	state.events = [
		{ ...event, id: 'event-4', cursor: 4, payload: { message_id: 'message-2', delta: ' after reconnect' } }
	];
	state.sockets.at(-1)!.close({ code: 1012, reason: 'Restart' });
	await expect.poll(() => state.sockets.length).toBe(socketCount + 1);
	await expect(page.getByText('Live partial answer after reconnect', { exact: true })).toBeVisible();
	expect(state.sockets.at(-1)!.url()).toContain('after=4');
	state.messages = [userMessage, { ...reply, content: 'Live partial answer after reconnect', status: 'completed' }];
	state.cursor = 5;
	state.sockets.at(-1)!.send(
		JSON.stringify({
			...event,
			id: 'event-5',
			cursor: 5,
			type: 'message.completed',
			payload: { message_id: 'message-2', status: 'completed' }
		})
	);
	await page.reload();
	await expect(page.getByText('Live partial answer after reconnect', { exact: true })).toBeVisible();
	expect(state.sends).toHaveLength(1);
});

test('keyboard newline, local drafts and mobile layout', async ({ page }, testInfo) => {
	await page.setViewportSize({ width: 375, height: 812 });
	const state = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('First line');
	await composer.press('Shift+Enter');
	await composer.type('Second line');
	expect(state.sends).toHaveLength(0);
	await page.reload();
	await expect(composer).toHaveValue('First line\nSecond line');
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('mobile.png') });
});

test('clears conversation content after access is revoked', async ({ page }) => {
	const state = await fixture(page);
	state.messages = [userMessage, { ...reply, content: 'Private response', status: 'completed' }];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Private response', { exact: true })).toBeVisible();
	state.denied = true;
	state.sockets[0].close({ code: 1008, reason: 'Access revoked' });
	await expect(page.getByText('Private response', { exact: true })).toHaveCount(0);
	await expect(page.getByText('This conversation is no longer available.')).toBeVisible();
});

test('loads long history without moving the reader and follows only at the live edge', async ({ page }, testInfo) => {
	const state = await fixture(page);
	state.messages = Array.from({ length: 130 }, (_, i) => ({
		...userMessage,
		id: `history-${i}`,
		sequence: i + 1,
		role: i % 2 ? 'assistant' : 'user',
		content: `History message ${i}. ` + 'Detailed content. '.repeat(30)
	}));
	await page.goto('/test/assistant?conversation=chat-1');
	const viewport = page.locator('[data-slot="message-scroller-viewport"]');
	await expect(page.locator('[data-slot="message"]')).toHaveCount(50);
	await viewport.hover();
	await page.mouse.wheel(0, -100000);
	await expect.poll(() => viewport.evaluate((el) => el.scrollTop)).toBe(0);
	await page.getByRole('button', { name: 'Load older messages' }).click();
	await expect(page.locator('[data-slot="message"]')).toHaveCount(100);
	await expect.poll(() => viewport.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
	const position = await viewport.evaluate((el) => el.scrollTop);
	const event = {
		id: 'new',
		cursor: 200,
		conversation_id: 'chat-1',
		type: 'message.created',
		payload: { ...reply, id: 'latest', sequence: 131, content: 'New live activity' }
	};
	state.sockets.at(-1)!.send(JSON.stringify(event));
	await expect(page.locator('[data-slot="message"]')).toHaveCount(101);
	expect(Math.abs((await viewport.evaluate((el) => el.scrollTop)) - position)).toBeLessThan(40);
	await page.getByRole('button', { name: 'Scroll to latest' }).click();
	await expect(page.getByText('New live activity', { exact: true })).toBeInViewport();
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop.png') });
	await page.evaluate(() => document.documentElement.classList.add('dark'));
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop-dark.png') });
});

test('does not fetch provider-authored image URLs or execute agent HTML', async ({ page }) => {
	const state = await fixture(page);
	const fetched: string[] = [];
	await page.route('https://untrusted.example/**', (route) => {
		fetched.push(route.request().url());
		return route.fulfill({ status: 204 });
	});
	state.messages = [
		userMessage,
		{
			...reply,
			status: 'completed',
			content:
				'Safe response\n\n![preview](https://untrusted.example/leak?conversation=private)\n\n<script>window.agentExecuted=true</script>\n\n[Review changes](https://github.com/example/project/pull/1)'
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Safe response', { exact: true })).toBeVisible();
	await expect(page.locator('.assistant-markdown img')).toHaveCount(0);
	expect(fetched).toEqual([]);
	expect(await page.evaluate(() => Boolean((window as unknown as { agentExecuted?: boolean }).agentExecuted))).toBe(
		false
	);
	await expect(page.getByRole('link', { name: 'Review changes' })).toHaveAttribute(
		'href',
		'https://github.com/example/project/pull/1'
	);
});

test('keeps accepted sends visible while a history reload fails', async ({ page }) => {
	const state = await fixture(page);
	state.failAfterSend = true;
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Hello assistant');
	await composer.press('Enter');
	await expect(page.getByRole('alert').filter({ hasText: 'Something went wrong' })).toBeVisible();
	await expect(page.getByText('Responding…', { exact: true })).toBeVisible();
	await composer.fill('Another thought');
	await expect(page.getByRole('button', { name: 'Send message' })).toHaveCount(0);
	await expect(page.getByText('Hello assistant', { exact: true })).toBeVisible({ timeout: 8000 });
	expect(state.sends).toHaveLength(1);
});

test('preserves a new draft typed while a send is pending', async ({ page }) => {
	const state = await fixture(page);
	let release!: () => void;
	state.sendDelay = new Promise<void>((resolve) => (release = resolve));
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Hello assistant');
	await composer.press('Enter');
	await expect.poll(() => state.sends.length).toBe(1);
	await composer.fill('My next thought');
	release();
	await expect(page.getByText('Hello assistant', { exact: true })).toBeVisible();
	await expect(composer).toHaveValue('My next thought');
});

test('ignores a delayed reconnect after a fresh conversation load', async ({ page }) => {
	const state = await fixture(page);
	state.messages = [userMessage, reply];
	let release!: () => void;
	state.replayDelay = new Promise<void>((resolve) => (release = resolve));
	await page.goto('/test/assistant?conversation=chat-1');
	await expect.poll(() => state.sockets.length).toBe(1);
	state.sockets[0].close({ code: 1012, reason: 'Restart' });
	await expect.poll(() => state.replayRequested).toBe(true);
	await page.getByRole('button', { name: 'Stop response' }).click();
	await expect.poll(() => state.sockets.length).toBe(2);
	release();
	// Observe the stale network continuation; it must not open a third socket.
	await page.waitForTimeout(300);
	expect(state.sockets).toHaveLength(2);
});

test('does not restore delayed older history after access is revoked', async ({ page }) => {
	const state = await fixture(page);
	state.messages = Array.from({ length: 60 }, (_, i) => ({
		...userMessage,
		id: `history-${i}`,
		sequence: i + 1,
		content: `Private message ${i}`
	}));
	let release!: () => void;
	state.olderDelay = new Promise<void>((resolve) => (release = resolve));
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.locator('[data-slot="message"]')).toHaveCount(50);
	await page.getByRole('button', { name: 'Load older messages' }).click();
	await expect.poll(() => state.olderRequested).toBe(true);
	state.denied = true;
	state.sockets.at(-1)!.close({ code: 1008, reason: 'Revoked' });
	await expect(page.getByText('This conversation is no longer available.')).toBeVisible();
	release();
	await page.waitForTimeout(300);
	await expect(page.locator('[data-slot="message"]')).toHaveCount(0);
});
