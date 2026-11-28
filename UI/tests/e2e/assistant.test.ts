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
		tools: [] as any[],
		approvals: [] as any[],
		queue: [] as any[],
		decisions: [] as string[],
		decisionStatus: 200,
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
		olderRequested: false,
		// Append each send to the existing transcript instead of replacing it.
		appendSends: false,
		historyDelay: null as Promise<void> | null
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
		if (path === '/api/workspaces/test/agent-tasks') return route.fulfill({ json: state.queue });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, per_page: 50, has_more: false } });
		if (path.endsWith('/task-options')) return route.fulfill({ json: { providers: [], model_servers: [] } });
		if (path === '/api/workspaces/test/agent-model-catalog')
			return route.fulfill({
				json: { harness: url.searchParams.get('harness') ?? '', source: '', stale: false, unavailable: true, models: [] }
			});
		if (path === '/api/workspaces/test/agent-tasks/task-9')
			return route.fulfill({
				json: {
					id: 'task-9',
					conversation_id: 'chat-issue',
					state: 'running',
					reason: 'Native coding harness started',
					provider: 'codex',
					model: 'deepseek-flash',
					generation: 1
				}
			});
		if (path.endsWith('/conversations'))
			return route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation] });
		if (path.includes('/conversations/chat-1')) {
			if (state.denied) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (path.endsWith('/messages')) {
				state.sends.push(route.request().postDataJSON().content);
				if (state.sendDelay) await state.sendDelay;
				if (state.failAfterSend) state.failNextHistory = true;
				if (state.appendSends) {
					const sequence = state.messages.length + 1;
					const sent = {
						...userMessage,
						id: `sent-${sequence}`,
						sequence,
						content: route.request().postDataJSON().content
					};
					const live = { ...reply, id: `live-${sequence + 1}`, sequence: sequence + 1 };
					state.messages = [...state.messages, sent, live];
					return route.fulfill({ status: 202, json: live });
				}
				state.messages = [userMessage, reply];
				return route.fulfill({ status: 202, json: reply });
			}
			if (path.includes('/approvals/')) {
				const decision = route.request().postDataJSON().decision;
				state.decisions.push(decision);
				if (state.decisionStatus !== 200)
					return route.fulfill({ status: state.decisionStatus, json: { error: { code: 'INTERNAL_ERROR' } } });
				const resolved = {
					...state.approvals.find(a => path.endsWith('/' + a.id)),
					status: decision === 'approve' ? 'approved' : 'denied',
					task_id: decision === 'approve' ? 'task-9' : undefined,
					resolved_at: '2026-10-26T10:05:00Z',
					updated_at: '2026-10-26T10:05:00Z'
				};
				state.approvals = state.approvals.map(a => a.id === resolved.id ? resolved : a);
				return route.fulfill({ json: resolved });
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
			if (state.historyDelay) await state.historyDelay;
			const before = Number(url.searchParams.get('before') ?? 0);
			if (before && state.olderDelay) {
				state.olderRequested = true;
				await state.olderDelay;
			}
			const messages = state.messages.filter((m) => !before || m.sequence < before);
			return route.fulfill({
				json: {
					conversation,
					messages: messages.slice(-50),
					tools: state.tools,
					approvals: state.approvals,
					cursor: state.cursor,
					has_more: messages.length > 50
				}
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

test('a desktop scrollbar does not shorten the chat panel', async ({ page }) => {
	await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const shell = page.getByTestId('assistant-shell');
	await expect(shell).toBeVisible();
	const before = (await shell.boundingBox())!;
	await page.evaluate(() => {
		Object.defineProperty(window.visualViewport!, 'height', { configurable: true, value: innerHeight - 15 });
		window.visualViewport!.dispatchEvent(new Event('resize'));
	});
	expect((await shell.boundingBox())!.height).toBe(before.height);
});

test('mobile chat controls and approvals fit above the visual keyboard viewport', async ({ browser }, testInfo) => {
	const context = await browser.newContext({ hasTouch: true, isMobile: true, viewport: { width: 360, height: 800 }, reducedMotion: 'reduce' });
	const page = await context.newPage();
	await pendingDelegation(page, { title: 'Review the delivery report', instructions: 'Check the totals.' });
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByTestId('assistant-composer');
	const message = composer.getByRole('textbox', { name: 'Message' });
	await message.fill('Ready to review');
	await expect(composer.getByTestId('approval-card')).toBeVisible();
	const panel = page.getByTestId('panel-toggle');
	for (const control of [panel, ...await composer.getByRole('button').all()]) {
		const box = (await control.boundingBox())!;
		expect(Math.min(box.width, box.height), await control.getAttribute('aria-label') ?? '').toBeGreaterThanOrEqual(44);
	}
	await panel.click();
	await expect(page.getByRole('dialog')).toHaveCSS('animation-name', 'none');
	await page.keyboard.press('Escape');
	await message.focus();
	// Mobile browsers can resize only the visual viewport when the keyboard opens.
	await page.evaluate(() => {
		Object.defineProperty(window.visualViewport!, 'height', { configurable: true, value: 360 });
		window.visualViewport!.dispatchEvent(new Event('resize'));
	});
	const send = composer.getByRole('button', { name: 'Send message' });
	await expect.poll(() => send.evaluate(el => el.getBoundingClientRect().bottom)).toBeLessThanOrEqual(360);
	await expect(message).toBeFocused();
	await expect(message).toHaveValue('Ready to review');
	await page.evaluate(() => {
		Reflect.deleteProperty(window.visualViewport!, 'height');
		window.visualViewport!.dispatchEvent(new Event('resize'));
	});
	await expect.poll(() => send.evaluate(el => el.getBoundingClientRect().top)).toBeGreaterThan(360);
	await page.screenshot({ path: testInfo.outputPath('mobile-chat.png'), animations: 'disabled' });
	await context.close();
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

function longTranscript(length: number) {
	return Array.from({ length }, (_, i) => ({
		...userMessage,
		id: `history-${i}`,
		sequence: i + 1,
		role: i % 2 ? 'assistant' : 'user',
		content: `History message ${i}. ` + 'Detailed content. '.repeat(30)
	}));
}

// Streams `count` deltas into the live reply, each long enough to grow the transcript.
async function streamDeltas(
	page: Page,
	state: Awaited<ReturnType<typeof fixture>>,
	messageId: string,
	from: number,
	count: number
) {
	for (let i = from; i < from + count; i++) {
		state.sockets.at(-1)!.send(
			JSON.stringify({
				id: `delta-${i}`,
				cursor: 1000 + i,
				conversation_id: 'chat-1',
				type: 'message.delta',
				payload: { message_id: messageId, delta: `\n\nStreamed paragraph ${i}. ` + 'More streamed text. '.repeat(20) }
			})
		);
		await expect(page.getByText(`Streamed paragraph ${i}.`, { exact: false })).toHaveCount(1);
	}
}

function atLiveEdge(viewport: ReturnType<Page['locator']>) {
	return viewport.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight <= 16);
}

for (const size of [
	{ name: 'desktop', width: 1280, height: 800 },
	{ name: 'phone', width: 390, height: 844 }
]) {
	test(`pressing Enter shows the sent message and follows the streaming reply (${size.name})`, async ({
		page
	}, testInfo) => {
		await page.setViewportSize({ width: size.width, height: size.height });
		const state = await fixture(page);
		state.appendSends = true;
		state.messages = longTranscript(30);
		await page.goto('/test/assistant?conversation=chat-1');
		const viewport = page.locator('[data-slot="message-scroller-viewport"]');
		await expect(page.locator('[data-slot="message"]')).toHaveCount(30);
		// The reader glanced at earlier history; sending is still a request to see the reply.
		await viewport.hover();
		await page.mouse.wheel(0, -600);
		await expect.poll(() => atLiveEdge(viewport)).toBe(false);
		const composer = page.getByRole('textbox', { name: 'Message' });
		await composer.fill('What changed in the latest release?');
		await composer.press('Enter');
		await expect.poll(() => state.sends).toEqual(['What changed in the latest release?']);
		await expect(page.getByText('What changed in the latest release?', { exact: true })).toBeInViewport();
		await expect.poll(() => atLiveEdge(viewport)).toBe(true);
		await expect.poll(() => state.sockets.length).toBeGreaterThanOrEqual(2);
		await streamDeltas(page, state, 'live-32', 0, 6);
		await expect(page.getByText('Streamed paragraph 5.', { exact: false })).toBeInViewport();
		await expect.poll(() => atLiveEdge(viewport)).toBe(true);
		await expect(page.getByRole('button', { name: 'Scroll to latest' })).toHaveAttribute('data-active', 'false');
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
		await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`send-follow-${size.name}.png`) });
	});
}

test('a slow history reload after sending keeps the transcript and the sent message on screen', async ({ page }) => {
	const state = await fixture(page);
	state.appendSends = true;
	state.messages = longTranscript(30);
	await page.goto('/test/assistant?conversation=chat-1');
	const viewport = page.locator('[data-slot="message-scroller-viewport"]');
	await expect(page.locator('[data-slot="message"]')).toHaveCount(30);
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
	let releaseReload!: () => void;
	const reload = new Promise<void>((resolve) => (releaseReload = resolve));
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Slow question');
	state.historyDelay = reload;
	await composer.press('Enter');
	await expect.poll(() => state.sends).toEqual(['Slow question']);
	// While the reload is pending the transcript stays rendered and the accepted reply is followed.
	await expect(page.locator('[data-slot="message"]')).toHaveCount(31);
	await expect(page.locator('[data-slot="message"]').last()).toBeInViewport();
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
	await expect(page.getByRole('status').filter({ hasText: 'Loading' })).toHaveCount(0);
	state.historyDelay = null;
	releaseReload();
	await expect(page.locator('[data-slot="message"]')).toHaveCount(32);
	await expect(page.getByText('Slow question', { exact: true })).toBeInViewport();
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
});

test('a reader who scrolls up while a reply streams stays put and can jump to the latest', async ({
	page
}, testInfo) => {
	const state = await fixture(page);
	state.appendSends = true;
	state.messages = longTranscript(30);
	await page.goto('/test/assistant?conversation=chat-1');
	const viewport = page.locator('[data-slot="message-scroller-viewport"]');
	await expect(page.locator('[data-slot="message"]')).toHaveCount(30);
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Summarise the thread');
	await composer.press('Enter');
	await expect.poll(() => state.sockets.length).toBeGreaterThanOrEqual(2);
	await streamDeltas(page, state, 'live-32', 0, 2);
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
	await viewport.hover();
	await page.mouse.wheel(0, -1500);
	await expect.poll(() => atLiveEdge(viewport)).toBe(false);
	const position = await viewport.evaluate((el) => el.scrollTop);
	await streamDeltas(page, state, 'live-32', 2, 4);
	expect(Math.abs((await viewport.evaluate((el) => el.scrollTop)) - position)).toBeLessThan(40);
	const latest = page.getByRole('button', { name: 'Scroll to latest' });
	await expect(latest).toHaveAttribute('data-active', 'true');
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('scrolled-up-latest.png') });
	await latest.click();
	await expect(page.getByText('Streamed paragraph 5.', { exact: false })).toBeInViewport();
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
	// Back at the live edge, later chunks are followed again.
	await streamDeltas(page, state, 'live-32', 6, 2);
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
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

test('renders Markdown tables in assistant replies with alignment and horizontal scroll', async ({ page }, testInfo) => {
	const state = await fixture(page);
	const wideHeader = Array.from({ length: 12 }, (_, i) => `Header ${i + 1}`);
	const wideRow = Array.from({ length: 12 }, (_, i) => `Wide cell data ${i + 1}`);
	const wideTable = [wideHeader, wideHeader.map(() => '---'), wideRow]
		.map((cells) => `| ${cells.join(' | ')} |`)
		.join('\n');
	state.messages = [
		userMessage,
		{
			...reply,
			status: 'completed',
			content: [
				'Aligned table:',
				'',
				'| Left | Center | Right |',
				'|:--|:-:|--:|',
				'| one | two | three |',
				'',
				'```', 'long_code_line'.repeat(40), '```', '',
				'Wide table:',
				'',
				wideTable,
				'',
				'Malformed table:',
				'',
				'| Name | Value |',
				'|--|--|',
				'| kept | extra-dropped | merged-cell |',
				'',
				'| not | a table',
				'|--',
				'| 1 | 2 |',
				'',
				'<table style="position:fixed" onclick="window.agentExecuted=true"><tr><td>injected</td></tr></table>'
			].join('\n')
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');

	const markdown = page.locator('.assistant-markdown');
	await expect(markdown.locator('table')).toHaveCount(3);

	const aligned = markdown.locator('table').first();
	await expect(aligned.locator('thead th')).toHaveCount(3);
	await expect(aligned.locator('tbody td')).toHaveCount(3);
	await expect(aligned.getByRole('columnheader', { name: 'Center', exact: true })).toBeVisible();
	await expect(aligned.getByRole('cell', { name: 'two', exact: true })).toBeVisible();
	expect(
		await aligned
			.locator('tbody td')
			.evaluateAll((cells) => cells.map((cell) => getComputedStyle(cell).textAlign))
	).toEqual(['left', 'center', 'right']);
	expect(
		await aligned
			.locator('tbody td')
			.first()
			.evaluate((cell) => getComputedStyle(cell).borderTopWidth)
	).not.toBe('0px');

	const wide = markdown.locator('table').nth(1);
	await expect(wide.locator('thead th')).toHaveCount(12);
	await page.setViewportSize({ width: 360, height: 800 });
	for (const block of [wide, markdown.locator('pre')]) {
		expect(await block.evaluate(el => { el.scrollLeft = 32; return el.scrollLeft; })).toBeGreaterThan(0);
	}
	const overflow = await wide.evaluate((table) => ({
		scrollWidth: table.scrollWidth,
		clientWidth: table.clientWidth
	}));
	expect(overflow.scrollWidth).toBeGreaterThan(overflow.clientWidth);
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	const malformed = markdown.locator('table').nth(2);
	await expect(malformed.getByRole('cell', { name: 'kept', exact: true })).toBeVisible();
	await expect(malformed.getByRole('cell', { name: 'extra-dropped', exact: true })).toBeVisible();
	await expect(malformed.getByText('merged-cell', { exact: true })).toHaveCount(0);
	await expect(markdown.locator('p').filter({ hasText: '| not | a table' })).toHaveCount(1);

	// Model-authored HTML stays text; alignment survives only as the inert attribute.
	await expect(markdown.locator('[style]')).toHaveCount(0);
	await expect(
		markdown.getByText(
			'<table style="position:fixed" onclick="window.agentExecuted=true"><tr><td>injected</td></tr></table>',
			{ exact: true }
		)
	).toBeVisible();
	expect(await page.evaluate(() => Boolean((window as unknown as { agentExecuted?: boolean }).agentExecuted))).toBe(
		false
	);
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('assistant-tables.png') });
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
	await page.setViewportSize({ width: 375, height: 812 });
	const state = await fixture(page);
	state.messages = [userMessage, reply];
	let release!: () => void;
	state.replayDelay = new Promise<void>((resolve) => (release = resolve));
	await page.goto('/test/assistant?conversation=chat-1');
	await expect.poll(() => state.sockets.length).toBe(1);
	state.sockets[0].close({ code: 1012, reason: 'Restart' });
	await expect.poll(() => state.replayRequested).toBe(true);
	const stop = page.getByRole('button', { name: 'Stop response' });
	const box = (await stop.boundingBox())!;
	expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(44);
	await stop.click();
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

const chatIssue = {
	id: 'issue-1',
	identifier: 'ENG-1',
	workspace_id: workspace.id,
	team_id: 'team-1',
	title: 'Implement issue chat',
	description: 'Issue context',
	status: 'todo',
	priority: 0,
	number: 1,
	creator_id: 'user-1',
	created_at: conversation.created_at,
	updated_at: conversation.updated_at,
	labels: [],
	assignees: []
};

test('the issue page opens its conversation in the assistant instead of embedding a chat', async ({ page }) => {
	const state = await fixture(page);
	const issueChat = { ...conversation, issue_id: chatIssue.id, team_id: chatIssue.team_id };
	let opens = 0;
	await page.route('**/api/workspaces/test/issues/ENG-1', (route) => route.fulfill({ json: chatIssue }));
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) => {
		if (route.request().method() === 'POST') {
			expect(route.request().postDataJSON()).toEqual({ issue_id: chatIssue.id });
			opens++;
			return route.fulfill({ json: issueChat });
		}
		return route.fulfill({ json: [issueChat] });
	});
	await page.route('**/api/workspaces/test/conversations/chat-1?*', (route) =>
		route.fulfill({
			json: {
				conversation: issueChat,
				issue: {
					id: chatIssue.id,
					identifier: chatIssue.identifier,
					title: chatIssue.title,
					description: chatIssue.description,
					status: chatIssue.status
				},
				messages: state.messages,
				cursor: state.cursor,
				has_more: false
			}
		})
	);
	await page.goto('/test/issue/ENG-1');
	await expect(page.getByText('Issue context')).toBeVisible();
	// The description is followed by the issue's own content: no chat is embedded.
	await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(0);
	await expect(page.getByRole('region', { name: 'Issue chat' })).toHaveCount(0);
	const open = page.getByRole('button', { name: 'Chat with assistant', exact: true });
	await expect(open).toBeVisible();
	// Keyboard: the button is reachable and activates with Enter.
	await open.focus();
	await page.keyboard.press('Enter');
	await expect(page).toHaveURL(/\/test\/assistant\?conversation=chat-1$/);
	expect(opens).toBe(1);
	await expect(page.getByRole('link', { name: 'ENG-1', exact: true })).toHaveAttribute('href', '/test/issue/ENG-1');
	const composer = page.getByRole('textbox', { name: 'Message', exact: true });
	await composer.fill('Explain the implementation');
	await composer.press('Enter');
	await expect.poll(() => state.sends).toEqual(['Explain the implementation']);
	// The issue's conversation is the selected one in the sidebar.
	await expect(
		page.getByRole('navigation', { name: 'Conversations' }).getByRole('link', { name: conversation.title })
	).toHaveAttribute('aria-current', 'page');
});

test('a denied issue conversation reports the failure and stays on the issue', async ({ page }) => {
	await fixture(page);
	await page.route('**/api/workspaces/test/issues/ENG-1', (route) => route.fulfill({ json: chatIssue }));
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) =>
		route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } })
	);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Chat with assistant', exact: true }).click();
	await expect(page.getByText("Could not open this issue's conversation.")).toBeVisible();
	await expect(page).toHaveURL(/\/issue\/ENG-1$/);
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('renders recorded tool calls from history and live events as escaped data', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'I created the issue.' };
	state.messages = [userMessage, answered];
	state.cursor = 4;
	const created = {
		id: 'tool-1',
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence: 0,
		call_id: 'call_1',
		name: 'create_issue',
		input: { team_key: 'ENG', title: 'Dark mode' },
		status: 'completed',
		output: { identifier: 'ENG-7', title: '<img src=x onerror="window.evil=1">' },
		created_at: conversation.created_at
	};
	state.tools = [created];
	await page.goto('/test/assistant?conversation=chat-1');
	const trigger = page.getByRole('button', { name: 'Created ENG-7' });
	await expect(trigger).toBeVisible();
	await expect(page.getByRole('link', { name: 'ENG-7' })).toHaveAttribute('href', '/test/issue/ENG-7');
	await trigger.click();
	await expect(
		page.locator('[data-slot="code-block-code"]').getByText('<img src=x onerror=', { exact: false })
	).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	const search = {
		...created,
		id: 'tool-2',
		sequence: 1,
		call_id: 'call_2',
		name: 'search_issues',
		input: { query: 'dark' },
		status: 'running',
		output: undefined
	};
	state.sockets
		.at(-1)!
		.send(JSON.stringify({ id: 'e5', cursor: 5, conversation_id: 'chat-1', type: 'tool.started', payload: search }));
	await expect(page.getByText('Running Search issues', { exact: true })).toBeVisible();
	const failed = { ...search, status: 'failed', error: 'query must be 1 to 200 characters' };
	state.sockets
		.at(-1)!
		.send(JSON.stringify({ id: 'e6', cursor: 6, conversation_id: 'chat-1', type: 'tool.completed', payload: failed }));
	await expect(page.getByRole('button', { name: 'Failed to run Search issues' })).toBeVisible();
	// A late replay of the start must not hide the recorded failure.
	state.sockets
		.at(-1)!
		.send(JSON.stringify({ id: 'e5', cursor: 5, conversation_id: 'chat-1', type: 'tool.started', payload: search }));
	await expect(page.getByRole('button', { name: 'Failed to run Search issues' })).toBeVisible();
	await expect(page.getByText('I created the issue.', { exact: true })).toBeVisible();
	const updated = {
		...created,
		id: 'tool-3',
		sequence: 2,
		call_id: 'call_3',
		name: 'update_issue',
		input: { identifier: 'ENG-7', priority: 1 },
		output: { identifier: 'ENG-7', title: 'Dark mode' }
	};
	state.sockets
		.at(-1)!
		.send(JSON.stringify({ id: 'e7', cursor: 7, conversation_id: 'chat-1', type: 'tool.completed', payload: updated }));
	await expect(page.getByRole('button', { name: 'Updated ENG-7' })).toBeVisible();
});

