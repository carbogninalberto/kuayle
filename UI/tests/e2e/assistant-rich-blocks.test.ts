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

const chartSpec = {
	type: 'bar',
	title: 'Issues this quarter',
	unit: 'issues',
	categories: ['Q1', 'Q2', 'Q3', 'Q4'],
	series: [{ name: 'Created', data: [4, 7, 3, 9] }]
};
const chartFence = `\`\`\`chart\n${JSON.stringify(chartSpec)}\n\`\`\``;
const htmlFence = `\`\`\`html\n<script>document.body.dataset.ran = '1'</script>\n\`\`\``;

async function fixture(page: Page) {
	const state = {
		messages: [] as (typeof userMessage)[],
		tools: [] as any[],
		approvals: [] as any[],
		queue: [] as any[],
		cursor: 0,
		sockets: [] as WebSocketRoute[]
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
		if (path.endsWith('/conversations'))
			return route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation] });
		if (path.includes('/conversations/chat-1')) {
			if (path.endsWith('/events')) return route.fulfill({ json: [] });
			const before = Number(url.searchParams.get('before') ?? 0);
			const messages = state.messages.filter((message) => !before || message.sequence < before).slice(-50);
			return route.fulfill({
				json: {
					conversation,
					messages,
					tools: state.tools,
					approvals: state.approvals,
					cursor: state.cursor,
					has_more: false
				}
			});
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

async function showReply(page: Page, content: string, status = 'completed') {
	const state = await fixture(page);
	state.messages = [userMessage, { ...reply, content, status }];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Hello assistant', { exact: true })).toBeVisible();
	return state;
}

test('renders a chart fence as a themed canvas instead of code', async ({ page }) => {
	await showReply(page, `Quarterly summary\n\n${chartFence}\n\nDone.`);
	const chart = page.locator('.assistant-markdown [role="img"]');
	await expect(chart).toHaveAttribute('aria-label', /Issues this quarter/);
	const canvas = chart.locator('canvas');
	await expect(canvas).toHaveCount(1);
	const box = await canvas.boundingBox();
	expect(box?.width ?? 0).toBeGreaterThan(100);
	expect(box?.height ?? 0).toBeGreaterThan(100);
	await expect(page.locator('.assistant-markdown pre').filter({ hasText: '"series"' })).toHaveCount(0);
});

test('runs an html widget in a sandbox with scripts but no same-origin or network', async ({ page }) => {
	const fetched: string[] = [];
	page.on('request', (request) => {
		if (request.url().startsWith('https://untrusted.example/')) fetched.push(request.url());
	});
	await page.route('https://untrusted.example/**', (route) => route.fulfill({ status: 200, body: 'ok', contentType: 'text/html' }));
	const widget = `<button>Run</button><script>document.body.dataset.ran = '1'; fetch('https://untrusted.example/x').catch(() => {});</script>`;
	await showReply(page, `Widget:\n\n\`\`\`html\n${widget}\n\`\`\``);

	const frame = page.locator('iframe');
	await expect(frame).toHaveCount(1);
	await expect(frame).toHaveAttribute('sandbox', 'allow-scripts');
	await expect(frame).not.toHaveAttribute('sandbox', /allow-same-origin/);
	await expect(page.frameLocator('iframe').locator('body')).toHaveAttribute('data-ran', '1');
	expect(await frame.evaluate((element) => (element as HTMLIFrameElement).contentDocument === null)).toBe(true);
	expect(fetched).toEqual([]);
});

test('tears down a widget that navigates its own frame', async ({ page }) => {
	await page.route('https://untrusted.example/**', (route) =>
		route.fulfill({ status: 200, body: '<!doctype html><title>Gone</title>', contentType: 'text/html' })
	);
	const widget = `<button onclick="location.href = 'https://untrusted.example/'">Leave</button>`;
	await showReply(page, `Widget:\n\n\`\`\`html\n${widget}\n\`\`\``);

	await page.frameLocator('iframe').getByRole('button', { name: 'Leave' }).click();
	await expect(page.locator('iframe')).toHaveCount(0);
	await expect(page.getByText("Couldn't render this widget; showing its source.")).toBeVisible();
	await expect(page.locator('.assistant-markdown pre code')).toContainText('Leave');
});

test('malformed and oversized blocks fall back to visible code text', async ({ page }) => {
	const malformed = '```chart\n{"type":"bar","categories":["Q1"],"series":[{"name":"Created","data":[1,2]}]}\n```';
	const oversizedChart = `\`\`\`chart\n${JSON.stringify({
		...chartSpec,
		categories: Array.from({ length: 100 }, (_, index) => `${index}-${'x'.repeat(180)}`),
		series: [{ name: 'Created', data: Array(100).fill(1) }]
	})}\n\`\`\``;
	const oversizedHtml = `\`\`\`html\n<p>${'x'.repeat(70000)}</p>\n\`\`\``;
	await showReply(page, `Before\n\n${malformed}\n\n${oversizedChart}\n\n${oversizedHtml}\n\nAfter`);

	await expect(page.locator('iframe')).toHaveCount(0);
	await expect(page.locator('.assistant-markdown canvas')).toHaveCount(0);
	await expect(page.locator('.assistant-markdown pre')).toHaveCount(3);
	await expect(page.locator('.assistant-markdown pre').first()).toContainText('"categories"');
	await expect(page.getByText('After', { exact: true })).toBeVisible();
});

test('a streaming reply keeps a chart fence as code', async ({ page }) => {
	await showReply(page, `Quarterly summary\n\n${chartFence}`, 'streaming');
	await expect(page.locator('.assistant-markdown canvas')).toHaveCount(0);
	await expect(page.locator('.assistant-markdown pre code')).toContainText('"series"');
});

test('copying a message leaves out chart JSON and iframe markup', async ({ page, context }) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	const content = `Quarterly summary\n\n${chartFence}\n\nWidget:\n\n${htmlFence}\n\nDone.`;
	await showReply(page, content);
	const clipboard = () => page.evaluate(() => navigator.clipboard.readText());
	await page.getByRole('button', { name: 'Copy message' }).nth(1).click();
	await expect.poll(clipboard).toBe('Quarterly summary\n\nWidget:\n\nDone.');
	const copied = await clipboard();
	expect(copied).not.toContain('"categories"');
	expect(copied).not.toContain('<script');
	expect(copied).not.toContain('<iframe');
	expect(copied).not.toContain('<html');
});
