import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

async function fixture(page: Page) {
	const time = '2026-10-26T10:00:00Z';
	const conversation = {
		id: 'chat-1',
		workspace_id: 'workspace-1',
		creator_id: 'user-1',
		issue_id: 'issue-1',
		team_id: 'team-1',
		title: 'Implement search',
		created_at: time,
		updated_at: time
	};
	const issue = {
		id: 'issue-1',
		identifier: 'ENG-1',
		title: 'Implement search',
		status: 'todo',
		description: 'Search issues'
	};
	const state = {
		task: null as any,
		creates: [] as any[],
		retries: [] as any[],
		cancels: 0,
		sockets: [] as WebSocketRoute[],
		events: [] as any[],
		denied: false,
		failAfterCreate: false,
		logs: [] as any[]
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', (socket) => state.sockets.push(socket));
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const req = route.request(),
			url = new URL(req.url()),
			path = url.pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (state.denied && (path.includes('/conversations/') || path.includes('/agent-tasks/')))
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		if (path.endsWith('/task-options'))
			return route.fulfill({
				json: {
					providers: [{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false }],
					model_provider: {
						provider: 'deepseek',
						configured: true,
						default_model: 'deepseek-flash',
						thinking: 'high',
						models: ['deepseek-flash', 'deepseek-v4-pro'],
						thinking_levels: ['off', 'low', 'high', 'max']
					}
				}
			});
		if (path.endsWith('/tasks')) {
			if (req.method() === 'POST') {
				const input = req.postDataJSON();
				state.creates.push(input);
				state.task = {
					...input,
					id: 'task-1',
					workspace_id: 'workspace-1',
					conversation_id: 'chat-1',
					creator_id: 'user-1',
					issue_id: 'issue-1',
					team_id: 'team-1',
					state: 'queued',
					reason: 'Waiting for execution',
					generation: 1,
					failure_count: 0,
					checkout_id: 'checkout-1',
					run_id: 'run-1',
					created_at: time,
					updated_at: time
				};
				if (state.failAfterCreate) {
					state.failAfterCreate = false;
					return route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } });
				}
				return route.fulfill({ status: 202, json: state.task });
			}
			return route.fulfill({ json: { tasks: state.task ? [state.task] : [] } });
		}
		if (path.endsWith('/events'))
			return route.fulfill({ json: state.events.filter((e) => e.cursor > Number(url.searchParams.get('after') ?? 0)) });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], cursor: 0, has_more: false } });
		if (path.includes('/agent-tasks/task-1')) {
			if (path.endsWith('/cancel')) {
				state.cancels++;
				state.task = { ...state.task, state: 'cancelling', reason: 'Stopping native execution' };
				return route.fulfill({ status: 204 });
			}
			if (path.endsWith('/retry')) {
				state.retries.push(req.postDataJSON());
				state.task = {
					...state.task,
					state: 'queued',
					generation: 2,
					run_id: 'run-2',
					prompt: req.postDataJSON().prompt
				};
				return route.fulfill({ status: 202, json: state.task });
			}
			if (path.endsWith('/history'))
				return route.fulfill({
					json: {
						transitions: [
							{
								sequence: 1,
								task_id: 'task-1',
								from_state: '',
								to_state: 'queued',
								reason: 'Waiting for execution',
								created_at: time
							}
						]
					}
				});
			if (path.endsWith('/runs'))
				return route.fulfill({
					json: {
						runs: [{ id: 'run-1', provider_id: 'codex', status: 'running', machine_id: 'machine-1', created_at: time }]
					}
				});
			if (path.endsWith('/logs')) {
				const logs = state.logs.filter((log) => log.id > Number(url.searchParams.get('after') ?? 0));
				return route.fulfill({
					json: { logs: logs.slice(0, 100), ...(logs.length > 100 ? { next_after: logs[99].id } : {}) }
				});
			}

			return route.fulfill({ json: state.task });
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
	const emit = (type: string, payload: any) => {
		const event = {
			id: crypto.randomUUID(),
			conversation_id: 'chat-1',
			cursor: state.events.length + 1,
			type,
			payload
		};
		state.events.push(event);
		state.sockets.at(-1)!.send(JSON.stringify(event));
		return event;
	};
	return { state, emit };
}

test('delegates from an issue conversation and shows real execution updates', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Start coding', exact: true });
	await dialog.getByRole('textbox', { name: 'Task instructions' }).fill('Implement search and add regression coverage');
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect.poll(() => state.creates.length).toBe(1);
	expect(state.creates[0]).toMatchObject({
		provider: 'codex',
		model: 'deepseek-flash',
		thinking: 'high',
		prompt: 'Implement search and add regression coverage'
	});
	await expect(page.getByText('Queued', { exact: true })).toBeVisible();
	state.task = { ...state.task, state: 'running', reason: 'Native coding harness started', machine_id: 'machine-1' };
	const event = emit('task.state_changed', state.task);
	await expect(page.getByText('Running', { exact: true })).toBeVisible();
	state.sockets.at(-1)!.send(JSON.stringify(event));
	await expect(page.getByText('Implement search and add regression coverage', { exact: true })).toHaveCount(1);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	state.logs = [
		{
			id: 1,
			agent_run_id: 'run-1',
			stream: 'stdout',
			sequence: 1,
			content: 'Checking search query authorization\n',
			truncated: false
		}
	];
	emit('task.activity', {
		task_id: 'task-1',
		run_id: 'run-1',
		log_id: 1,
		stream: 'stdout',
		truncated: false,
		events: []
	});
	await expect(page.getByText('Checking search query authorization', { exact: false })).toBeVisible();
	await page.reload();
	await expect(page.getByText('Running', { exact: true })).toBeVisible();
	expect(state.creates).toHaveLength(1);
	await page.getByRole('button', { name: 'Cancel task', exact: true }).click();
	await expect.poll(() => state.cancels).toBe(1);
	await expect(page.getByText('Cancelling', { exact: true })).toBeVisible();
});