test('shows issue metadata, valid options and a saved issue warning', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'ENG-7 was created. Check the cycle name for the update.' };
	state.messages = [
		{ ...userMessage, content: 'Create an issue assigned to me, due Friday, in Sprint X with label Needs QA.' },
		answered
	];
	const created = {
		id: 'tool-1',
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence: 0,
		call_id: 'call_1',
		name: 'create_issue',
		input: { team_key: 'ENG', title: 'Ship the fix', assignees: ['me'], due_date: '2026-11-20', cycle: 'Sprint X', labels: ['Needs QA'] },
		status: 'completed',
		output: { identifier: 'ENG-7', title: 'Ship the fix' },
		created_at: conversation.created_at
	};
	const error = 'cycle value "Sprint Z" did not match. Valid options include "Sprint X" (more options available)';
	const warning = 'Issue ENG-8 was created. The requested labels could not be confirmed. Check the existing issue and use update_issue if a correction is needed.';
	state.tools = [
		created,
		{
			...created,
			id: 'tool-2',
			sequence: 1,
			call_id: 'call_2',
			name: 'update_issue',
			input: { identifier: 'ENG-7', cycle: 'Sprint Z' },
			status: 'failed',
			output: undefined,
			error
		},
		{
			...created,
			id: 'tool-3',
			sequence: 2,
			call_id: 'call_3',
			input: { team_key: 'ENG', title: 'Check the fix', labels: ['Needs QA'] },
			status: 'failed',
			output: undefined,
			error: warning
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Created ENG-7' }).click();
	const toolCode = page.locator('[data-slot="code-block-code"]');
	const createInput = toolCode.filter({ hasText: '"due_date"' });
	for (const value of ['"me"', '"2026-11-20"', '"Sprint X"', '"Needs QA"']) {
		await expect(createInput).toContainText(value);
	}
	await page.getByRole('button', { name: 'Failed to run Update issue' }).click();
	await expect(toolCode.filter({ hasText: error })).toBeVisible();
	await page.getByRole('button', { name: 'Failed to run Create issue' }).click();
	await expect(toolCode.filter({ hasText: warning })).toBeVisible();
});

test('asks for approval before delegating and records the decision from the backend', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Approval is pending.' };
	state.messages = [userMessage, answered];
	const call = {
		id: 'tool-9',
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence: 0,
		call_id: 'call_9',
		name: 'delegate_issue',
		input: { identifier: 'ENG-1' },
		status: 'completed',
		output: { approval_id: 'approval-1', status: 'pending_user_approval', identifier: 'ENG-1' },
		created_at: conversation.created_at
	};
	state.tools = [call];
	state.approvals = [
		{
			id: 'approval-1',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-9',
			action: 'delegate_issue',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				instructions: '<b onmouseover="window.evil=1">Implement</b> dark mode',
				effort: 'high'
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Start a coding agent on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByTestId('approval-details')).toContainText('Implement');
	await expect(page.getByText('Effort: high', { exact: true })).toBeVisible();
	// The decision stays between the reply and the composer.
	const text = await page.getByText('Approval is pending.', { exact: true }).boundingBox();
	const request = await page.getByText('Start a coding agent on ENG-1?', { exact: true }).boundingBox();
	expect(request!.y).toBeGreaterThan(text!.y);
	await page.setViewportSize({ width: 360, height: 800 });
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeInViewport();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	state.decisionStatus = 500;
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByRole('alert').getByText('The decision could not be saved. Try again.')).toBeVisible();
	state.decisionStatus = 200;
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
	const subagent = page.getByRole('button', { name: /Coding agent on ENG-1/ });
	await expect(subagent).toBeVisible();
	await expect(subagent).toContainText('Running');
	await subagent.click();
	await expect(page.getByText('Native coding harness started', { exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Open the issue chat' })).toHaveAttribute(
		'href',
		'/test/assistant?conversation=chat-issue'
	);
	expect(state.decisions).toEqual(['approve', 'approve']);
	await page.reload();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	// A stale pending copy replayed later cannot reopen the decision.
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	state.sockets.at(-1)!.send(
		JSON.stringify({
			id: 'e9',
			cursor: 9,
			conversation_id: 'chat-1',
			type: 'approval.requested',
			payload: { ...state.approvals[0], status: 'pending', updated_at: conversation.created_at }
		})
	);
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
});

test('the approval card runs the model on the model server the user picked', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-9',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_9',
			name: 'delegate_issue',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-1', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-1',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-9',
			action: 'delegate_issue',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: { identifier: 'ENG-1', title: 'Dark mode', instructions: 'Implement dark mode' },
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	];
	// Two gateways list the same model name; only the server tells them apart.
	const server = (id: string, name: string, is_default: boolean, models: string[]) => ({
		id,
		name,
		models,
		model_names: { 'gpt-5': 'GPT 5', o4: 'O4', 'gpt-6': 'GPT 6' },
		default_model: models[0],
		fast_model: '',
		thinking: 'high',
		is_default,
		harnesses: ['codex'],
		thinking_levels: ['off', 'low', 'high', 'max'],
		updated_at: conversation.created_at
	});
	await page.route('**/api/workspaces/test/conversations/chat-1/task-options', (route) =>
		route.fulfill({
			json: {
				providers: [{ id: 'codex', display_name: 'Codex' }],
				default_provider: 'codex',
				model_servers: [
					server('server-a', 'Gateway A', true, ['gpt-5']),
					server('server-b', 'Gateway B', false, ['gpt-5', 'o4'])
				]
			}
		})
	);
	const bodies: unknown[] = [];
	let refreshes = 0;
	await page.route('**/api/workspaces/test/agent-model-servers', (route) => {
		refreshes++;
		if (refreshes === 2) return route.fulfill({ status: 502, json: {} });
		return route.fulfill({
			json: [server('server-a', 'Gateway A', true, ['gpt-5']), server('server-b', 'Gateway B', false, ['gpt-6'])]
		});
	});
	page.on('request', (request) => {
		if (request.method() === 'POST' && request.url().includes('/approvals/')) bodies.push(request.postDataJSON());
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	const serverButton = page.getByRole('button', { name: 'Model server' });
	await expect(serverButton).toHaveText('Gateway A');
	const model = page.getByRole('combobox', { name: 'Model', exact: true });
	await expect(model).toHaveText('GPT 5');
	await serverButton.click();
	await page.getByRole('menuitemradio', { name: 'Gateway B' }).click();
	await expect(serverButton).toHaveText('Gateway B');
	await expect(model).toHaveText('GPT 5');
	await model.click();
	await expect(page.getByRole('option', { name: /O4.*o4/ })).toBeVisible();
	await page.getByRole('button', { name: 'Refresh', exact: true }).click();
	await expect(page.getByRole('option', { name: /O4.*o4/ })).toHaveCount(0);
	const refreshed = page.getByRole('option', { name: /GPT 6.*gpt-6/ });
	await expect(refreshed).toBeVisible();
	await page.getByRole('button', { name: 'Refresh', exact: true }).click();
	await expect(page.getByTestId('model-combobox-status')).toContainText('this is the last one read');
	await refreshed.click();
	await expect(model).toHaveText('GPT 6');
	expect(refreshes).toBe(2);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', model: 'gpt-6', model_server_id: 'server-b' }]);
});

// KUA-204: the approver chooses the workflow a delegation's task follows; a
// pipeline's writer role decides the harness and model.
const emptyPipeline = { kind: 'pipeline', phases: [], roster: [] };
const workflowList = {
	workflows: [
		{ id: 'standard', name: 'Standard', builtin: true, archived: false },
		{ id: 'workflow-loop', name: 'Review loop', builtin: false, archived: false, graph: emptyPipeline },
		{ id: 'workflow-old', name: 'Old flow', builtin: false, archived: true, graph: emptyPipeline }
	],
	defaults: [],
	can_manage: true,
	pipelines_enabled: true
};

async function pendingDelegation(page: Page, payload: Record<string, unknown> = {}) {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-9',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_9',
			name: 'delegate_issue',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-1', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-1',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-9',
			action: 'delegate_issue',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			message_model: answered.model,
			payload: { identifier: 'ENG-1', title: 'Dark mode', instructions: 'Implement dark mode', ...payload },
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	];
	const server = (id: string, name: string, is_default: boolean) => ({
		id,
		name,
		models: ['gpt-5'],
		default_model: 'gpt-5',
		fast_model: '',
		thinking: 'high',
		is_default,
		harnesses: ['codex'],
		thinking_levels: ['off', 'low', 'high', 'max'],
		updated_at: conversation.created_at
	});
	await page.route('**/api/workspaces/test/conversations/chat-1/task-options', (route) =>
		route.fulfill({
			json: {
				providers: [{ id: 'codex', display_name: 'Codex' }],
				default_provider: 'codex',
				model_servers: [server('server-a', 'Gateway A', true), server('server-b', 'Gateway B', false)]
			}
		})
	);
	await page.route('**/api/workspaces/test/workflows', (route) => route.fulfill({ json: workflowList }));
	return state;
}

test('the approval card runs the workflow the approver picked, without a model choice', async ({ page }) => {
	const state = await pendingDelegation(page);
	const bodies: unknown[] = [];
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		const resolved = {
			...state.approvals[0],
			payload: { ...state.approvals[0].payload, workflow_id: 'workflow-loop', workflow: 'Review loop' },
			status: 'approved',
			task_id: 'task-9',
			resolved_at: '2026-10-26T10:05:00Z',
			updated_at: '2026-10-26T10:05:00Z'
		};
		state.approvals = [resolved];
		return route.fulfill({ json: resolved });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	const workflow = page.getByTestId('task-workflow');
	await expect(workflow).toHaveText('Issue default');
	await expect(page.getByRole('button', { name: 'Model server' })).toBeVisible();
	await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toBeVisible();
	await expect(page.getByText("A pipeline's writer role decides", { exact: false })).toHaveCount(0);
	await workflow.click();
	// Standard and the workspace's non-archived pipelines, nothing else.
	await expect(page.getByRole('option')).toHaveText(['Issue default', 'Standard', 'Review loop']);
	await page.getByRole('option', { name: 'Review loop' }).click();
	await expect(workflow).toHaveText('Review loop');
	await expect(page.getByText("A pipeline's writer role decides", { exact: false })).toBeVisible();
	// The workflow decides the agent, so its pickers are gone.
	await expect(page.getByRole('button', { name: 'Model server' })).toHaveCount(0);
	await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toHaveCount(0);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', workflow_id: 'workflow-loop' }]);
	// The approved card shows the workflow that ran.
	await expect(page.getByText('Workflow: Review loop', { exact: true })).toBeVisible();
	await expect(page.getByTestId('task-workflow')).toHaveCount(0);
});

// KUA-291: a pipeline is chosen on the card while the server has pipelines
// on; its writer role then decides the agent. Off, only Standard is left, so
// the card offers no choice.
test('the approval card offers a pipeline only while pipelines are on', async ({ page }) => {
	const state = await pendingDelegation(page);
	let pipelines = false;
	const bodies: unknown[] = [];
	await page.route('**/api/workspaces/test/workflows', (route) =>
		route.fulfill({
			json: {
				...workflowList,
				workflows: [
					...workflowList.workflows,
					{ id: 'workflow-team', name: 'Team', builtin: false, archived: false, graph: emptyPipeline }
				],
				pipelines_enabled: pipelines
			}
		})
	);
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		const resolved = {
			...state.approvals[0],
			payload: { ...state.approvals[0].payload, workflow_id: 'workflow-team', workflow: 'Team' },
			status: 'approved',
			task_id: 'task-9',
			resolved_at: '2026-10-26T10:05:00Z',
			updated_at: '2026-10-26T10:05:00Z'
		};
		state.approvals = [resolved];
		return route.fulfill({ json: resolved });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toBeVisible();
	await expect(page.getByTestId('task-workflow')).toHaveCount(0);

	pipelines = true;
	await page.reload();
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	const workflow = page.getByTestId('task-workflow');
	await workflow.click();
	await expect(page.getByRole('option')).toHaveText(['Issue default', 'Standard', 'Review loop', 'Team']);
	await page.getByRole('option', { name: 'Team' }).click();
	await expect(
		page.getByText("A pipeline's writer role decides the coding agent and model.", { exact: true })
	).toBeVisible();
	await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toHaveCount(0);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Workflow: Team', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', workflow_id: 'workflow-team' }]);
});

test('a model picked before choosing a custom workflow is not sent with it', async ({ page }) => {
	const state = await pendingDelegation(page);
	const bodies: unknown[] = [];
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		return route.fulfill({ json: { ...state.approvals[0], status: 'approved', task_id: 'task-9' } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await page.getByRole('button', { name: 'Model server' }).click();
	await page.getByRole('menuitemradio', { name: 'Gateway B' }).click();
	await page.getByTestId('task-workflow').click();
	await page.getByRole('option', { name: 'Review loop' }).click();
	await expect(page.getByRole('button', { name: 'Model server' })).toHaveCount(0);
	// Back to Standard: the model picker starts again from the proposal.
	await page.getByTestId('task-workflow').click();
	await page.getByRole('option', { name: 'Standard' }).click();
	await expect(page.getByRole('button', { name: 'Model server' })).toHaveText('Gateway A');
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', workflow_id: 'standard' }]);
});

test('an untouched card keeps the workflow the assistant proposed and its model picker', async ({ page }) => {
	const state = await pendingDelegation(page, { workflow_id: 'standard', workflow: 'Standard' });
	const bodies: unknown[] = [];
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		return route.fulfill({ json: { ...state.approvals[0], status: 'approved', task_id: 'task-9' } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByTestId('task-workflow')).toHaveText('Standard');
	await expect(page.getByRole('button', { name: 'Model server' })).toBeVisible();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve' }]);
});

test('going back to the issue default is sent as an empty workflow', async ({ page }) => {
	const state = await pendingDelegation(page, { workflow_id: 'workflow-loop', workflow: 'Review loop' });
	const bodies: unknown[] = [];
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		return route.fulfill({ json: { ...state.approvals[0], status: 'approved', task_id: 'task-9' } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByTestId('task-workflow')).toHaveText('Review loop');
	await expect(page.getByRole('button', { name: 'Model server' })).toHaveCount(0);
	await page.getByTestId('task-workflow').click();
	await page.getByRole('option', { name: 'Issue default' }).click();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', workflow_id: '' }]);
});

test('a workflow the server refuses is explained and the card stays pending', async ({ page }) => {
	const state = await pendingDelegation(page);
	const bodies: unknown[] = [];
	await page.route('**/conversations/chat-1/approvals/approval-1', (route) => {
		bodies.push(route.request().postDataJSON());
		if (bodies.length === 1)
			return route.fulfill({
				status: 422,
				json: {
					error: {
						code: 'INVALID_APPROVAL_EDIT',
						message: 'Workflow not found: name a workflow of this workspace that is not archived, or "standard"'
					}
				}
			});
		return route.fulfill({ json: { ...state.approvals[0], status: 'approved', task_id: 'task-9' } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await page.getByTestId('task-workflow').click();
	await page.getByRole('option', { name: 'Review loop' }).click();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByRole('alert').getByText('Workflow not found', { exact: false })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeEnabled();
	await expect(page.getByTestId('task-workflow')).toHaveText('Review loop');
	// The approver picks another workflow and approves.
	await page.getByTestId('task-workflow').click();
	await page.getByRole('option', { name: 'Standard' }).click();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Coding agent started on ENG-1', { exact: true })).toBeVisible();
	expect(bodies).toEqual([
		{ decision: 'approve', workflow_id: 'workflow-loop' },
		{ decision: 'approve', workflow_id: 'standard' }
	]);
});

test('only a delegation offers a workflow; reviews and follow-ups keep their cards', async ({ page }) => {
	const state = await pendingDelegation(page);
	const base = {
		conversation_id: 'chat-1',
		actor_id: 'user-1',
		issue_id: 'issue-1',
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at
	};
	const call = (id: string, sequence: number, name: string, approval: string) => ({
		...state.tools[0],
		id,
		sequence,
		call_id: `call_${id}`,
		name,
		output: { approval_id: approval, status: 'pending_user_approval', identifier: 'ENG-1' }
	});
	state.tools = [
		state.tools[0],
		call('tool-20', 1, 'request_agent_review', 'approval-20'),
		call('tool-21', 2, 'follow_up_task', 'approval-21')
	];
	state.approvals = [
		...state.approvals,
		{
			...base,
			id: 'approval-20',
			tool_call_id: 'tool-20',
			action: 'request_agent_review',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				harness: 'claude-code',
				post_to_pull_request: false,
				pull_request: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				commit: '0123456789abcdef0123456789abcdef01234567'
			}
		},
		{
			...base,
			id: 'approval-21',
			tool_call_id: 'tool-21',
			action: 'follow_up_task',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				generation: 1,
				instructions: 'Fix the review findings'
			}
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).first().click();
	await expect(page.getByText('Review ENG-1 with Claude Code?', { exact: true })).toBeVisible();
	await expect(page.getByText('Start a follow-up on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByTestId('task-workflow')).toHaveCount(1);
	await expect(page.locator('#approval-workflow-approval-1')).toBeVisible();
});

test('a guest who cannot list workflows keeps the card without a workflow picker', async ({ page }) => {
	await pendingDelegation(page);
	await page.route('**/api/workspaces/test/workflows', (route) =>
		route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN' } } })
	);
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Model server' })).toBeVisible();
	await expect(page.getByTestId('task-workflow')).toHaveCount(0);
});


test('a long conversation list scrolls inside the sidebar, never the page', async ({ page }) => {
	const state = await fixture(page);
	const many = Array.from({ length: 40 }, (_, i) => ({
		...conversation,
		id: `chat-${i + 2}`,
		title: `Conversation ${i + 2}`
	}));
	const requests: URLSearchParams[] = [];
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) => {
		const params = new URL(route.request().url()).searchParams;
		requests.push(params);
		const rows = [conversation, ...many].filter(c => c.title.includes(params.get('q') ?? ''));
		const start = rows.findIndex(c => c.id === params.get('before_id')) + 1;
		return route.fulfill({ json: rows.slice(start, start + Number(params.get('limit') ?? 100)) });
	});
	state.queue = [
		{
			id: 'b',
			conversation_id: 'chat-b',
			state: 'awaiting_review',
			reason: '',
			issue_identifier: 'ENG-2',
			issue_title: 'Title ENG-2',
			updated_at: conversation.created_at
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByRole('region', { name: 'Background tasks', exact: true })).toBeAttached();
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	const more = nav.getByRole('button', { name: 'Load more' });
	await expect(more).toBeVisible();
	expect(requests).toHaveLength(1);
	expect(await nav.getByRole('link').count()).toBeLessThan(41);
	const search = page.getByRole('searchbox', { name: 'Search conversations' });
	await search.fill('Conversation 41');
	await expect(nav.getByRole('link')).toHaveCount(1);
	await expect(nav.getByRole('link', { name: 'Conversation 41' })).toBeVisible();
	expect(requests.at(-1)?.get('q')).toBe('Conversation 41');
	await search.press('Escape');
	await more.click();
	await expect(nav.getByRole('link')).toHaveCount(41);
	await expect(more).toHaveCount(0);
	// Visually hidden labels below the fold must not stretch the document.
	expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true);
	await page.mouse.move(640, 300);
	await page.mouse.wheel(0, 3000);
	expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test('switching conversations keeps Recents listed while the next one loads', async ({ page }) => {
	await fixture(page);
	const second = { ...conversation, id: 'chat-2', title: 'Second chat' };
	let releases: (() => void)[] = [];
	let listRequests = 0;
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', async (route) => {
		if (route.request().method() === 'POST') return route.fulfill({ json: conversation });
		listRequests += 1;
		if (listRequests > 1) await new Promise<void>((release) => releases.push(release));
		return route.fulfill({ json: [conversation, second] });
	});
	await page.route(/\/api\/workspaces\/test\/conversations\/chat-2(\?|$)/, (route) =>
		route.fulfill({
			json: { conversation: second, messages: [], tools: [], approvals: [], cursor: 0, has_more: false }
		})
	);
	await page.routeWebSocket('**/conversations/chat-2/stream*', () => {});
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	await expect(nav.getByRole('link')).toHaveCount(2);
	await nav.getByRole('link', { name: 'Second chat' }).click();
	await expect(page).toHaveURL(/conversation=chat-2/);
	await expect.poll(() => listRequests).toBeGreaterThanOrEqual(2);
	// The remounted sidebar still lists both conversations while the fresh list waits.
	await expect(nav.getByRole('link')).toHaveCount(2);
	await expect(page.getByText('No conversations yet.')).toHaveCount(0);
	releases.forEach((release) => release());
	await expect(nav.getByRole('link')).toHaveCount(2);
});

test('the first conversation list shows loading, not the empty state', async ({ page }) => {
	await fixture(page);
	let release: (() => void) | undefined;
	let requested = false;
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', async (route) => {
		if (route.request().method() === 'POST') return route.fulfill({ json: conversation });
		requested = true;
		await new Promise<void>((resolve) => (release = resolve));
		return route.fulfill({ json: [conversation] });
	});
	await page.goto('/test/assistant');
	const loading = page.getByRole('status').filter({ hasText: 'Loading conversations…' });
	await expect(loading).toBeVisible();
	await expect(page.getByText('No conversations yet.')).toHaveCount(0);
	await expect.poll(() => requested).toBe(true);
	release!();
	await expect(page.getByRole('navigation', { name: 'Conversations' }).getByRole('link')).toHaveCount(1);
	await expect(loading).toHaveCount(0);
});

test('retries a failed conversation list in place', async ({ page }) => {
	await fixture(page);
	let attempts = 0;
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) => {
		if (++attempts === 1) return route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } });
		return route.fulfill({ json: [conversation] });
	});
	await page.goto('/test/assistant');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	await nav.getByRole('button', { name: 'Retry' }).click();
	await expect(nav.getByRole('link', { name: conversation.title })).toBeVisible();
	await expect(nav.getByRole('button', { name: 'Retry' })).toHaveCount(0);
	expect(attempts).toBe(2);
});

test('lists background agent work; failed tasks stay listed and finished ones fold into one row', async ({ page }) => {
	const state = await fixture(page);
	const item = (id: string, identifier: string, taskState: string) => ({
		id,
		conversation_id: `chat-${id}`,
		state: taskState,
		reason: '',
		issue_identifier: identifier,
		issue_title: `Title ${identifier}`,
		provider: 'codex',
		updated_at: conversation.created_at
	});
	state.queue = [
		item('a', 'ENG-1', 'running'),
		item('b', 'ENG-2', 'awaiting_review'),
		item('c', 'WEB-3', 'completed'),
		item('d', 'ENG-4', 'failed')
	];
	await page.goto('/test/assistant');
	const queue = page.getByRole('region', { name: 'Background tasks', exact: true });
	await expect(queue.getByText('3 in progress or waiting', { exact: true })).toBeAttached();
	await expect(queue.getByRole('link', { name: /ENG-1 · Title ENG-1/ })).toHaveAttribute(
		'href',
		'/test/assistant?conversation=chat-a&agent=a'
	);
	await expect(queue.getByRole('link', { name: /ENG-2/ })).toBeVisible();
	// A failure does not disappear into the finished row: it stays listed with its state.
	const failed = queue.getByRole('link', { name: /ENG-4/ });
	await expect(failed).toBeVisible();
	await expect(failed.getByRole('img', { name: 'Failed' })).toBeAttached();
	await expect(queue.getByRole('link', { name: /WEB-3/ })).toBeHidden();
	await queue.getByRole('button', { name: '1 finished' }).click();
	await expect(queue.getByRole('link', { name: /WEB-3/ })).toBeVisible();
});

test('three columns on desktop; both sidebars become drawers on narrow screens', async ({ page }, testInfo) => {
	const state = await fixture(page);
	state.messages = longTranscript(12);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	const sidebar = page.getByRole('complementary', { name: 'Assistant sidebar' });
	const conversationSection = page.getByRole('region', { name: 'Conversation', exact: true });
	const panel = page.getByTestId('side-panel');
	await expect(sidebar).toBeVisible();
	await expect(conversationSection).toBeVisible();
	await expect(panel).toBeVisible();
	await expect(panel.getByText('Background tasks')).toBeVisible();
	// The sidebar header holds the settings cog; there is no search button there and no user footer.
	const cog = sidebar.getByRole('link', { name: 'Assistant', exact: true });
	await expect(cog).toHaveAttribute('href', '/test/settings/assistant');
	const newConversation = sidebar.getByRole('link', { name: 'New conversation', exact: true });
	await expect(newConversation).toHaveAttribute('href', '/test/assistant');
	await expect(newConversation).toHaveAttribute('title', 'New conversation');
	const places = sidebar.getByRole('navigation', { name: 'Assistant places' });
	await expect(places.getByRole('link')).toHaveCount(4);
	const controls = [newConversation, places.getByRole('link', { name: 'Agents' }),
		places.getByRole('link', { name: 'Dev Machines' }), cog, places.getByRole('button', { name: 'Hide conversations' })];
	await newConversation.focus();
	const first = await newConversation.boundingBox();
	for (const control of controls.slice(1)) {
		await page.keyboard.press('Tab');
		await expect(control).toBeFocused();
		expect((await control.boundingBox())!.y).toBe(first!.y);
	}
	await expect(sidebar.getByRole('button', { name: 'Search conversations' })).toHaveCount(0);
	await expect(sidebar.getByRole('searchbox', { name: 'Search conversations' })).toBeVisible();
	// Collapsing hides the whole column; reopening brings the new header back.
	await sidebar.getByRole('button', { name: 'Hide conversations' }).click();
	await expect(sidebar).toHaveCount(0);
	await page.getByRole('button', { name: 'Show conversations' }).click();
	await expect(sidebar).toBeVisible();
	await expect(newConversation).toBeVisible();
	const [left, middle, right] = await Promise.all([
		sidebar.boundingBox(),
		conversationSection.boundingBox(),
		panel.boundingBox()
	]);
	expect(left!.x + left!.width).toBeLessThanOrEqual(middle!.x + 1);
	expect(middle!.x + middle!.width).toBeLessThanOrEqual(right!.x + 1);
	// The side panel can be closed and reopened from the conversation's top bar toggle.
	await page.getByRole('button', { name: 'Close panel' }).click();
	await expect(panel).toHaveCount(0);
	await page.getByTestId('panel-toggle').click();
	await expect(page.getByTestId('side-panel')).toBeVisible();

	await page.setViewportSize({ width: 390, height: 844 });
	await expect(page.getByRole('complementary', { name: 'Assistant sidebar' })).toHaveCount(0);
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.getByRole('button', { name: 'Show conversations' }).click();
	const drawer = page.getByRole('dialog', { name: 'Assistant sidebar' });
	await expect(drawer.getByRole('navigation', { name: 'Conversations' })).toBeVisible();
	await expect(drawer.getByRole('link', { name: 'New conversation', exact: true })).toBeVisible();
	await expect(drawer.getByRole('navigation', { name: 'Assistant places' }).getByRole('link')).toHaveCount(4);
	expect(await drawer.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
	await page.keyboard.press('Escape');
	await expect(drawer).toHaveCount(0);
	await page.getByTestId('panel-toggle').click();
	const details = page.getByRole('dialog', { name: 'Details' });
	await expect(details.getByText('Context', { exact: true })).toHaveCount(0);
	await details.getByRole('button', { name: 'Close panel' }).click();
	await expect(details).toHaveCount(0);
	const contextUsage = page.getByRole('button', { name: 'Context usage' });
	await expect(contextUsage).toHaveAttribute('aria-expanded', 'false');
	await contextUsage.press('Enter');
	const contextPopover = page.getByRole('dialog', { name: 'Context' });
	await expect(contextPopover).toBeVisible();
	await expect(contextPopover).toBeFocused();
	await expect(contextPopover.getByRole('img', { name: 'Context usage 24%' })).toBeVisible();
	await expect(contextPopover.getByText('12 of the latest 50 messages are sent to the model')).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath('assistant-context.png') });
	await page.keyboard.press('Escape');
	await expect(contextPopover).toHaveCount(0);
	await expect(contextUsage).toBeFocused();
});