async function delegate(page: Page, prompt = 'Implement and test issue search') {
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Start coding', exact: true });
	await dialog.getByRole('textbox', { name: 'Task instructions' }).fill(prompt);
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	return dialog;
}

test('replays task failure, retries the same task and clears revoked task content', async ({ page }) => {
	const { state } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await expect(page.getByText('Queued', { exact: true })).toBeVisible();
	const sockets = state.sockets.length;
	state.sockets.at(-1)!.close({ code: 1012, reason: 'Worker restart' });
	state.task = { ...state.task, state: 'failed', reason: 'Execution failed; runtime cleanup confirmed' };
	state.events.push({
		id: 'missed-failure',
		cursor: 1,
		conversation_id: 'chat-1',
		type: 'task.state_changed',
		payload: state.task
	});
	await expect.poll(() => state.sockets.length).toBeGreaterThan(sockets);
	await expect(page.getByText('Failed', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Retry task', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Retry task', exact: true });
	await dialog.getByRole('textbox', { name: 'Task instructions' }).fill('Address the failed authorization check');
	await dialog.getByRole('button', { name: 'Retry task', exact: true }).click();
	await expect.poll(() => state.retries.length).toBe(1);
	expect(state.retries[0]).toMatchObject({ generation: 1, prompt: 'Address the failed authorization check' });
	await expect(page.getByText('Attempt 2', { exact: false })).toBeVisible();
	expect(state.creates).toHaveLength(1);
	state.denied = true;
	state.sockets.at(-1)!.close({ code: 1008, reason: 'Revoked' });
	await expect(page.getByText('This conversation is no longer available.')).toBeVisible();
	await expect(page.getByText('Address the failed authorization check', { exact: true })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Start coding', exact: true })).toHaveCount(0);
});

test('retains delegation identity after a lost response and refresh', async ({ page }) => {
	const { state } = await fixture(page);
	state.failAfterCreate = true;
	await page.goto('/test/assistant?conversation=chat-1');
	const dialog = await delegate(page, 'Make search accessible');
	await expect(dialog.getByRole('alert')).toBeVisible();
	const requestID = state.creates[0].request_id;
	await page.reload();
	await expect(page.getByText('Queued', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	const recovered = page.getByRole('dialog', { name: 'Start coding', exact: true });
	await expect(recovered.getByRole('textbox', { name: 'Task instructions' })).toHaveValue('Make search accessible');
	await recovered.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect.poll(() => state.creates.length).toBe(2);
	expect(state.creates[1].request_id).toBe(requestID);
	await expect(page.getByRole('article', { name: 'Coding task', exact: true })).toHaveCount(1);
});

test('mobile task controls and bounded output stay within the viewport in both themes', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	await page.setViewportSize({ width: 375, height: 812 });
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await expect(page.getByRole('heading', { name: 'What are you working on?' })).toHaveCount(0);
	state.logs = Array.from({ length: 105 }, (_, i) => ({
		id: i + 1,
		agent_run_id: 'run-1',
		stream: 'stdout',
		sequence: i + 1,
		content: `Log entry ${i + 1} ${'x'.repeat(100)}\n`,
		truncated: false
	}));
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(page.getByRole('button', { name: 'Load more output', exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Load more output', exact: true }).click();
	await expect(page.getByText('Log entry 105', { exact: false })).toBeVisible();
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
		await page.screenshot({
			path: testInfo.outputPath(`task-mobile-${dark ? 'dark' : 'light'}.png`),
			fullPage: true
		});
	}
});

test('keeps displaying live output while activity arrives continuously', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await expect(page.getByText('Queued', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(page.getByText('No output received yet.', { exact: true })).toBeVisible();
	let sequence = 0;
	const timer = setInterval(() => {
		sequence++;
		state.logs.push({
			id: sequence,
			agent_run_id: 'run-1',
			stream: 'stdout',
			sequence,
			content: `Live progress ${sequence}\n`,
			truncated: false
		});
		emit('task.activity', {
			task_id: 'task-1',
			run_id: 'run-1',
			log_id: sequence,
			stream: 'stdout',
			truncated: false,
			events: []
		});
	}, 40);
	try {
		await expect(page.getByText('Live progress 1', { exact: false })).toBeVisible({ timeout: 2000 });
		expect(sequence).toBeGreaterThan(0);
	} finally {
		clearInterval(timer);
	}
});