test('earlier history folds behind one row and unfolds again', async ({ page }) => {
	const state = await fixture(page);
	state.messages = longTranscript(12);
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.locator('[data-slot="message"]')).toHaveCount(12);
	await page.getByRole('button', { name: 'Collapse earlier messages' }).click();
	await expect(page.locator('[data-slot="message"]')).toHaveCount(6);
	await expect(page.getByText('History message 0.', { exact: false })).toHaveCount(0);
	await expect(page.getByText('History message 11.', { exact: false })).toBeVisible();
	// The choice is remembered.
	await page.reload();
	const show = page.getByRole('button', { name: 'Show 6 earlier messages' });
	await expect(show).toBeVisible();
	await show.click();
	await expect(page.locator('[data-slot="message"]')).toHaveCount(12);
});

test('lists the issues a reply used and offers contextual suggestions', async ({ page }) => {
	const state = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const suggestion = page.getByRole('button', { name: 'Which of my issues are in review?' });
	await expect(suggestion).toBeVisible();
	await suggestion.click();
	await expect.poll(() => state.sends).toEqual(['Which of my issues are in review?']);

	const answered = { ...reply, status: 'completed', content: 'Two issues are in review.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 't1',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'c1',
			name: 'search_issues',
			input: { query: 'review' },
			status: 'completed',
			output: {
				issues: [
					{ identifier: 'ENG-1', title: 'Dark mode' },
					{ identifier: 'ENG-2', title: 'Search' },
					{ identifier: 'javascript:alert(1)', title: 'x' }
				]
			},
			created_at: conversation.created_at
		},
		{
			id: 't2',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 1,
			call_id: 'c2',
			name: 'get_issue',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { identifier: 'ENG-1', title: 'Dark mode' },
			created_at: conversation.created_at
		},
		{
			id: 't3',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 2,
			call_id: 'c3',
			name: 'plan_issues',
			input: { identifiers: ['ENG-1', 'ENG-2'] },
			status: 'completed',
			output: { waves: [[{ identifier: 'ENG-1', title: 'Dark mode' }], [{ identifier: 'ENG-2', title: 'Search' }]] },
			created_at: conversation.created_at
		}
	];
	await page.reload();
	await expect(page.getByText('Ran Plan issues')).toBeVisible();
	const sources = page.getByRole('button', { name: 'Used 2 issues' });
	await expect(sources).toBeVisible();
	await sources.click();
	await expect(page.getByRole('link', { name: /ENG-2 · Search/ })).toHaveAttribute('href', '/test/issue/ENG-2');
	await expect(page.getByRole('link', { name: /javascript/ })).toHaveCount(0);
});

test('groups conversations into folders and deletes one after confirmation', async ({ page }) => {
	await fixture(page);
	const other = { ...conversation, id: 'chat-2', title: 'Release notes' };
	const folders: { id: string; name: string; count: number }[] = [];
	const moves: unknown[] = [];
	const deletes: string[] = [];
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) =>
		route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation, other] })
	);
	await page.route('**/api/workspaces/test/conversation-folders', (route) => {
		if (route.request().method() === 'POST') {
			const folder = { id: 'folder-1', count: 0, name: route.request().postDataJSON().name };
			folders.push(folder);
			return route.fulfill({ status: 201, json: folder });
		}
		return route.fulfill({ json: folders });
	});
	await page.route('**/api/workspaces/test/conversations/chat-2', (route) => {
		if (route.request().method() === 'PATCH') {
			moves.push(route.request().postDataJSON());
			return route.fulfill({ json: { ...other, folder_id: route.request().postDataJSON().folder_id } });
		}
		if (route.request().method() === 'DELETE') {
			deletes.push('chat-2');
			return route.fulfill({ status: 204 });
		}
		return route.fallback();
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();
	const live = page.getByRole('button', { name: 'Live' });
	await live.click();
	await expect(
		page.getByText('Connected. New messages, tool calls and agent updates appear here as they happen.')
	).toBeVisible();
	await page.keyboard.press('Escape');

	await page.getByRole('button', { name: 'New folder' }).click();
	// The folder is named in place, in the list.
	const name = nav.getByRole('textbox', { name: 'Folder name' });
	await expect(name).toBeFocused();
	await name.fill('Releases');
	await name.press('Enter');
	await expect(nav.getByRole('button', { name: /^Releases \d+$/ })).toBeVisible();

	await nav.getByRole('link', { name: 'Release notes' }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Move to', exact: true }).click();
	await page.getByRole('menuitem', { name: 'Releases' }).click();
	await expect.poll(() => moves).toEqual([{ folder_id: 'folder-1' }]);
	const folder = nav.getByRole('button', { name: /^Releases \d+$/ });
	await expect(folder).toContainText('1');
	// Selecting a folder filters the separate conversation list.
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();
	await folder.click();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();

	await nav.getByRole('link', { name: 'Release notes' }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Delete' }).click();
	const dialog = page.getByRole('alertdialog');
	await expect(dialog).toContainText('Release notes');
	await dialog.getByRole('button', { name: 'Delete' }).click();
	await expect.poll(() => deletes).toEqual(['chat-2']);
	await expect(nav.getByRole('link', { name: 'Release notes' })).toHaveCount(0);
});

test('drags a conversation into a folder and back out to the list', async ({ page }) => {
	await fixture(page);
	const other = { ...conversation, id: 'chat-2', title: 'Release notes' };
	const folders = [{ id: 'folder-1', count: 0, name: 'Releases' }];
	const moves: unknown[] = [];
	let current = { ...other, folder_id: null as string | null };
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) =>
		route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation, current] })
	);
	await page.route('**/api/workspaces/test/conversation-folders', (route) => route.fulfill({ json: folders }));
	await page.route('**/api/workspaces/test/conversations/chat-2', (route) => {
		if (route.request().method() !== 'PATCH') return route.fallback();
		const body = route.request().postDataJSON();
		moves.push(body);
		current = { ...current, folder_id: body.folder_id };
		return route.fulfill({ json: current });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	const row = nav.locator('[data-conversation="chat-2"]');
	const folder = nav.getByRole('button', { name: /^Releases/ });
	await expect(row).toBeVisible();
	await row.dragTo(folder);
	await expect.poll(() => moves).toEqual([{ folder_id: 'folder-1' }]);
	// All recents remain available after filing a conversation.
	await expect(nav.getByRole('button', { name: /^Releases 1$/ })).toBeVisible();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();
	// Dropping on the list itself takes it out of the folder again.
	await nav.locator('[data-conversation="chat-2"]').dragTo(nav.locator('[data-conversation="chat-1"]'));
	await expect.poll(() => moves).toEqual([{ folder_id: 'folder-1' }, { folder_id: null }]);
	await expect(nav.getByRole('button', { name: /^Releases 0$/ })).toBeVisible();
	// Dropping where it already is changes nothing.
	await nav.locator('[data-conversation="chat-2"]').dragTo(nav.locator('[data-conversation="chat-1"]'));
	await page.waitForTimeout(200);
	expect(moves).toHaveLength(2);
});

test('renames a conversation in place from its menu', async ({ page }) => {
	await fixture(page);
	const renames: unknown[] = [];
	await page.route('**/api/workspaces/test/conversations/chat-1', (route) => {
		if (route.request().method() !== 'PATCH') return route.fallback();
		const body = route.request().postDataJSON();
		renames.push(body);
		return route.fulfill({ json: { ...conversation, title: body.title.trim() } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	const header = page.getByRole('region', { name: 'Conversation' });
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toBeVisible();
	await expect(header.getByText('A persistent conversation')).toBeVisible();

	// Escape keeps the old title and sends nothing.
	await nav.getByRole('link', { name: 'A persistent conversation' }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Rename' }).click();
	const field = nav.getByRole('textbox', { name: 'Conversation name' });
	await expect(field).toBeFocused();
	await expect(field).toHaveValue('A persistent conversation');
	await field.fill('Discarded');
	await field.press('Escape');
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toBeVisible();
	expect(renames).toEqual([]);

	await nav.getByRole('link', { name: 'A persistent conversation' }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Rename' }).click();
	await field.fill('  Launch plan ');
	await field.press('Enter');
	await expect.poll(() => renames).toEqual([{ title: 'Launch plan' }]);
	await expect(nav.getByRole('link', { name: 'Launch plan' })).toBeVisible();
	// The open conversation's header follows the new title.
	await expect(header.getByText('Launch plan')).toBeVisible();
});

test('folds only folders and manages a folder from its context menu', async ({ page }, testInfo) => {
	await fixture(page);
	const folders = [{ id: 'release', name: 'Release planning', count: 1 }, { id: 'design', name: 'Design reviews', count: 0 }];
	const chats = [{ ...conversation, folder_id: 'release' }, { ...conversation, id: 'notes', title: 'Review notes for the next release' }];
	await page.route('**/conversation-folders', route => route.fulfill({ json: folders }));
	await page.route('**/conversation-folders/release', route => {
		if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 });
		return route.fulfill({ json: { ...folders[0], ...route.request().postDataJSON() } });
	});
	await page.route(url => url.pathname.endsWith('/conversations'), route => {
		const folder = new URL(route.request().url()).searchParams.get('folder_id');
		return route.fulfill({ json: folder ? chats.filter(c => 'folder_id' in c && c.folder_id === folder) : chats });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const sidebar = page.getByRole('complementary', { name: 'Assistant sidebar' });
	const folder = sidebar.getByRole('button', { name: 'Release planning 1' });
	const toggle = sidebar.getByRole('button', { name: 'Folders', exact: true });
	await expect(folder).toBeVisible();
	await expect(sidebar.getByRole('link', { name: chats[1].title })).toBeVisible();
	await sidebar.screenshot({ path: testInfo.outputPath('sidebar-desktop.png') });
	await folder.click();
	await expect(folder).toHaveAttribute('aria-pressed', 'true');
	await expect(sidebar.getByRole('link', { name: chats[1].title })).toHaveCount(0);
	await toggle.click();
	await expect(folder).toBeHidden();
	await expect(sidebar.getByRole('link', { name: chats[1].title })).toBeVisible();
	await toggle.click();
	await folder.press('ContextMenu');
	await page.getByRole('menuitem', { name: 'Rename', exact: true }).press('Enter');
	const name = sidebar.getByRole('textbox', { name: 'Folder name' });
	await expect(name).toBeFocused();
	await name.fill('Release archive');
	await name.press('Enter');
	const renamed = sidebar.getByRole('button', { name: 'Release archive 1' });
	await page.evaluate(() => document.documentElement.classList.add('dark'));
	await renamed.click({ button: 'right' });
	const remove = page.getByRole('menuitem', { name: 'Delete', exact: true });
	await expect(remove).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath('sidebar-folder-menu-dark.png'), animations: 'disabled' });
	await remove.click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Delete folder' }).click();
	await expect(renamed).toHaveCount(0);
	await expect(sidebar.getByRole('link', { name: conversation.title })).toBeVisible();
});

test('long press opens the conversation menu in the mobile drawer', async ({ page }, testInfo) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await fixture(page);
	await page.goto('/test/assistant');
	await page.getByRole('button', { name: 'Show conversations' }).click();
	const drawer = page.getByRole('dialog', { name: 'Assistant sidebar' });
	const row = drawer.getByRole('link', { name: conversation.title });
	await expect(row).toBeVisible();
	const session = await page.context().newCDPSession(page);
	const box = (await row.boundingBox())!;
	await session.send('Emulation.setTouchEmulationEnabled', { enabled: true });
	await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: box.x + 20, y: box.y + box.height / 2 }] });
	await expect(page.getByRole('menuitem', { name: 'Rename' })).toBeVisible();
	await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
	await expect(page.getByRole('menuitem', { name: 'Delete', exact: true })).toBeVisible();
	await expect(page).toHaveURL(/\/test\/assistant$/);
	await page.screenshot({ path: testInfo.outputPath('sidebar-mobile-menu.png'), animations: 'disabled' });
});

test('shows project tags and opens row menus from the keyboard', async ({ page }) => {
	await fixture(page);
	await page.addInitScript(() => localStorage.setItem('assistant-project-filter:user-1:test', 'p-1'));
	const requests: (string | null)[] = [];
	await page.route((url) => url.pathname === '/api/workspaces/test/conversations', (route) => {
		requests.push(new URL(route.request().url()).searchParams.get('project_id'));
		return route.fulfill({ json: [
			{ ...conversation, project: { id: 'p-1', name: 'Launch' } },
			{ ...conversation, id: 'chat-2', title: 'Release notes' }
		] });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	await expect(nav.getByText('Launch', { exact: true })).toBeVisible();
	await expect(page.getByRole('combobox', { name: 'Project filter' })).toHaveCount(0);
	expect(requests).toEqual([null]);
	const row = nav.getByRole('link', { name: 'Release notes' });
	await row.press('Shift+F10');
	await expect(page.getByRole('menuitem', { name: 'Rename' })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: 'Move to project' })).toHaveCount(0);
	await page.keyboard.press('Escape');
	await expect(row).toBeFocused();
});

test('asks for approval before merging the reviewed pull request', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Merge approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-10',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_10',
			name: 'merge_pull_request',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-2', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-2',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-10',
			action: 'merge_pull_request',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				repository: 'acme/app',
				pull_request: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				commit: '0123456789abcdef0123456789abcdef01234567'
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByTestId('approval-card').getByRole('button', { name: 'Approve', exact: true })).toBeDisabled();
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Merge pull request #12 for ENG-1?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Merges (squash) the reviewed commit 0123456 of Dark mode in acme/app.', { exact: false })
	).toBeVisible();
	await expect(page.getByRole('link', { name: 'Open pull request' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/pull/12'
	);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Merged the pull request for ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: /Coding agent on ENG-1/ })).toHaveCount(0);
	expect(state.decisions).toEqual(['approve']);
});

// KUA-207: approving a branch behind its base updates it and waits for the new
// head's checks; the card shows that wait, never a failure, until it merges.
test('an approved merge waiting for the updated branch shows it waits, then that it merged', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Merge approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-11',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_11',
			name: 'merge_pull_request',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-3', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	const pending = {
		id: 'approval-3',
		conversation_id: 'chat-1',
		tool_call_id: 'tool-11',
		action: 'merge_pull_request',
		actor_id: 'user-1',
		issue_id: 'issue-1',
		payload: {
			identifier: 'ENG-1',
			title: 'Dark mode',
			task_id: 'task-9',
			repository: 'acme/app',
			pull_request: 276,
			pull_request_url: 'https://github.com/acme/app/pull/276',
			commit: '0123456789abcdef0123456789abcdef01234567'
		},
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at
	};
	const waiting = {
		...pending,
		status: 'waiting_checks',
		next_check_at: '2026-10-26T10:06:00Z',
		updated_at: '2026-10-26T10:05:00Z',
		payload: {
			...pending.payload,
			wait: {
				approved: pending.payload.commit,
				head: 'fedcba9876543210fedcba9876543210fedcba98',
				base: 'main',
				updates: 1,
				since: '2026-10-26T10:05:00Z',
				deadline: '2026-10-26T12:05:00Z',
				waiting:
					'Pull request #276 was 1 commit(s) behind main, and its CI never ran with the latest main. Kuayle brought the branch up to date with GitHub’s update-branch (new head fedcba98); Kuayle merges it once the checks on the new head pass'
			}
		}
	};
	state.approvals = [pending] as any;
	await page.route('**/conversations/chat-1/approvals/approval-3', (route) => {
		state.decisions.push(route.request().postDataJSON().decision);
		state.approvals = [waiting] as any;
		return route.fulfill({ json: waiting });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const card = page.getByTestId('approval-card');
	await card.getByRole('button', { expanded: false }).click();
	await expect(card.getByRole('link', { name: 'Open pull request' })).toHaveAttribute('href', pending.payload.pull_request_url);
	await card.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(
		page.getByText(
			'Approved: Kuayle brought the branch of ENG-1 up to date and merges it once the checks on the new head pass',
			{ exact: true }
		)
	).toBeVisible();
	await expect(page.getByTestId('merge-waiting')).toContainText('new head fedcba98');
	await expect(page.getByText(/Could not merge/)).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
	expect(state.decisions).toEqual(['approve']);

	// The poller merges the updated head; the result arrives as the approval's resolution.
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	const merged = {
		...waiting,
		status: 'approved',
		task_id: 'task-9',
		next_check_at: undefined,
		resolved_at: '2026-10-26T10:20:00Z',
		updated_at: '2026-10-26T10:20:00Z'
	};
	state.sockets.at(-1)!.send(
		JSON.stringify({ id: 'e1', cursor: 1, conversation_id: 'chat-1', type: 'approval.resolved', payload: merged })
	);
	await expect(page.getByText('Merged the pull request for ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByTestId('merge-waiting')).toHaveCount(0);
});

const pullRequestPayload = {
	identifier: 'ENG-1',
	title: 'Dark mode',
	task_id: 'task-9',
	repository: 'acme/app',
	pull_request: 12,
	pull_request_url: 'https://github.com/acme/app/pull/12',
	draft: false
};

const pullRequestCards: {
	name: string;
	payload: Record<string, unknown>;
	title: string;
	text: string[];
	done: string;
}[] = [
	{
		name: 'reply_to_review_comment',
		payload: {
			comment_id: 55,
			thread_id: 'T1',
			author: 'chatgpt-codex-connector',
			bot: true,
			path: 'src/app.go',
			excerpt: 'Possible nil dereference when the cache is empty',
			body: 'Fixed in 1a2b3c4: the cache is now checked first.'
		},
		title: 'Reply to chatgpt-codex-connector on pull request #12 for ENG-1?',
		text: [
			'chatgpt-codex-connector is a bot reviewer.',
			'Possible nil dereference when the cache is empty',
			'Fixed in 1a2b3c4: the cache is now checked first.'
		],
		done: 'Changed the pull request for ENG-1'
	},
	{
		name: 'resolve_review_thread',
		payload: { thread_id: 'T1', author: 'maintainer', path: 'src/app.go', comment_id: 55, excerpt: 'Add a test' },
		title: 'Resolve the review thread on src/app.go for ENG-1?',
		text: ['Add a test'],
		done: 'Changed the pull request for ENG-1'
	},
	{
		name: 'update_pull_request_branch',
		payload: { head: '0123456789abcdef0123456789abcdef01234567', base: 'main', behind: 3 },
		title: 'Update the branch of pull request #12 for ENG-1?',
		text: ['only while its head is still 0123456', 'It cannot resolve conflicts'],
		done: 'Changed the pull request for ENG-1'
	},
	{
		name: 'mark_pull_request_ready',
		payload: { draft: false },
		title: 'Mark pull request #12 for ENG-1 ready for review?',
		text: ['Changes the draft state of Dark mode in acme/app on GitHub.'],
		done: 'Changed the pull request for ENG-1'
	},
	{
		name: 'mark_pull_request_ready',
		payload: { draft: true },
		title: 'Put pull request #12 for ENG-1 back to draft?',
		text: ['Changes the draft state of Dark mode in acme/app on GitHub.'],
		done: 'Changed the pull request for ENG-1'
	}
];

for (const card of pullRequestCards) {
	test(`${card.name} (${JSON.stringify(card.payload.draft ?? '')}) waits for approval and shows exactly what changes`, async ({
		page
	}) => {
		const state = await fixture(page);
		machineApproval(state, card.name, { ...pullRequestPayload, ...card.payload });
		await page.goto('/test/assistant?conversation=chat-1');
		await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
		await expect(page.getByText(card.title, { exact: true })).toBeVisible();
		for (const text of card.text) await expect(page.getByText(text, { exact: false })).toBeVisible();
		await expect(page.getByRole('link', { name: 'Open pull request' })).toHaveAttribute(
			'href',
			'https://github.com/acme/app/pull/12'
		);
		// Nothing is decided before the user answers.
		expect(state.decisions).toEqual([]);
		await page.getByRole('button', { name: 'Approve', exact: true }).click();
		await expect(page.getByText(card.done, { exact: true })).toBeVisible();
		expect(state.decisions).toEqual(['approve']);
	});
}

test('denying a pull request change records the denial and changes nothing', async ({ page }) => {
	const state = await fixture(page);
	machineApproval(state, 'resolve_review_thread', { ...pullRequestPayload, thread_id: 'T1', path: 'a.go' });
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Deny', exact: true }).click();
	await expect(page.getByText('Denied changing the pull request for ENG-1', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['deny']);
});

test('a failed pull request change shows GitHub’s reason and the next step', async ({ page }) => {
	const state = await fixture(page);
	machineApproval(state, 'update_pull_request_branch', {
		...pullRequestPayload,
		head: '0123456789abcdef',
		base: 'main'
	});
	state.approvals[0].status = 'failed' as any;
	(state.approvals[0] as any).error =
		'GitHub refused: merge conflict. If the branch has conflicts or moved, ask for update_pull_request_branch again so the task agent merges the base itself';
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(
		page.getByText(/Could not change the pull request for ENG-1: GitHub refused: merge conflict/)
	).toBeVisible();
	await expect(page.getByText(/task agent merges the base itself/)).toBeVisible();
});

test('merge approval shows the commit others pushed after the task was published', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Merge approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-10',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_10',
			name: 'merge_pull_request',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-2', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-2',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-10',
			action: 'merge_pull_request',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				repository: 'acme/app',
				pull_request: 108,
				pull_request_url: 'https://github.com/acme/app/pull/108',
				commit: '3220dbc9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
				published_commit: '15af2c3abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
				head_change: {
					kind: 'merge',
					tree_changed: false,
					commits: [
						{
							sha: '3220dbc9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
							author: 'maintainer',
							subject: "Merge branch 'main'",
							merge: true
						}
					]
				}
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Merge pull request #108 for ENG-1?', { exact: true })).toBeVisible();
	// The card names the exact commit that merges, not the one Kuayle published.
	await expect(page.getByText('reviewed commit 3220dbc of Dark mode', { exact: false })).toBeVisible();
	const change = page.getByTestId('merge-head-change');
	await expect(change).toContainText('Since Kuayle published 15af2c3, a branch was merged into this pull request.');
	await expect(change).toContainText('The files are identical to what Kuayle published.');
	await expect(page.getByTestId('merge-review-stale')).toContainText('re-run the agent review');
	await expect(change).toContainText("3220dbc by maintainer: Merge branch 'main'");
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	expect(state.decisions).toEqual(['approve']);
});

test('an approval card names the GitHub App permissions the action lacks', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-11',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_11',
			name: 'merge_pull_request',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-3', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	const approval = (githubAccess: Record<string, unknown>) => ({
		id: 'approval-3',
		conversation_id: 'chat-1',
		tool_call_id: 'tool-11',
		action: 'merge_pull_request',
		actor_id: 'user-1',
		issue_id: 'issue-1',
		payload: {
			identifier: 'ENG-1',
			title: 'Dark mode',
			task_id: 'task-9',
			repository: 'acme/app',
			pull_request: 108,
			pull_request_url: 'https://github.com/acme/app/pull/108',
			commit: '3220dbc9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			github_access: githubAccess
		},
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at
	});
	state.approvals = [
		approval({
			missing: [{ name: 'contents', level: 'write' }],
			settings_url: 'https://github.com/settings/apps/kuayle/permissions',
			message: 'The GitHub App lacks permissions this needs (contents: write).'
		})
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByTestId('approval-card')).toContainText('GitHub App permission missing');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	const gap = page.getByTestId('github-access-gap');
	await expect(gap).toContainText('GitHub App permission missing');
	await expect(gap).toContainText('(contents: write)');
	await expect(gap.getByRole('link', { name: 'Open GitHub App settings' })).toHaveAttribute(
		'href',
		'https://github.com/settings/apps/kuayle/permissions'
	);
	// The approval stays available: approving re-checks the permission.
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeEnabled();

	state.approvals = [approval({ missing: [], unverified: true, message: 'GitHub did not answer.' })] as any;
	await page.reload();
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByTestId('github-access-gap')).toContainText("Couldn't verify GitHub App permissions");
	await expect(page.getByTestId('github-access-gap').getByRole('link')).toHaveCount(0);
});

test('merge approval warns when the list of commits others pushed is truncated', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Merge approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-10',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_10',
			name: 'merge_pull_request',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-2', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-2',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-10',
			action: 'merge_pull_request',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				repository: 'acme/app',
				pull_request: 108,
				pull_request_url: 'https://github.com/acme/app/pull/108',
				commit: '3220dbc9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
				published_commit: '15af2c3abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
				head_change: {
					kind: 'merge',
					tree_changed: true,
					truncated: true,
					total_commits: 121,
					commits: [
						{
							sha: '3220dbc9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
							author: 'maintainer',
							subject: "Merge branch 'main'",
							merge: true
						}
					]
				}
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Merge pull request #108 for ENG-1?', { exact: true })).toBeVisible();
	// The card names the exact commit that merges, not the one Kuayle published.
	await expect(page.getByText('reviewed commit 3220dbc of Dark mode', { exact: false })).toBeVisible();
	await expect(page.getByTestId('merge-head-truncated')).toContainText(
		'120 more commits not shown — inspect the pull request on GitHub before approving.'
	);
	await expect(page.getByRole('link', { name: 'Open pull request' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/pull/108'
	);
});

test('asks for approval before accepting work in Kuayle without merging', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Acceptance approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-11',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_11',
			name: 'accept_agent_work',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-3', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-3',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-11',
			action: 'accept_agent_work',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				generation: 2,
				pull_request: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				commit: '0123456789abcdef0123456789abcdef01234567'
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Accept the work on ENG-1 without merging?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Completes iteration 2 of Dark mode in Kuayle. Pull request #12 stays open and is not merged.', {
			exact: false
		})
	).toBeVisible();
	await expect(page.getByRole('link', { name: 'Open pull request' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/pull/12'
	);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Accepted the work on ENG-1 without merging', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: /Coding agent on ENG-1/ })).toHaveCount(0);
	expect(state.decisions).toEqual(['approve']);
});

test('a requested review and a follow-up wait for approval in the chat', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Review approval is pending.' };
	state.messages = [userMessage, answered];
	const call = (id: string, sequence: number, name: string, approval: string) => ({
		id,
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence,
		call_id: `call_${id}`,
		name,
		input: { identifier: 'ENG-1' },
		status: 'completed',
		output: { approval_id: approval, status: 'pending_user_approval', identifier: 'ENG-1' },
		created_at: conversation.created_at
	});
	state.tools = [
		call('tool-20', 0, 'request_agent_review', 'approval-20'),
		call('tool-21', 1, 'follow_up_task', 'approval-21')
	];
	const base = {
		conversation_id: 'chat-1',
		actor_id: 'user-1',
		issue_id: 'issue-1',
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at
	};
	state.approvals = [
		{
			...base,
			id: 'approval-20',
			tool_call_id: 'tool-20',
			action: 'request_agent_review',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				harness: 'claude-code',
				focus: 'Error handling',
				post_to_pull_request: false,
				pull_request: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				commit: '0123456789abcdef0123456789abcdef01234567'
			}
		},
		{
			...base,
			id: 'approval-21',
			tool_call_id: 'tool-21',
			action: 'follow_up_task',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				generation: 1,
				instructions: 'Fix the review findings',
				harness: 'claude-code',
				model: 'claude-opus-5-5',
				effort: 'medium'
			}
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).first().click();
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Review ENG-1 with Claude Code?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('The agent reviews commit 0123456 of pull request #12 without pushing anything.', { exact: true })
	).toBeVisible();
	await expect(
		page.getByText('The findings stay in Kuayle; nothing is posted on GitHub.', { exact: true })
	).toBeVisible();
	await expect(page.getByText('Error handling', { exact: true })).toBeVisible();
	await expect(page.getByText('Start a follow-up on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByText('Fix the review findings', { exact: true })).toBeVisible();
	await expect(page.getByText('Effort: medium', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Approve', exact: true }).first().click();
	await expect(page.getByText('Claude Code review of ENG-1 started', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});

// KUA-193: a follow-up the user confirmed past merged work says so on its
// card, with the pull request; approving it is the confirmation.
test('a follow-up on merged work names the merged pull request on its card', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Follow-up approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-22',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_tool-22',
			name: 'follow_up_task',
			input: { identifier: 'ENG-1', confirm_merged: true },
			status: 'completed',
			output: { approval_id: 'approval-22', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	const payload = {
		identifier: 'ENG-1',
		title: 'Dark mode',
		task_id: 'task-9',
		generation: 1,
		instructions: 'Fix the flaky test',
		merged_pull_request: { number: 12, url: 'https://github.com/acme/app/pull/12', repository: 'acme/app' }
	};
	state.approvals = [
		{
			id: 'approval-22',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-22',
			action: 'follow_up_task',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload,
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Start a follow-up on ENG-1?', { exact: true })).toBeVisible();
	const merged = page.getByTestId('follow-up-merged');
	await expect(merged).toContainText("This issue's work was merged in #12. Approving runs the agent again anyway.");
	await expect(merged.getByRole('link', { name: 'View pull request #12' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/pull/12'
	);
	// A link that is not a web address is not offered.
	payload.merged_pull_request.url = 'javascript:alert(1)';
	await page.reload();
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByTestId('follow-up-merged')).toContainText('merged in #12');
	await expect(page.getByTestId('follow-up-merged').getByRole('link')).toHaveCount(0);
});

// Preview and port approvals: the card says which machine and port and who
// can open the link; once approved, the chat opens the port through the API.
async function machinePorts(page: Page, ports: () => any[]) {
	const launches: string[] = [];
	await page.route('**/api/workspaces/test/dev-machines/machine-1/ports**', (route) => {
		const path = new URL(route.request().url()).pathname;
		const launch = path.match(/\/ports\/([^/]+)\/launch$/);
		if (launch) {
			launches.push(launch[1]);
			const target = ports().find((p) => p.id === launch[1]);
			return route.fulfill({
				status: 201,
				json: { status: 'ready', launch_url: `https://${target.host}/?ticket=redacted` }
			});
		}
		return route.fulfill({ json: { ports: ports(), listening: [], scanned_at: null } });
	});
	await page
		.context()
		.route('https://*.machines.example.test/**', (route) =>
			route.fulfill({ contentType: 'text/html', body: '<title>App</title>' })
		);
	return launches;
}

function machineApproval(state: Awaited<ReturnType<typeof fixture>>, name: string, payload: Record<string, unknown>) {
	const answered = { ...reply, status: 'completed', content: 'Approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-30',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_30',
			name,
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: { approval_id: 'approval-30', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-30',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-30',
			action: name,
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				machine_id: 'machine-1',
				machine: 'eng-1-dark-mode',
				audience: 'owner',
				...payload
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
}

const previewPort = {
	id: 'port-9',
	port: 4100,
	label: 'ENG-1',
	source: 'task',
	agent_task_id: 'task-9',
	task_identifier: 'ENG-1',
	host: '0123456789abcdef0123-4100.machines.example.test',
	listening: true,
	created_at: '2026-10-26T10:06:00Z'
};

test('a preview waits for approval, then opens its link from the chat', async ({ page }) => {
	const state = await fixture(page);
	machineApproval(state, 'start_preview', { command: ['npm', 'run', 'dev'], port: 5173 });
	let started = false;
	const launches = await machinePorts(page, () => (started ? [previewPort] : []));
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Start the preview of ENG-1?', { exact: true })).toBeVisible();
	await expect(
		page.getByText("Runs the preview command of Dark mode in the task's worktree on machine eng-1-dark-mode.", {
			exact: true
		})
	).toBeVisible();
	await expect(page.getByText('npm run dev', { exact: true })).toBeVisible();
	await expect(page.getByText('Preferred port 5173; the machine picks a free one if it is taken.')).toBeVisible();
	await expect(
		page.getByText("Only you, the machine's owner, can open the link while signed in to Kuayle.")
	).toBeVisible();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Started the preview of ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByText('Waiting for the preview to get its port…')).toBeVisible();
	started = true;
	const open = page.getByRole('button', { name: 'Open ENG-1' });
	await expect(open).toBeVisible({ timeout: 10_000 });
	await expect(page.getByText(previewPort.host, { exact: true })).toBeVisible();
	const popupPromise = page.waitForEvent('popup');
	await open.click();
	const popup = await popupPromise;
	await expect.poll(() => popup.url(), { timeout: 10_000 }).toContain(previewPort.host);
	expect(await popup.evaluate(() => window.opener)).toBeNull();
	expect(launches).toEqual(['port-9']);
	expect(state.decisions).toEqual(['approve']);
});

test('an exposed port says it is reused and lists its link', async ({ page }) => {
	const state = await fixture(page);
	const host = '0123456789abcdef0123-3000.machines.example.test';
	machineApproval(state, 'expose_port', { port: 3000, label: 'web', host, existing: true });
	const web = { ...previewPort, id: 'port-1', port: 3000, label: 'web', source: 'user', agent_task_id: null, host };
	await machinePorts(page, () => [web]);
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Expose port 3000 of ENG-1?', { exact: true })).toBeVisible();
	await expect(
		page.getByText(`Publishes port 3000 of machine eng-1-dark-mode at ${host}.`, { exact: true })
	).toBeVisible();
	await expect(page.getByText('The machine already exposes this port; approving reuses its link.')).toBeVisible();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Exposed port 3000 of ENG-1', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Open web' })).toBeVisible();
});

test('listed ports open from the chat, never from the tool output', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Here are the ports.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-31',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_31',
			name: 'list_ports',
			input: { identifier: 'ENG-1' },
			status: 'completed',
			output: {
				identifier: 'ENG-1',
				machine_id: '00000000-0000-4000-8000-000000000001',
				ports: [{ port: 3000, url: 'https://evil.example.test' }]
			},
			created_at: conversation.created_at
		}
	];
	const requested: string[] = [];
	await page.route('**/api/workspaces/test/dev-machines/*/ports', (route) => {
		requested.push(new URL(route.request().url()).pathname);
		return route.fulfill({ json: { ports: [previewPort], listening: [], scanned_at: null } });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Ran List ports', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Open ENG-1' })).toBeVisible();
	await expect(page.getByRole('link', { name: /evil/ })).toHaveCount(0);
	expect(requested).toEqual(['/api/workspaces/test/dev-machines/00000000-0000-4000-8000-000000000001/ports']);
});

test('a check run waits for approval and shows the exact command it runs', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Check approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-40',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_tool-40',
			name: 'run_check',
			input: { identifier: 'ENG-1', command: 'npm test -- auth' },
			status: 'completed',
			output: { approval_id: 'approval-40', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-40',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-40',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			action: 'run_check',
			payload: {
				identifier: 'ENG-1',
				title: 'Dark mode',
				task_id: 'task-9',
				generation: 1,
				command: ['sh', '-c', 'npm test -- auth']
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Run the check on ENG-1?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Runs on the checkout of Dark mode, without starting the agent or changing the task:', {
			exact: true
		})
	).toBeVisible();
	await expect(page.getByText('["sh","-c","npm test -- auth"]', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Check started on ENG-1; its result appears on the task', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});

test('an instance upgrade waits for approval, shows both versions and follows the updater', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Upgrade approval is pending.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-50',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_tool-50',
			name: 'upgrade_instance',
			input: {},
			status: 'completed',
			output: { approval_id: 'approval-50', status: 'pending_user_approval' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-50',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-50',
			actor_id: 'user-1',
			issue_id: '00000000-0000-0000-0000-000000000000',
			action: 'upgrade_instance',
			payload: { current_version: '0.2.8', latest_version: '0.3.0', changelog: 'v0.3.0: Faster boards' },
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	let polls = 0;
	await page.route('**/api/system/update-status', (route) => {
		polls += 1;
		return route.fulfill({
			json:
				polls < 2
					? { enabled: true, running: true, state: 'running' }
					: { enabled: true, running: false, state: 'succeeded' }
		});
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Upgrade Kuayle from 0.2.8 to 0.3.0?', { exact: true })).toBeVisible();
	await expect(page.getByText('v0.3.0: Faster boards', { exact: true })).toBeVisible();
	// Nothing is requested from the updater, and nothing runs, before approval.
	expect(polls).toBe(0);
	expect(state.decisions).toEqual([]);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(
		page.getByText('Upgrade to 0.3.0 started; the instance restarts, follow it below', { exact: true })
	).toBeVisible();
	await expect(
		page.getByText('Upgrade finished and the health check passed. Reload to run the new version.', { exact: true })
	).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});

test('a release waits for approval with its notes editable, and publishes the edited notes', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'The release is waiting for your approval.' };
	state.messages = [userMessage, answered];
	const draft =
		'### Features\n- **Release tools (KUA-88).**\n\n**Full Changelog**: https://github.com/acme/app/compare/v1.2.0...v1.3.0';
	state.tools = [
		{
			id: 'tool-60',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_tool-60',
			name: 'create_release',
			input: { repository: 'acme/app', tag: 'v1.3.0' },
			status: 'completed',
			output: { approval_id: 'approval-60', status: 'pending_user_approval' },
			created_at: conversation.created_at
		}
	];
	const pending = {
		id: 'approval-60',
		conversation_id: 'chat-1',
		tool_call_id: 'tool-60',
		actor_id: 'user-1',
		issue_id: '00000000-0000-0000-0000-000000000000',
		action: 'create_release',
		payload: {
			repository: 'acme/app',
			tag: 'v1.3.0',
			name: 'v1.3.0',
			target_commit: '0123456789abcdef0123456789abcdef01234567',
			branch: 'main',
			previous_tag: 'v1.2.0',
			notes: draft,
			checks_passed: 12,
			commits: 4,
			scanned_files: 9,
			unscanned_files: ['UI/static/logo.png'],
			compare_url: 'https://github.com/acme/app/compare/v1.2.0...0123456789abcdef0123456789abcdef01234567',
			release_url: 'https://github.com/acme/app/releases/tag/v1.3.0'
		},
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at
	};
	state.approvals = [pending] as any;
	const bodies: any[] = [];
	await page.route('**/conversations/chat-1/approvals/**', (route) => {
		const body = route.request().postDataJSON();
		bodies.push(body);
		// The server checks the edited notes again; the first edit holds a private address.
		if (body.notes?.includes('10.0.0.8'))
			return route.fulfill({
				status: 422,
				json: {
					error: {
						code: 'INVALID_APPROVAL_EDIT',
						message:
							'The check of the release notes found 1 secret(s), private address(es), host name(s) or local path(s): notes line 1 (private address).'
					}
				}
			});
		const approved = {
			...pending,
			payload: { ...pending.payload, notes: body.notes ?? draft, notes_edited: body.notes !== undefined },
			status: 'approved',
			resolved_at: '2026-10-26T10:05:00Z',
			updated_at: '2026-10-26T10:05:00Z'
		};
		state.approvals = [approved] as any;
		return route.fulfill({ json: approved });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Publish v1.3.0 of acme/app?', { exact: true })).toBeVisible();
	await expect(page.getByText('Tags 0123456 on main, where all 12 checks passed.', { exact: false })).toBeVisible();
	await expect(page.getByTestId('release-scan')).toContainText(
		'Since v1.2.0: 4 commits; the secret scan read 9 changed files'
	);
	await expect(page.getByText(/UI\/static\/logo\.png/)).toBeVisible();
	const notes = page.getByLabel('Release notes (published as you leave them)');
	await expect(notes).toHaveValue(draft);
	expect(bodies).toEqual([]);

	// An empty edit cannot be approved.
	await notes.fill('   ');
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeDisabled();

	// The server's reason for refusing an edit is shown, and the card stays pending.
	await notes.fill('- staged on 10.0.0.8 first');
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByRole('alert').filter({ hasText: 'notes line 1 (private address)' })).toBeVisible();
	await expect(notes).toBeVisible();

	// Reset restores the draft; the edited notes are what is sent and published.
	await page.getByRole('button', { name: 'Reset to the draft' }).click();
	await expect(notes).toHaveValue(draft);
	const edited = '### Fixed\n- **Edited on the card (KUA-88).**';
	await notes.fill(edited);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Published v1.3.0', { exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Open the release' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/releases/tag/v1.3.0'
	);
	expect(bodies.map((body) => body.decision)).toEqual(['approve', 'approve']);
	expect(bodies[1]).toEqual({ decision: 'approve', notes: edited });
});

// A ship_pull_request chain approval for ENG-7, with the given steps.
function shipApproval(steps: { step: string; status: string; detail?: string; url?: string }[], extra = {}) {
	return {
		id: 'approval-70',
		conversation_id: 'chat-1',
		tool_call_id: 'tool-70',
		actor_id: 'user-1',
		issue_id: 'issue-7',
		action: 'ship_pull_request',
		payload: {
			identifier: 'ENG-7',
			title: 'Ship it',
			repository: 'acme/app',
			pull_request: 42,
			merge: {
				identifier: 'ENG-7',
				title: 'Ship it',
				task_id: 'task-7',
				repository: 'acme/app',
				pull_request: 42,
				pull_request_url: 'https://github.com/acme/app/pull/42',
				commit: 'abcdef0123456789abcdef0123456789abcdef01',
				merge_method: 'squash'
			},
			release: {
				repository: 'acme/app',
				tag: 'v1.4.0',
				name: 'v1.4.0',
				target_commit: '',
				branch: 'main',
				previous_tag: 'v1.3.0',
				notes: '### Features\n- **Ship in one go (KUA-90).**',
				checks_passed: 0,
				commits: 0,
				scanned_files: 0,
				release_url: 'https://github.com/acme/app/releases/tag/v1.4.0'
			},
			upgrade: { current_version: '1.3.0', latest_version: '1.4.0' },
			steps
		},
		status: 'pending',
		expires_at: '2026-10-27T10:00:00Z',
		created_at: conversation.created_at,
		updated_at: conversation.created_at,
		...extra
	};
}

async function shipFixture(page: Page) {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Shipping waits for your approval.' };
	state.messages = [userMessage, answered];
	state.tools = [
		{
			id: 'tool-70',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_tool-70',
			name: 'ship_pull_request',
			input: { identifier: 'ENG-7', tag: 'v1.4.0', upgrade: true },
			status: 'completed',
			output: { approval_id: 'approval-70', status: 'pending_user_approval' },
			created_at: conversation.created_at
		}
	];
	return state;
}

const pendingSteps = ['merge', 'tests', 'release', 'build', 'upgrade'].map((step) => ({ step, status: 'pending' }));

test('shipping asks for one approval that lists every step, then follows each step', async ({ page }) => {
	const state = await shipFixture(page);
	const pending = shipApproval(pendingSteps);
	state.approvals = [pending] as any;
	const bodies: any[] = [];
	await page.route('**/conversations/chat-1/approvals/**', (route) => {
		const body = route.request().postDataJSON();
		bodies.push(body);
		const approving = {
			...pending,
			payload: { ...pending.payload, release: { ...pending.payload.release, notes: body.notes } },
			status: 'approving',
			updated_at: '2026-10-26T10:05:00Z'
		};
		state.approvals = [approving] as any;
		return route.fulfill({ json: approving });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Ship ENG-7: merge #42 and publish v1.4.0 of acme/app?', { exact: true })).toBeVisible();
	// Every step is listed, in order, before anything runs.
	const steps = page.getByTestId('ship-steps').getByRole('listitem');
	await expect(steps).toHaveCount(5);
	await expect(steps.nth(0)).toContainText('Merge pull request #42 (squash) at abcdef0');
	await expect(steps.nth(1)).toContainText('Wait for the checks of the merge commit on main');
	await expect(steps.nth(2)).toContainText('Publish v1.4.0 at the merge commit');
	await expect(steps.nth(3)).toContainText('Wait for the release build');
	await expect(steps.nth(4)).toContainText('Upgrade this instance from 1.3.0 to 1.4.0');
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(1);
	expect(bodies).toEqual([]);

	// The notes are editable as on a release card; the edit is what is approved.
	const notes = page.getByLabel('Release notes (published as you leave them)');
	await expect(notes).toHaveValue(pending.payload.release.notes);
	await notes.fill('   ');
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toBeDisabled();
	const edited = '### Features\n- **Edited before shipping.**';
	await notes.fill(edited);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Shipping ENG-7 step by step', { exact: true })).toBeVisible();
	expect(bodies).toEqual([{ decision: 'approve', notes: edited }]);

	// Progress arrives as approval updates; the finished chain links the release.
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	const socket = state.sockets.at(-1)!;
	const running = shipApproval(
		[
			{ step: 'merge', status: 'done', detail: 'Merged #42 by squash as 1234567' },
			{ step: 'tests', status: 'running' },
			...pendingSteps.slice(2)
		],
		{ status: 'approving', updated_at: '2026-10-26T10:06:00Z' }
	);
	socket.send(
		JSON.stringify({ id: 'e1', cursor: 1, conversation_id: 'chat-1', type: 'approval.updated', payload: running })
	);
	await expect(page.getByText('Merged #42 by squash as 1234567', { exact: true })).toBeVisible();
	const done = shipApproval(
		[
			{ step: 'merge', status: 'done', detail: 'Merged #42 by squash as 1234567' },
			{ step: 'tests', status: 'done', detail: '12 check(s) passed on main at 1234567' },
			{
				step: 'release',
				status: 'done',
				detail: 'Published v1.4.0 at 1234567 with 9 scanned file(s)',
				url: 'https://github.com/acme/app/releases/tag/v1.4.0'
			},
			{ step: 'build', status: 'done', detail: 'The release build succeeded: Release' },
			{ step: 'upgrade', status: 'done', detail: 'Started the upgrade from 1.3.0 to 1.4.0; the instance restarts' }
		],
		{ status: 'approved', resolved_at: '2026-10-26T10:30:00Z', updated_at: '2026-10-26T10:30:00Z' }
	);
	socket.send(
		JSON.stringify({ id: 'e2', cursor: 2, conversation_id: 'chat-1', type: 'approval.resolved', payload: done })
	);
	await expect(page.getByText('Shipped ENG-7: merged, tested and released v1.4.0', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Started the upgrade from 1.3.0 to 1.4.0; the instance restarts', { exact: true })
	).toBeVisible();
	await expect(page.getByRole('link', { name: 'Open the release' }).first()).toHaveAttribute(
		'href',
		'https://github.com/acme/app/releases/tag/v1.4.0'
	);
});

test('a failed ship step is reported with its reason, and later steps show they did not run', async ({ page }) => {
	const state = await shipFixture(page);
	const error =
		'Step 2 of 5 (wait for the tests) failed: 1 check failed on main. Next step: pull request #42 is merged as 1234567 but nothing was released. Not run: publish the release, wait for the release build, upgrade the instance.';
	state.approvals = [
		shipApproval(
			[
				{ step: 'merge', status: 'done', detail: 'Merged #42 by squash as 1234567' },
				{ step: 'tests', status: 'failed', detail: '1 check failed on main' },
				{ step: 'release', status: 'skipped' },
				{ step: 'build', status: 'skipped' },
				{ step: 'upgrade', status: 'skipped' }
			],
			{ status: 'failed', error, resolved_at: '2026-10-26T10:30:00Z', updated_at: '2026-10-26T10:30:00Z' }
		)
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText(`Shipping ENG-7 stopped: ${error}`, { exact: true })).toBeVisible();
	const steps = page.getByTestId('ship-steps').getByRole('listitem');
	await expect(steps.nth(1)).toContainText('Failed');
	await expect(steps.nth(1)).toContainText('1 check failed on main');
	for (const index of [2, 3, 4]) await expect(steps.nth(index)).toContainText('Not run');
	await expect(page.getByRole('link', { name: 'Open the release' })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
});

test('message_task shows the whole message and sends it only when approved', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'Approve the message to send it.' };
	state.messages = [userMessage, answered];
	const body = 'Also add tests.\nKeep the v1 route until the migration lands.';
	state.tools = [
		{
			id: 'tool-live-1',
			conversation_id: 'chat-1',
			message_id: answered.id,
			sequence: 0,
			call_id: 'call_1',
			name: 'message_task',
			input: { identifier: 'ENG-1', body },
			status: 'completed',
			output: { approval_id: 'approval-live-1', status: 'pending_user_approval', identifier: 'ENG-1' },
			created_at: conversation.created_at
		}
	];
	state.approvals = [
		{
			id: 'approval-live-1',
			conversation_id: 'chat-1',
			tool_call_id: 'tool-live-1',
			action: 'message_task',
			actor_id: 'user-1',
			issue_id: 'issue-1',
			payload: { identifier: 'ENG-1', title: 'Dark mode', task_id: 'task-9', body },
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Send this message to the agent on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByTestId('approval-card')).toContainText('Dark mode');
	const shown = page.getByTestId('approval-details');
	await expect(shown.getByText('Also add tests.', { exact: false })).toBeVisible();
	await expect(shown.getByText('Keep the v1 route until the migration lands.', { exact: false })).toBeVisible();
	await expect(page.getByText('Message sent to the running agent of ENG-1')).toHaveCount(0);
	expect(state.decisions).toEqual([]);
	await page.getByRole('button', { name: 'Approve', exact: true }).click();
	await expect(page.getByText('Message sent to the agent on ENG-1', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});

// Calls recorded before messages needed approval were sent at once.
test('an earlier message_task call still says where its message went', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'I told the agent.' };
	state.messages = [userMessage, answered];
	const call = {
		id: 'tool-live-1',
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence: 0,
		call_id: 'call_1',
		name: 'message_task',
		input: { identifier: 'ENG-1', body: 'Also add tests' },
		status: 'completed',
		output: { identifier: 'ENG-1', sent: true, seq: 1, task_state: 'running', delivery: 'live' },
		created_at: conversation.created_at
	};
	state.tools = [
		call,
		{
			...call,
			id: 'tool-live-2',
			sequence: 1,
			call_id: 'call_2',
			input: { identifier: 'ENG-2', body: 'Keep v1' },
			output: { identifier: 'ENG-2', sent: true, seq: 1, task_state: 'awaiting_review', delivery: 'next_attempt' }
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const live = page.getByRole('button', { name: 'Message sent to the running agent of ENG-1' });
	await expect(live).toBeVisible();
	await expect(
		page.getByRole('button', { name: "Message for ENG-2 queued for the agent's next iteration" })
	).toBeVisible();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
	await live.click();
	await expect(
		page.locator('[data-slot="code-block-code"]').getByText('Also add tests', { exact: false })
	).toBeVisible();
});

test('a proposed skill shows its full draft and links to Settings; a loaded skill is one line', async ({ page }) => {
	const state = await fixture(page);
	const answered = { ...reply, status: 'completed', content: 'I saved the draft.' };
	state.messages = [userMessage, answered];
	const skillId = '00000000-0000-4000-8000-0000000000aa';
	const tool = (id: string, sequence: number, name: string, input: unknown, output: unknown) => ({
		id,
		conversation_id: 'chat-1',
		message_id: answered.id,
		sequence,
		call_id: `call_${id}`,
		name,
		input,
		status: 'completed',
		output,
		created_at: conversation.created_at
	});
	const body = '## Steps\n\n1. Bump the version\n2. Tag the release\n\n<img src=x onerror="window.__xss=1">';
	state.tools = [
		tool(
			'tool-40',
			0,
			'load_skill',
			{ name: 'How we triage' },
			{ skill_id: skillId, name: 'How we triage', version: 2, team_key: null, bytes: 120 }
		),
		tool(
			'tool-41',
			1,
			'propose_skill',
			{ name: 'How we release', description: 'Use when releasing.', body },
			{
				skill_id: skillId,
				name: 'How we release',
				description: 'Use when asked how to cut a release.',
				body,
				team_key: 'ENG',
				version: 3,
				status: 'draft',
				skill_status: 'active',
				active_version: 2,
				note: 'Saved as draft version 3.'
			}
		)
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Used skill How we triage', { exact: true })).toBeVisible();
	const card = page.getByRole('article', { name: 'Proposed skill: How we release' });
	await expect(card).toBeVisible();
	await expect(card.getByText('Draft — not active until activated')).toBeVisible();
	await expect(card.getByText('Use when asked how to cut a release.')).toBeVisible();
	await expect(card.getByRole('heading', { name: 'Steps' })).toBeVisible();
	await expect(card.getByText('Tag the release')).toBeVisible();
	await expect(
		card.getByText('Draft version 3 for ENG. Active version 2 stays in effect until the draft is activated.')
	).toBeVisible();
	await expect(card.locator('img')).toHaveCount(0);
	expect(await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)).toBeUndefined();
	await expect(card.getByRole('link', { name: 'Review in Settings' })).toHaveAttribute(
		'href',
		`/test/settings/assistant?tab=skills&skill=${skillId}`
	);
});

test('the composer references wiki pages chosen with @ or dropped from a wiki tree', async ({ page }) => {
	const state = await fixture(page);
	state.appendSends = true;
	const guide = '00000000-0000-0000-0000-000000000801';
	const plan = '00000000-0000-0000-0000-000000000802';
	const pages = [
		{ id: guide, accessible: true, title: 'Deploy guide', team_id: 'team-1', scope_name: 'Engineering' },
		{ id: plan, accessible: true, title: 'Launch plan', project_id: 'project-1', scope_name: 'Launch' }
	];
	await page.route('**/api/workspaces/test/wiki/references**', (route) => {
		const request = route.request();
		if (request.method() === 'POST')
			return route.fulfill({
				json: (request.postDataJSON().ids as string[]).map(
					(id) => pages.find((p) => p.id === id) ?? { id, accessible: false }
				)
			});
		const query = (new URL(request.url()).searchParams.get('q') ?? '').toLowerCase();
		return route.fulfill({ json: pages.filter((p) => p.title.toLowerCase().includes(query)) });
	});
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.click();
	await composer.pressSequentially('Summarise @dep');
	const option = page.locator('.mention-menu').getByRole('button', { name: /Deploy guide/ });
	await expect(option).toContainText('Engineering');
	// Enter picks the page instead of sending the message.
	await composer.press('Enter');
	const guideToken = `@[Deploy guide](wiki:${guide} team Engineering)`;
	await expect(composer).toHaveValue(`Summarise ${guideToken} `);
	expect(state.sends).toEqual([]);

	// A page dragged from a wiki tree carries only its ID.
	const dataTransfer = await page.evaluateHandle((id) => {
		const data = new DataTransfer();
		data.setData('application/x-kuayle-wiki-page', id);
		return data;
	}, plan);
	await composer.dispatchEvent('drop', { dataTransfer });
	const planToken = `@[Launch plan](wiki:${plan} project Launch)`;
	await expect(composer).toHaveValue(`Summarise ${guideToken} ${planToken} `);

	await composer.press('Enter');
	await expect.poll(() => state.sends).toEqual([`Summarise ${guideToken} ${planToken} `]);
	// The sent message shows the references as chips, not as tokens.
	const chips = page.getByTestId('wiki-reference');
	await expect(chips).toHaveCount(2);
	await expect(chips.nth(0)).toHaveText(/Deploy guide\s*Engineering/);
	await expect(chips.nth(0)).toHaveAttribute('href', `/test/teams/team-1/wiki?page=${guide}`);
	await expect(page.getByText('wiki:', { exact: false })).toHaveCount(0);
});

const copyCode = 'func main() {\n\tfmt.Println("**hi**")\n}';
const copyPrompt = `Line one\n  indented line two\n\n${'long '.repeat(3000)}end https://example.com/a?b=1`;
const copyAnswer = `## Plan\n\nUse **bold** and [the docs](https://example.com/docs).\n\n\`\`\`go\n${copyCode}\n\`\`\`\n\n- first\n- second`;

test('copies a user message and an assistant reply in full, by pointer and by keyboard', async ({ page, context }) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	const state = await fixture(page);
	state.messages = [
		{ ...userMessage, content: copyPrompt },
		{ ...reply, content: copyAnswer, status: 'completed' }
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const clipboard = () => page.evaluate(() => navigator.clipboard.readText());
	// One control per message, in message order: the user's, then the reply's.
	const buttons = page.getByRole('button', { name: 'Copy message' });
	await expect(buttons).toHaveCount(2);

	// Out of the way until its message is hovered.
	await page.mouse.move(0, 0);
	await expect(buttons.first()).toHaveCSS('opacity', '0');
	await page.getByText('Line one', { exact: false }).hover();
	await expect(buttons.first()).toHaveCSS('opacity', '1');
	await buttons.first().click();
	// The user's text is copied exactly, however long.
	await expect.poll(clipboard).toBe(copyPrompt);
	await expect(page.getByText('Copied', { exact: true })).toHaveCount(1);

	// Keyboard: the control takes focus, shows itself and copies on Enter.
	await page.mouse.move(0, 0);
	await buttons.nth(1).focus();
	await expect(buttons.nth(1)).toHaveCSS('opacity', '1');
	await page.keyboard.press('Enter');
	// The reply is copied as it reads: no Markdown markers, code and link address kept.
	await expect
		.poll(clipboard)
		.toBe(`Plan\n\nUse bold and the docs (https://example.com/docs).\n\n${copyCode}\n\n- first\n- second`);
	await expect(buttons.nth(1)).toBeFocused();
	// Rendering is untouched: the reply is still Markdown.
	await expect(page.getByRole('link', { name: 'the docs' })).toHaveAttribute('href', 'https://example.com/docs');
	await expect(page.locator('pre')).toHaveText(copyCode);
});

test('the copy control is always shown where nothing can hover', async ({ browser }) => {
	const context = await browser.newContext({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 844 } });
	const page = await context.newPage();
	const state = await fixture(page);
	state.messages = [
		{ ...userMessage, content: copyPrompt },
		{ ...reply, content: copyAnswer, status: 'completed' }
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const buttons = page.getByRole('button', { name: 'Copy message' });
	await expect(buttons).toHaveCount(2);
	for (const button of await buttons.all()) {
		await expect(button).toHaveCSS('opacity', '1');
		const reserved = await button.evaluate(el => {
			const content = el.closest('[data-slot="bubble"]')!.querySelector('[data-slot="bubble-content"]')!;
			return parseFloat(getComputedStyle(content).paddingInlineEnd);
		});
		const box = (await button.boundingBox())!;
		expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(44);
		expect(reserved).toBeGreaterThanOrEqual(box.width);
	}
	await context.close();
});

test('a refused copy says so instead of confirming', async ({ page }) => {
	await page.addInitScript(() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { writeText: () => Promise.reject(new Error('denied')) }
		});
		document.execCommand = () => false;
	});
	const state = await fixture(page);
	state.messages = [{ ...userMessage, content: copyPrompt }];
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Copy message' }).click();
	await expect(page.getByText('Could not copy the message')).toBeVisible();
	await expect(page.getByText('Copied', { exact: true })).toHaveCount(0);
});

test('the copy control sits at the top-right inside the bubble and never covers text', async ({ page }) => {
	const state = await fixture(page);
	const wideUser =
		'A user message whose first line runs the full width of its bubble so the copy control has to stay in the reserved space at the right. '.repeat(
			2
		);
	const wideReply =
		'An assistant reply whose first line also runs the full width of its bubble, leaving the copy control clear of every glyph. '.repeat(
			2
		);
	state.messages = [
		{ ...userMessage, content: wideUser },
		{ ...reply, content: wideReply, status: 'completed' }
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const buttons = page.getByRole('button', { name: 'Copy message' });
	await expect(buttons).toHaveCount(2);

	// Hover still reveals the control, and keyboard focus keeps it visible.
	await page.mouse.move(0, 0);
	await expect(buttons.first()).toHaveCSS('opacity', '0');
	await page.getByText('A user message', { exact: false }).hover();
	await expect(buttons.first()).toHaveCSS('opacity', '1');
	await page.mouse.move(0, 0);
	await buttons.nth(1).focus();
	await expect(buttons.nth(1)).toHaveCSS('opacity', '1');

	const firstLineRight = (content: ReturnType<Page['locator']>) =>
		content.evaluate((el) => {
			const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
			let node = walker.nextNode();
			while (node && !node.textContent?.trim()) node = walker.nextNode();
			if (!node) return null;
			const range = document.createRange();
			range.selectNodeContents(node);
			return (range.getClientRects()[0] ?? range.getBoundingClientRect()).right;
		});

	for (const button of await buttons.all()) {
		await button.scrollIntoViewIfNeeded();
		const bubble = button.locator('xpath=ancestor::*[@data-slot="bubble"]');
		await expect(bubble).toHaveCount(1);
		const box = (await button.boundingBox())!;
		const bubbleBox = (await bubble.boundingBox())!;
		expect(box.x).toBeGreaterThanOrEqual(bubbleBox.x);
		expect(box.y).toBeGreaterThanOrEqual(bubbleBox.y);
		expect(box.x + box.width).toBeLessThanOrEqual(bubbleBox.x + bubbleBox.width);
		expect(box.y + box.height).toBeLessThanOrEqual(bubbleBox.y + bubbleBox.height);
		expect(bubbleBox.x + bubbleBox.width - (box.x + box.width)).toBeLessThanOrEqual(8);
		expect(box.y - bubbleBox.y).toBeLessThanOrEqual(8);
		const right = await firstLineRight(bubble.locator('[data-slot=bubble-content]'));
		expect(right).not.toBeNull();
		expect(right!).toBeLessThanOrEqual(box.x);
	}

	// A one-word reply stays clear too: the bubble grows around the reserved space.
	state.messages = [{ ...userMessage, content: 'Hi' }, { ...reply, content: 'OK', status: 'completed' }];
	await page.goto('/test/assistant?conversation=chat-1');
	const shortButton = page.getByRole('button', { name: 'Copy message' }).nth(1);
	await shortButton.scrollIntoViewIfNeeded();
	const shortBubble = shortButton.locator('xpath=ancestor::*[@data-slot="bubble"]');
	await expect(shortBubble).toHaveCount(1);
	const shortBox = (await shortButton.boundingBox())!;
	const shortBubbleBox = (await shortBubble.boundingBox())!;
	expect(shortBox.x + shortBox.width).toBeLessThanOrEqual(shortBubbleBox.x + shortBubbleBox.width);
	expect(shortBubbleBox.x + shortBubbleBox.width - (shortBox.x + shortBox.width)).toBeLessThanOrEqual(8);
	const shortRight = await firstLineRight(shortBubble.locator('[data-slot=bubble-content]'));
	expect(shortRight).not.toBeNull();
	expect(shortRight!).toBeLessThanOrEqual(shortBox.x);
});

test('assistant reply text can be drag-selected and copied exactly; user text cannot', async ({ page, context }) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	const state = await fixture(page);
	state.messages = [
		{ ...userMessage, content: 'User words that must stay untouched.' },
		{ ...reply, content: copyAnswer, status: 'completed' }
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const selectable = page.locator('[data-selectable="text"]');
	await expect(selectable).toHaveCount(1);
	expect(await selectable.evaluate((el) => getComputedStyle(el).userSelect)).toBe('text');
	const replyParagraph = selectable.locator('p').filter({ hasText: 'Use' }).first();
	expect(await replyParagraph.evaluate((el) => getComputedStyle(el).userSelect)).toBe('text');
	const userParagraph = page
		.locator('[data-slot=bubble-content]')
		.filter({ hasText: 'User words that must stay untouched.' })
		.locator('p');
	expect(await userParagraph.evaluate((el) => getComputedStyle(el).userSelect)).toBe('none');

	const points = await replyParagraph.evaluate((el) => {
		const text = el.textContent ?? '';
		const start = text.indexOf('Use');
		const end = text.indexOf('bold') + 'bold'.length;
		const nodes: Node[] = [];
		const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
		let node = walker.nextNode();
		while (node) {
			nodes.push(node);
			node = walker.nextNode();
		}
		const locate = (offset: number) => {
			let remaining = offset;
			for (const candidate of nodes) {
				const length = candidate.textContent?.length ?? 0;
				if (remaining <= length) return { node: candidate, offset: remaining };
				remaining -= length;
			}
			const last = nodes[nodes.length - 1];
			return { node: last, offset: last.textContent?.length ?? 0 };
		};
		const range = (from: number, to: number) => {
			const value = document.createRange();
			const a = locate(from);
			const b = locate(to);
			value.setStart(a.node, a.offset);
			value.setEnd(b.node, b.offset);
			return value;
		};
		const first = range(start, start + 3).getClientRects()[0];
		const lastRects = range(end - 4, end).getClientRects();
		const last = lastRects[lastRects.length - 1];
		return {
			from: { x: first.left + 1, y: first.top + first.height / 2 },
			to: { x: last.right - 1, y: last.top + last.height / 2 }
		};
	});
	await page.mouse.move(points.from.x, points.from.y);
	await page.mouse.down();
	await page.mouse.move(points.to.x, points.to.y, { steps: 8 });
	await page.mouse.up();
	await expect.poll(() => page.evaluate(() => window.getSelection()?.toString() ?? '')).toBe('Use bold');
	await page.keyboard.press('ControlOrMeta+c');
	await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('Use bold');

	// The user's plain text is not selectable, as before.
	await page.evaluate(() => window.getSelection()?.removeAllRanges());
	const userBox = (await userParagraph.boundingBox())!;
	await page.mouse.move(userBox.x + 2, userBox.y + userBox.height / 2);
	await page.mouse.down();
	await page.mouse.move(userBox.x + userBox.width - 2, userBox.y + userBox.height / 2, { steps: 8 });
	await page.mouse.up();
	await expect.poll(() => page.evaluate(() => window.getSelection()?.toString() ?? '')).toBe('');
});

test('bulk issue changes share one approval and report individual outcomes', async ({ page }) => {
	const state = await fixture(page);
	const changes = [
		{ identifier: 'ENG-1', status: 'done', priority: 2 },
		{ identifier: 'ENG-2', label_ids: [], cycle_id: '' },
		{ identifier: 'ENG-3', assignee_id: '', project_id: '' }
	];
	machineApproval(state, 'bulk_update_issues', { changes });
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByText('Update issues (3)', { exact: true })).toBeVisible();
	const rows = page.getByRole('listitem').filter({ hasText: /ENG-[123]/ });
	await expect(rows).toHaveCount(3);
	await expect(rows.nth(0)).toContainText('Status: Done · Priority: High');
	await expect(rows.nth(1)).toContainText('Labels: None · Cycle: None');
	await expect(rows.nth(2)).toContainText('Assignee: None · Project: None');
	const approve = page.getByRole('button', { name: 'Approve', exact: true });
	await expect(approve).toHaveCount(1);
	const results = [
		{ identifier: 'ENG-1', success: true },
		{ identifier: 'ENG-2', success: false, error: 'issue not found' },
		{ identifier: 'ENG-3', success: true }
	];
	await page.route('**/conversations/chat-1/approvals/approval-30', async (route) => {
		state.decisions.push(route.request().postDataJSON().decision);
		await route.fulfill({ json: {
			...state.approvals[0], status: 'failed', error: '1 of 3 issue updates failed',
			payload: { changes, results }
		} });
	});
	await approve.focus();
	await page.keyboard.press('Enter');
	await expect(page.getByText('Update issues (3) · 1 of 3 issue updates failed')).toBeVisible();
	await expect(rows.nth(1)).toContainText('issue not found');
	await expect(rows.nth(2)).toContainText('Updated');
	expect(state.decisions).toEqual(['approve']);
});

test('one approval lists all 25 proposed issue updates', async ({ page }) => {
	const state = await fixture(page);
	const project = 'f6a0c94c-990f-48d0-b0a7-0ed9b4ad6593';
	const changes = Array.from({ length: 25 }, (_, i) => ({
		identifier: `ENG-${i + 1}`, status: 'done', project_id: project
	}));
	machineApproval(state, 'bulk_update_issues', { changes });
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByTestId('approval-card').getByRole('button', { expanded: false }).click();
	await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(1);
	const rows = page.getByRole('listitem').filter({ hasText: /ENG-\d+/ });
	await expect(rows).toHaveCount(25);
	await expect(rows.last()).toHaveText(`ENG-25 Status: Done · Project: ${project}`);
	await rows.last().scrollIntoViewIfNeeded();
	expect(await rows.last().evaluate((row) => row.scrollWidth <= row.clientWidth)).toBe(true);
});

test('pending approvals stay above the composer in creation order with keyboard disclosure', async ({ page }, testInfo) => {
	const title = 'Track delivery for WhatsApp Business messages';
	const state = await pendingDelegation(page, {
		title, instructions: '## Delivery status\n\nPersist **provider message IDs**.',
		repository: 'acme/notifications', base_branch: 'main', workflow_id: 'standard', workflow: 'Standard'
	});
	const first = state.approvals[0];
	await page.route('**/api/workspaces/test/workflows', route => route.fulfill({
		json: { ...workflowList, defaults: [{ workflow_id: 'workflow-loop' }] }
	}));
	state.approvals.unshift({
		...first, id: 'approval-2', tool_call_id: 'tool-10', created_at: '2026-11-01T10:00:00Z',
		action: 'message_task', payload: { identifier: 'ENG-2', title: 'Update the delivery report', body: 'Include delayed messages.' }
	});
	await page.setViewportSize({width: 1440, height: 960});
	await page.goto('/test/assistant?conversation=chat-1');
	const stack = page.getByTestId('approval-stack');
	const cards = stack.getByRole('article');
	await expect(cards).toHaveCount(2);
	await expect(cards.first()).toContainText(title);
	await expect(cards.last()).toContainText('Update the delivery report');
	const toggle = cards.first().getByRole('button', { expanded: false });
	await toggle.focus();
	await page.keyboard.press('Enter');
	await expect(cards.first().getByRole('heading', { name: 'Delivery status' })).toBeVisible();
	await expect(cards.first().locator('strong')).toHaveText('provider message IDs');
	await expect(cards.first().getByRole('combobox', { name: 'Model', exact: true })).toBeVisible();
	await expect(cards.first().getByTestId('task-workflow')).toHaveText('Standard');
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('approvals-expanded.png') });
	await page.keyboard.press('Space');
	await expect(toggle).toBeFocused();
	await expect(cards.first().getByRole('heading', { name: 'Delivery status' })).toBeHidden();
	await expect(stack).toBeInViewport();
	expect(await stack.evaluate(el => !!el.closest('[data-slot="message-scroller"]'))).toBe(false);
	const form = page.getByRole('form');
	await expect(form.getByRole('textbox')).toBeEnabled();
	await expect(form.locator('[data-slot="input-group"]')).toHaveCSS('opacity', '1');
	expect((await stack.boundingBox())!.y + (await stack.boundingBox())!.height).toBeLessThanOrEqual((await form.boundingBox())!.y + 1);
	await page.emulateMedia({ reducedMotion: 'reduce' });
	await page.setViewportSize({ width: 390, height: 844 });
	await expect(page.getByTestId('assistant-sidebar')).toHaveCount(0);
	await expect(cards.last()).toBeInViewport();
	await page.evaluate(() => {
		document.documentElement.classList.add('dark');
		document.documentElement.setAttribute('data-theme', 'dark');
	});
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('approvals-phone.png') });
	await toggle.click();
	const card = cards.first();
	const heading = (await card.getByRole('button', { name: new RegExp(title), expanded: true }).boundingBox())!;
	const decision = (await card.getByRole('button', { name: 'Approve' }).boundingBox())!;
	expect(heading.y + heading.height).toBeLessThan(decision.y);
	expect(Math.abs(heading.x + heading.width - decision.x - decision.width)).toBeLessThan(2);
	const fields = [
		card.getByTestId('task-workflow'),
		card.getByRole('button', { name: 'Model server' }),
		card.getByRole('combobox', { name: 'Model', exact: true })
	];
	const bounds = await Promise.all(fields.map(field => field.boundingBox()));
	expect(Math.max(...bounds.map(b => b!.y)) - Math.min(...bounds.map(b => b!.y))).toBeLessThan(2);
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('approvals-controls-phone.png') });
	await fields[0].click();
	await page.getByRole('option', { name: 'Issue default' }).click();
	const note = card.getByText("A pipeline's writer role decides the coding agent and model.");
	await expect(note).toBeVisible();
	expect((await note.boundingBox())!.width).toBeGreaterThan(decision.x + decision.width - heading.x - 2);
	await cards.first().getByRole('button', { name: 'Deny' }).click();
	await expect(cards).toHaveCount(1);
	await expect(cards.first()).toContainText('Update the delivery report');
});

test('a live approval keeps the reader’s scroll position and typing focus', async ({ page }) => {
	const state = await pendingDelegation(page, {
		instructions: ['## Implementation', ...Array.from({ length: 25 }, (_, index) => `- Verify case ${index + 1}`), '', 'Persist delivery tracking IDs.'].join('\n')
	});
	const [approval] = state.approvals;
	state.approvals = [];
	state.messages = longTranscript(48);
	await page.goto('/test/assistant?conversation=chat-1');
	const viewport = page.locator('[data-slot="message-scroller-viewport"]');
	const composer = page.getByRole('textbox', { name: 'Message', exact: true });
	await expect(page.locator('[data-slot="message"]')).toHaveCount(48);
	await expect.poll(() => atLiveEdge(viewport)).toBe(true);
	await composer.fill('Draft in progress');
	await viewport.hover();
	await page.mouse.wheel(0, -1500);
	await expect.poll(() => atLiveEdge(viewport)).toBe(false);
	const position = await viewport.evaluate(el => el.scrollTop);
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	state.approvals = [approval];
	state.sockets.at(-1)!.send(JSON.stringify({
		id: 'pending', cursor: 1, conversation_id: 'chat-1', type: 'approval.requested', payload: approval
	}));
	const card = page.getByTestId('approval-card');
	await expect(card).toBeInViewport();
	await expect(card).toContainText(approval.message_model);
	await expect(composer).toBeFocused();
	await expect(composer).toHaveValue('Draft in progress');
	await card.getByRole('button', { expanded: false }).click();
	const details = card.getByTestId('approval-details');
	await expect(details.getByRole('heading', { name: 'Implementation' })).toBeVisible();
	const end = details.getByText('Persist delivery tracking IDs.', { exact: true });
	await end.scrollIntoViewIfNeeded();
	await expect(end).toBeInViewport();
	expect(await details.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
	expect(await viewport.evaluate(el => el.scrollTop)).toBe(position);
	await card.getByRole('button', { name: 'Deny', exact: true }).click();
	await expect(card).toHaveCount(0);
	await expect(composer).toBeFocused();
	expect(await viewport.evaluate(el => el.scrollTop)).toBe(position);
});

test('scheduled job card follows registration, firing and cancellation', async ({ page }) => {
	const state = await fixture(page);
	state.appendSends = true;
	await page.goto('/test/assistant?conversation=chat-1');
	const composer = page.getByRole('textbox', { name: 'Message' });
	await composer.fill('Remind me to check the release notes.');
	await composer.press('Enter');
	await expect.poll(() => state.sends).toEqual(['Remind me to check the release notes.']);
	await expect.poll(() => state.sockets.length).toBeGreaterThanOrEqual(2);
	const job = {
		id: 'job-1',
		conversation_id: conversation.id,
		parent_id: state.messages[0].id,
		state: 'scheduled',
		timezone: 'UTC',
		instructions: 'Check the release notes',
		next_run_at: '2026-11-27T09:00:00Z',
		last_outcome: 'Registered'
	};
	const sendJob = (payload: typeof job) => {
		state.sockets
			.at(-1)!
			.send(
				JSON.stringify({
					id: 'job-event-' + ++state.cursor,
					cursor: state.cursor,
					conversation_id: conversation.id,
					type: 'scheduled_job.updated',
					payload
				})
			);
	};
	sendJob(job);
	const card = page.getByRole('region', { name: 'Scheduled job' });
	await expect(card.getByText('Registered')).toBeVisible();
	await expect(card.getByText(/Next run/)).toBeVisible();
	sendJob({ ...job, state: 'completed', last_outcome: 'Reminder sent' });
	await expect(card.getByText('Reminder sent')).toBeVisible();
	state.sockets
		.at(-1)!
		.send(
			JSON.stringify({
				id: 'notice',
				cursor: ++state.cursor,
				conversation_id: conversation.id,
				type: 'message.created',
				payload: {
					...reply,
					id: 'notice-1',
					sequence: 3,
					parent_id: job.parent_id,
					status: 'completed',
					content: 'Check the release notes'
				}
			})
		);
	await expect(page.getByText('Check the release notes', { exact: true })).toHaveCount(2);
	await expect(card.getByRole('button')).toHaveCount(0);
	const recurring = { ...job, id: 'job-2', instructions: 'Check again tomorrow' };
	sendJob(recurring);
	const current = card.filter({ hasText: recurring.instructions });
	await page.route('**/conversations/chat-1/jobs/job-2/cancel', route =>
		route.fulfill({ json: { ...recurring, state: 'cancelled', last_outcome: 'Cancelled' } })
	);
	const cancel = current.getByRole('button', { name: 'Cancel' });
	await cancel.focus();
	await cancel.press('Enter');
	await expect(current.getByText('Cancelled')).toBeVisible();
	// The cancel response can arrive before a queued poll event on the socket.
	sendJob(recurring);
	sendJob({ ...job, id: 'job-3', instructions: 'Another reminder' });
	await expect(card.getByText('Another reminder')).toBeVisible();
	await expect(current.getByRole('button')).toHaveCount(0);
});
