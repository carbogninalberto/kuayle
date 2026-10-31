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
		validation: null as any,
		manualChecks: [] as any[],
		checkRuns: [] as any[],
		checkError: null as any,
		artifacts: [] as any[],
		publication: { step: null } as any,
		publications: 0,
		signals: [] as any[],
		changedFiles: [] as string[],
		diff: '',
		ports: [] as any[],
		portLaunches: [] as string[],
		appStarts: 0,
		appStops: 0,
		appStartError: null as any,
		creates: [] as any[],
		retries: [] as any[],
		accepts: [] as any[],
		acceptError: null as any,
		cancels: 0,
		sockets: [] as WebSocketRoute[],
		events: [] as any[],
		denied: false,
		failAfterCreate: false,
		logs: [] as any[],
		messages: [] as any[],
		missingRun: false,
		signIns: [] as any[],
		signInFound: false,
		tokenSaves: [] as any[],
		sessions: [] as any[],
		agentMessages: [] as any[],
		liveMessages: [] as any[],
		liveSends: [] as string[],
		chatAgentMessages: [] as any[],
		messageQueries: [] as string[],
		messageActions: [] as string[],
		providers: [{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false }] as any[]
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
		if (path === '/api/workspaces/test/agent-sign-ins/claude-code/token' && req.method() === 'PUT') {
			const { token } = req.postDataJSON();
			state.tokenSaves.push(token);
			// The real API answers with the status only; the continue call then
			// starts the attempt with the saved token.
			state.signInFound = true;
			return route.fulfill({
				json: {
					provider: 'claude-code',
					supports_token: true,
					token: { saved_at: time, expires_at: '2027-10-26T10:00:00Z', expired: false },
					terminal: null
				}
			});
		}
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (state.denied && (path.includes('/conversations/') || path.includes('/agent-tasks/')))
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		if (path.endsWith('/task-options'))
			return route.fulfill({
				json: {
					providers: state.providers,
					model_servers: [
						{
							id: 'server-1',
							name: 'DeepSeek',
							models: ['deepseek-flash', 'deepseek-v4-pro'],
							default_model: 'deepseek-flash',
							fast_model: '',
							thinking: 'high',
							is_default: true,
							harnesses: ['codex', 'claude-code'],
							thinking_levels: ['off', 'low', 'high', 'max'],
							updated_at: time
						}
					]
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
			return route.fulfill({
				json: {
					conversation,
					issue,
					messages: state.messages,
					agent_messages: state.chatAgentMessages,
					cursor: 0,
					has_more: false
				}
			});
		if (path.includes('/agent-tasks/task-1')) {
			if (path.endsWith('/live-messages')) {
				if (req.method() === 'POST') {
					const { body } = req.postDataJSON();
					state.liveSends.push(body);
					const sent = {
						id: `live-${state.liveMessages.length + 1}`,
						task_id: 'task-1',
						direction: 'to_agent',
						seq: state.liveMessages.length + 1,
						sender: 'user',
						author_id: 'user-1',
						kind: 'message',
						body,
						state: 'queued',
						delivered_via: null,
						run_id: null,
						reject_reason: null,
						created_at: time,
						delivered_at: null
					};
					state.liveMessages = [...state.liveMessages, sent];
					return route.fulfill({ status: 201, json: sent });
				}
				return route.fulfill({ json: { messages: state.liveMessages } });
			}
			if (path.endsWith('/preview') && req.method() === 'POST') {
				state.appStarts++;
				if (state.appStartError) return route.fulfill(state.appStartError);
				state.task = { ...state.task, preview: { state: 'starting', port_id: null, port: null, error: null } };
				return route.fulfill({ status: 202, json: {} });
			}
			if (path.endsWith('/preview') && req.method() === 'DELETE') {
				state.appStops++;
				state.ports = state.ports.filter((p) => p.agent_task_id !== 'task-1' || p.source !== 'task');
				state.task = { ...state.task, preview: { state: 'stopped', port_id: null, port: null, error: null } };
				return route.fulfill({ status: 202, json: {} });
			}
			if (path.endsWith('/agent-tasks/task-1/checks') && req.method() === 'POST') {
				state.checkRuns.push(req.postDataJSON());
				if (state.checkError) return route.fulfill(state.checkError);
				const step = {
					id: `check-${state.manualChecks.length + 1}`,
					agent_run_id: state.task.run_id,
					step_type: 'manual_check',
					status: 'queued',
					command_argv: ['npm', 'test'],
					summary: 'Waiting for the check to start'
				};
				state.manualChecks = [...state.manualChecks, step];
				return route.fulfill({ status: 202, json: step });
			}
			if (path.endsWith('/runs/run-1/checks')) return route.fulfill({ json: state.manualChecks });
			if (state.missingRun && path.includes('/runs/'))
				return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (path.endsWith('/cancel')) {
				state.cancels++;
				state.task = { ...state.task, state: 'cancelling', reason: 'Stopping native execution' };
				return route.fulfill({ status: 204 });
			}
			if (path.endsWith('/sign-in')) {
				state.signIns.push(req.postDataJSON());
				if (!state.signInFound) return route.fulfill({ status: 409, json: { error: { code: 'SIGN_IN_NOT_FOUND' } } });
				state.task = { ...state.task, state: 'queued', generation: 2, run_id: 'run-2', sign_in_required: false };
				return route.fulfill({ status: 202, json: state.task });
			}
			if (path.endsWith('/accept')) {
				state.accepts.push(req.postDataJSON());
				if (state.acceptError) return route.fulfill(state.acceptError);
				state.task = { ...state.task, state: 'completed', reason: 'Accepted in Kuayle by Owner' };
				return route.fulfill({ json: state.task });
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
						runs: [
							{
								id: 'run-1',
								provider_id: 'codex',
								status: 'running',
								machine_id: 'machine-1',
								created_at: time,
								changed_files: state.changedFiles
							}
						]
					}
				});
			if (path.endsWith('/runs/run-1/artifacts')) return route.fulfill({ json: state.artifacts });
			if (path.endsWith('/runs/run-1/artifacts/artifact-1'))
				return route.fulfill({
					body: '<html><script>window.evil=true</script></html>',
					headers: { 'content-type': 'text/html', 'content-disposition': 'attachment; filename="index.html"' }
				});
			if (path.endsWith('/runs/run-1/diff')) return route.fulfill({ json: { diff: state.diff, truncated: false } });
			if (path.endsWith('/validation')) return route.fulfill({ json: state.validation });
			if (path.endsWith('/signals')) return route.fulfill({ json: state.signals });
			if (path.endsWith('/sessions')) return route.fulfill({ json: { sessions: state.sessions } });

			if (path.endsWith('/publication')) {
				state.publications++;
				return route.fulfill({ json: state.publication });
			}
			if (path.endsWith('/logs')) {
				const logs = state.logs.filter((log) => log.id > Number(url.searchParams.get('after') ?? 0));
				return route.fulfill({
					json: { logs: logs.slice(0, 100), ...(logs.length > 100 ? { next_after: logs[99].id } : {}) }
				});
			}

			return route.fulfill({ json: state.task });
		}
		if (path.endsWith('/dev-machines/machine-1/ports'))
			return route.fulfill({ json: { ports: state.ports, listening: [], scanned_at: time } });
		const launch = path.match(/\/dev-machines\/machine-1\/ports\/([^/]+)\/launch$/);
		if (launch) {
			state.portLaunches.push(launch[1]);
			return route.fulfill({
				status: 201,
				json: { status: 'ready', launch_url: `https://machine-${launch[1]}.machines.example.test/?ticket=redacted` }
			});
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/agent-messages') {
			state.messageQueries.push(url.search);
			return route.fulfill({ json: { messages: state.agentMessages } });
		}
		const messageAction = path.match(/\/agent-messages\/([^/]+)\/(hold|release|drop)$/);
		if (messageAction) {
			state.messageActions.push(`${messageAction[1]}:${messageAction[2]}`);
			const next = { hold: 'held', release: 'queued', drop: 'dropped' }[messageAction[2]];
			state.agentMessages = state.agentMessages.map((item) =>
				item.id === messageAction[1] ? { ...item, state: next } : item
			);
			return route.fulfill({ json: state.agentMessages.find((item) => item.id === messageAction[1]) });
		}
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
	// The harness is chosen from logo tiles; the default server fits Codex.
	await expect(dialog.getByRole('radio', { name: 'Codex' })).toHaveAttribute('aria-checked', 'true');
	await dialog.getByRole('textbox', { name: 'Task instructions' }).fill('Implement search and add regression coverage');
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect.poll(() => state.creates.length).toBe(1);
	expect(state.creates[0]).toMatchObject({
		provider: 'codex',
		model_server_id: 'server-1',
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
	await page.getByRole('button', { name: 'Execution output', exact: true }).click(); // raw output starts collapsed
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
	await page
		.getByRole('region', { name: 'Coding tasks' })
		.getByRole('button', { name: 'More actions', exact: true })
		.click();
	await page.getByRole('menuitem', { name: 'Cancel task', exact: true }).click();
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
	await page.getByRole('button', { name: 'Execution output', exact: true }).click(); // raw output starts collapsed
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
	await page.getByRole('button', { name: 'Execution output', exact: true }).click(); // raw output starts collapsed
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

test('validates test command overrides and preserves them across transport retries', async ({ page }) => {
	const { state } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Start coding', exact: true });
	await dialog.getByRole('textbox', { name: 'Task instructions' }).fill('Implement search');
	const command = dialog.getByRole('textbox', { name: 'Test command' });
	await command.fill('npm test');
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect(dialog.getByRole('alert')).toContainText('JSON array');
	expect(state.creates).toHaveLength(0);
	await command.fill('["npm","test"]');
	state.failAfterCreate = true;
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect.poll(() => state.creates.length).toBe(1);
	const request = state.creates[0];
	await page.reload();
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	await expect(command).toHaveValue('["npm","test"]');
	await dialog.getByRole('button', { name: 'Delegate task', exact: true }).click();
	await expect.poll(() => state.creates.length).toBe(2);
	expect(state.creates[1]).toEqual(request);
	expect(request.test_command).toEqual(['npm', 'test']);
	await expect(dialog).not.toBeVisible();
	state.task = { ...state.task, state: 'blocked', test_command: ['npm', 'test'] };
	await page.reload();
	await page.getByRole('button', { name: 'Retry task', exact: true }).click();
	const retry = page.getByRole('dialog', { name: 'Retry task', exact: true });
	await expect(retry.getByRole('textbox', { name: 'Test command' })).toHaveValue('["npm","test"]');
	await retry.getByRole('textbox', { name: 'Test command' }).fill('[]');
	await retry.getByRole('button', { name: 'Retry task', exact: true }).click();
	await expect.poll(() => state.retries.length).toBe(1);
	expect(state.retries[0].test_command).toEqual([]);
});

test('a skipped check says the change was published as an untested draft', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const checks = page.getByRole('region', { name: 'Independent checks', exact: true });
	state.validation = {
		id: 'validation-1',
		agent_run_id: 'run-1',
		status: 'skipped',
		command_argv: [],
		summary: 'No test command is configured; validation is unavailable'
	};
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await checks.getByRole('button', { name: 'Not validated: no check command is configured' }).click();
	await expect(checks.getByText(/opened as a draft pull request/)).toBeVisible();
});

test('independent checks update live and retain their actual exit status after reload', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const checks = page.getByRole('region', { name: 'Independent checks', exact: true });
	await expect(checks.getByText('No independent check recorded')).toBeVisible();
	state.validation = {
		id: 'validation-1',
		agent_run_id: 'run-1',
		status: 'running',
		command_argv: ['sh', '-c', 'exit 7'],
		started_at: '2026-10-27T10:00:00Z'
	};
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await expect(checks.getByText('Running independent check')).toBeVisible();
	state.validation = { ...state.validation, status: 'failed', exit_code: 7, completed_at: '2026-10-27T10:01:00Z' };
	state.artifacts = [
		{
			id: 'artifact-1',
			step_id: 'validation-1',
			path: 'playwright-report/index.html',
			content_type: 'text/html',
			size: 2048,
			created_at: '2026-10-27T10:01:00Z'
		}
	];
	state.logs = [
		{
			id: 1,
			agent_run_id: 'run-1',
			service_id: 'validation-1',
			stream: 'stdout',
			sequence: 1,
			content: '<script>window.evil=true</script> actual check failed',
			truncated: false
		}
	];
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await checks.getByRole('button', { name: 'Independent check failed' }).click();
	await expect(checks.getByText('Exit code: 7')).toBeVisible();
	await expect(checks.getByText(/actual check failed/)).toBeVisible();
	await expect(page.getByText(/actual check failed/)).toHaveCount(1);
	const report = checks.getByRole('button', { name: /playwright-report\/index\.html/ });
	await expect(report).toContainText('2 KB');
	const pages = page.context().pages().length;
	const [download] = await Promise.all([page.waitForEvent('download'), report.click()]);
	expect(download.suggestedFilename()).toBe('index.html');
	expect(page.context().pages()).toHaveLength(pages);
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await page.reload();
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(checks.getByText('Independent check failed')).toBeVisible();
	expect(state.creates).toHaveLength(1);
});

test('agent-chosen checks show their source, each command and the agent summary as text', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const checks = page.getByRole('region', { name: 'Independent checks', exact: true });
	state.validation = {
		id: 'validation-1',
		agent_run_id: 'run-1',
		status: 'failed',
		command_argv: ['go', 'vet', './...'],
		command_source: 'agent',
		commands: [
			['go', 'vet', './...'],
			['sh', '-c', 'cd BE && go test ./internal/service/...']
		],
		command_results: [{ exit_code: 0 }, { exit_code: 2 }],
		agent_summary: '<img src=x onerror="window.evil=true"> Service tests only; the UI was not run.',
		exit_code: 2,
		completed_at: '2026-10-27T10:01:00Z'
	};
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await checks.getByRole('button', { name: 'Independent check failed' }).click();
	await expect(checks.getByText('Tests chosen by the agent, re-run by Kuayle')).toBeVisible();
	const commands = checks.getByRole('list', { name: 'Commands' });
	await expect(commands.getByRole('listitem')).toHaveCount(2);
	await expect(commands.getByRole('listitem').nth(0)).toContainText('go vet ./...');
	await expect(commands.getByRole('listitem').nth(0)).toContainText('Passed');
	await expect(commands.getByRole('listitem').nth(1)).toContainText('sh -c cd BE && go test ./internal/service/...');
	await expect(commands.getByRole('listitem').nth(1)).toContainText('Failed, exit code 2');
	await expect(checks.getByText("Agent's summary (unverified)")).toBeVisible();
	await expect(checks.getByText(/Service tests only; the UI was not run\./)).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
});

test('a configured check is labelled as configured', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const checks = page.getByRole('region', { name: 'Independent checks', exact: true });
	state.validation = {
		id: 'validation-1',
		agent_run_id: 'run-1',
		status: 'succeeded',
		command_argv: ['npm', 'test'],
		command_source: 'configured',
		exit_code: 0,
		completed_at: '2026-10-27T10:01:00Z'
	};
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await checks.getByRole('button', { name: 'Independent check passed' }).click();
	await expect(checks.getByText('Check configured in the development settings')).toBeVisible();
	await expect(checks.getByText("Agent's summary (unverified)")).toHaveCount(0);
});

test('a rejected Compose stack shows its reason with the failed check', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const checks = page.getByRole('region', { name: 'Independent checks', exact: true });
	state.validation = {
		id: 'validation-1',
		agent_run_id: 'run-1',
		status: 'failed',
		command_argv: ['npm', 'test'],
		compose_file: 'deploy/compose.yaml',
		summary: 'Compose file rejected: service "app": "privileged" is not supported',
		completed_at: '2026-10-27T10:01:00Z'
	};
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.validation });
	await checks.getByRole('button', { name: 'Independent check failed' }).click();
	await expect(checks.getByText('Services from deploy/compose.yaml')).toBeVisible();
	await expect(checks.getByText('Compose file rejected: service "app": "privileged" is not supported')).toBeVisible();
	await expect(checks.getByText(/Exit code/)).toHaveCount(0);
});

test('runs the check from the task card and shows the manual check live, apart from the independent check', async ({
	page
}) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const task = page.getByRole('article', { name: 'Coding task' });
	const run = task.getByRole('group', { name: 'Tools' }).getByRole('button', { name: 'Run check', exact: true });
	await expect(run).toHaveCount(0);
	await awaitReview(state, emit);
	state.task = { ...state.task, machine_id: 'machine-1' };
	emit('task.state_changed', state.task);
	await run.click();
	await expect.poll(() => state.checkRuns).toEqual([{}]);
	const manual = task.getByRole('region', { name: 'Manual check', exact: true });
	await expect(manual.getByText('Manual check queued')).toBeVisible();
	await expect(task.getByRole('region', { name: 'Independent checks', exact: true })).toContainText(
		'No independent check recorded'
	);
	const busy = task.getByRole('group', { name: 'Tools' }).getByRole('button', { name: 'Running manual check' });
	state.manualChecks = [{ ...state.manualChecks[0], status: 'running', started_at: '2026-10-27T10:00:00Z' }];
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.manualChecks[0] });
	await expect(manual.getByText('Running manual check')).toBeVisible();
	await expect(busy).toBeDisabled();
	state.logs = [
		{
			id: 1,
			agent_run_id: 'run-1',
			service_id: 'check-1',
			stream: 'stdout',
			sequence: 1,
			content: '1 failing: search returns hidden issues',
			truncated: false
		}
	];
	state.manualChecks = [
		{ ...state.manualChecks[0], status: 'failed', exit_code: 1, completed_at: '2026-10-27T10:01:00Z' }
	];
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.manualChecks[0] });
	await manual.getByRole('button', { name: 'Manual check failed' }).click();
	await expect(manual.getByText('Exit code: 1')).toBeVisible();
	await expect(manual.getByText(/1 failing: search returns hidden issues/)).toBeVisible();
	await expect(page.getByText(/1 failing: search returns hidden issues/)).toHaveCount(1);
	await expect(run).toBeEnabled();
	expect(state.task.state).toBe('awaiting_review');
	expect(state.retries).toHaveLength(0);

	// A busy machine refuses the check with the server's reason.
	state.checkError = {
		status: 409,
		json: { error: { code: 'MACHINE_BUSY', message: 'The agent is working on this machine; try again when it pauses' } }
	};
	await run.click();
	await expect(
		task.getByRole('alert').filter({ hasText: 'The agent is working on this machine; try again when it pauses' })
	).toBeVisible();
});

test('opens the recorded pull request only after publication succeeds', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const link = page.getByRole('link', { name: 'Open pull request', exact: true });
	state.task = {
		...state.task,
		state: 'publishing',
		reason: 'Independent validation passed; preparing review artifacts'
	};
	// An agent-reported URL without a recorded publication is never a review link.
	state.publication = {
		step: { id: 'publication-1', agent_run_id: 'run-1', status: 'running', command_argv: [] },
		pull_request_url: 'https://github.com/attacker/repo/pull/1'
	};
	emit('task.state_changed', state.task);
	await expect.poll(() => state.publications).toBeGreaterThan(0);
	await expect(link).toHaveCount(0);
	state.publication = {
		step: { ...state.publication.step, status: 'succeeded', completed_at: '2026-10-27T10:02:00Z' },
		branch: 'kuayle/eng-1',
		pull_request_url: 'https://github.com/acme/app/pull/12'
	};
	emit('task.publication_updated', { task_id: 'task-1', run_id: 'run-1', step: state.publication.step });
	await expect(link).toHaveAttribute('href', 'https://github.com/acme/app/pull/12');
	await expect(link).toHaveAttribute('rel', /noopener/);
	state.task = {
		...state.task,
		state: 'awaiting_review',
		reason: 'Pull request opened; waiting for human review',
		machine_id: 'machine-1'
	};
	emit('task.state_changed', state.task);
	await expect(page.getByText('Pull request opened; waiting for human review', { exact: true })).toBeVisible();
	state.changedFiles = ['src/theme.ts'];
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(page.getByText('Pull request opened', { exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'theme.ts' })).toHaveAttribute(
		'href',
		'https://github.com/acme/app/blob/kuayle/eng-1/src/theme.ts'
	);
	await page.reload();
	await expect(link).toHaveAttribute('href', 'https://github.com/acme/app/pull/12');
	expect(state.creates).toHaveLength(1);
});

async function awaitReview(state: any, emit: (type: string, payload: any) => void) {
	state.publication = {
		step: { id: 'publication-1', agent_run_id: 'run-1', status: 'succeeded', command_argv: [] },
		branch: 'kuayle/eng-1',
		pull_request_url: 'https://github.com/acme/app/pull/12'
	};
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emit('task.state_changed', state.task);
}

test('accepts work awaiting review in Kuayle without merging, after confirmation', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const task = page.getByRole('article', { name: 'Coding task' });
	const accept = task.getByRole('button', { name: 'Accept', exact: true });
	await expect(accept).toHaveCount(0);
	await awaitReview(state, emit);
	await expect(accept).toBeVisible();
	await expect(task.getByRole('link', { name: 'Open pull request', exact: true })).toBeVisible();

	await accept.click();
	const dialog = page.getByRole('alertdialog', { name: 'Accept ENG-1 without merging?' });
	await expect(dialog).toContainText('The pull request stays open on GitHub and is not merged.');
	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	await expect(dialog).toHaveCount(0);
	expect(state.accepts).toHaveLength(0);

	// A newer attempt or a merge in the meantime refuses the acceptance.
	state.acceptError = { status: 409, json: { error: { code: 'TASK_CONFLICT' } } };
	await accept.click();
	await dialog.getByRole('button', { name: 'Accept work', exact: true }).click();
	await expect(task.getByText('The task changed before it could be accepted. Review its current state.')).toBeVisible();
	state.acceptError = null;

	await accept.click();
	await dialog.getByRole('button', { name: 'Accept work', exact: true }).click();
	await expect(task.getByText('Accepted in Kuayle by Owner', { exact: true })).toBeVisible();
	await expect(task.getByText('Completed', { exact: true })).toBeVisible();
	expect(state.accepts).toEqual([{ generation: 1 }, { generation: 1 }]);
	await expect(accept).toHaveCount(0);
	await expect(task.getByRole('link', { name: 'Open pull request', exact: true })).toBeVisible();
});

for (const [name, width] of [
	['desktop', 1280],
	['phone', 390]
] as const) {
	test(`fits the accept action at ${name} width`, async ({ page }, testInfo) => {
		await page.setViewportSize({ width, height: 900 });
		const { state, emit } = await fixture(page);
		await page.goto('/test/assistant?conversation=chat-1');
		await delegate(page);
		await awaitReview(state, emit);
		const accept = page
			.getByRole('article', { name: 'Coding task' })
			.getByRole('button', { name: 'Accept', exact: true });
		await expect(accept).toBeVisible();
		// Every action stays reachable next to Accept, including the pull request.
		const pr = page.getByRole('article', { name: 'Coding task' }).getByRole('link', { name: 'Open pull request' });
		for (const control of [accept, pr]) {
			const box = await control.boundingBox();
			expect(box!.x + box!.width).toBeLessThanOrEqual(width);
		}
		expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
		await page.screenshot({ path: testInfo.outputPath(`task-accept-${name}.png`) });
		await accept.click();
		const dialog = page.getByRole('alertdialog', { name: 'Accept ENG-1 without merging?' });
		await expect(dialog).toBeVisible();
		const dialogBox = await dialog.boundingBox();
		expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(width);
		await page.waitForTimeout(300);
		await page.screenshot({ path: testInfo.outputPath(`task-accept-dialog-${name}.png`) });
	});
}

const taskPort = (over: any) => ({
	label: null,
	source: 'task',
	agent_task_id: 'task-1',
	task_identifier: 'ENG-1',
	listening: true,
	created_at: '2026-10-27T10:00:00Z',
	...over,
	host: `machine-${over.id}.machines.example.test`
});

test('starts, opens and stops the task app from its card', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = {
		...state.task,
		state: 'awaiting_review',
		reason: 'Pull request opened; waiting for human review',
		machine_id: 'machine-1',
		preview: null
	};
	emit('task.state_changed', state.task);
	await expect(page.getByText('Pull request opened; waiting for human review', { exact: true })).toBeVisible();
	await page
		.context()
		.route('https://*.machines.example.test/**', (route) => route.fulfill({ contentType: 'text/html', body: 'app' }));
	const tools = page.getByRole('group', { name: 'Tools' });
	const app = tools.getByRole('button', { name: /^App/ });
	await app.click();
	await page.getByRole('menuitem', { name: 'Start app' }).click();
	await expect.poll(() => state.appStarts).toBe(1);
	await expect(tools.getByRole('button', { name: 'App: starting' })).toBeVisible();
	// The manager reports the process running on its allocated port; the card
	// keeps checking while the app starts.
	state.ports = [
		taskPort({ id: 'port-t', port: 4100, label: 'ENG-1' }),
		taskPort({ id: 'port-a', port: 6006, label: 'storybook', source: 'agent' }),
		taskPort({ id: 'port-o', port: 4101, label: 'ENG-9', agent_task_id: 'task-9', task_identifier: 'ENG-9' }),
		taskPort({ id: 'port-u', port: 3000, label: 'web', source: 'user', agent_task_id: null, task_identifier: null })
	];
	state.task = { ...state.task, preview: { state: 'running', port_id: 'port-t', port: 4100, error: null } };
	await expect(tools.getByRole('button', { name: 'App: running' })).toBeVisible({ timeout: 10_000 });
	await tools.getByRole('button', { name: 'App: running' }).click();
	const menu = page.getByRole('menu');
	await expect(menu.getByRole('menuitem', { name: /^Open/ })).toHaveCount(2);
	await expect(menu.getByRole('menuitem', { name: 'Start app' })).toHaveCount(0);
	const popupPromise = page.waitForEvent('popup');
	await menu.getByRole('menuitem', { name: 'Open ENG-1 :4100' }).click();
	const popup = await popupPromise;
	await expect.poll(() => popup.url()).toContain('machine-port-t.machines.example.test');
	expect(state.portLaunches).toEqual(['port-t']);
	await tools.getByRole('button', { name: 'App: running' }).click();
	await page.getByRole('menu').getByRole('menuitem', { name: 'Stop app' }).click();
	await expect.poll(() => state.appStops).toBe(1);
	await expect(tools.getByRole('button', { name: 'App', exact: true })).toBeVisible();
	await tools.getByRole('button', { name: 'App', exact: true }).click();
	await expect(page.getByRole('menu').getByRole('menuitem', { name: 'Open storybook :6006' })).toBeVisible();
	await expect(page.getByRole('menu').getByRole('menuitem', { name: 'Start app' })).toBeVisible();
});

test('shows why the task app failed or could not start', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = {
		...state.task,
		state: 'awaiting_review',
		reason: 'Pull request opened; waiting for human review',
		machine_id: 'machine-1',
		preview: { state: 'failed', port_id: null, port: null, error: 'npm ERR! Missing script: "dev"' }
	};
	emit('task.state_changed', state.task);
	const tools = page.getByRole('group', { name: 'Tools' });
	await tools.getByRole('button', { name: 'App: failed' }).click();
	const menu = page.getByRole('menu');
	await expect(menu.getByText('npm ERR! Missing script: "dev"')).toBeVisible();
	state.appStartError = {
		status: 422,
		json: { error: { code: 'PREVIEW_UNAVAILABLE', message: 'This task has no preview command.' } }
	};
	await menu.getByRole('menuitem', { name: 'Start app' }).click();
	await expect(page.getByRole('alert').filter({ hasText: 'This task has no preview command.' })).toBeVisible();
});

for (const [name, width] of [
	['desktop', 1280],
	['phone', 390]
] as const) {
	test(`fits the task app control at ${name} width`, async ({ page }, testInfo) => {
		await page.setViewportSize({ width, height: 900 });
		const { state, emit } = await fixture(page);
		state.ports = [taskPort({ id: 'port-t', port: 4100, label: 'ENG-1' })];
		await page.goto('/test/assistant?conversation=chat-1');
		await delegate(page);
		state.task = {
			...state.task,
			state: 'awaiting_review',
			reason: 'Pull request opened; waiting for human review',
			machine_id: 'machine-1',
			preview: { state: 'running', port_id: 'port-t', port: 4100, error: null }
		};
		emit('task.state_changed', state.task);
		const task = page.getByRole('article', { name: 'Coding task' });
		await task.getByRole('button', { name: 'App: running' }).click();
		await expect(page.getByRole('menuitem', { name: 'Open ENG-1 :4100' })).toBeVisible();
		expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
		const item = await page.getByRole('menuitem', { name: 'Open ENG-1 :4100' }).boundingBox();
		expect(item!.x + item!.width).toBeLessThanOrEqual(width);
		// Let the menu finish its opening animation before capturing it.
		await page.waitForTimeout(300);
		await page.screenshot({ path: testInfo.outputPath(`task-app-${name}.png`) });
	});
}

test('shows GitHub CI and review signals with the follow-up they caused', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emit('task.state_changed', state.task);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	const section = page.getByRole('region', { name: 'GitHub checks and reviews', exact: true });
	await expect(section).toHaveCount(0);
	const base = {
		task_id: 'task-1',
		run_id: 'run-1',
		summary: '',
		url: 'https://github.com/acme/app/pull/12',
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	};
	state.signals = [
		{
			...base,
			id: 's2',
			kind: 'review',
			external_id: '5',
			outcome: 'changes_requested',
			name: '<script>window.evil=1</script>',
			action: 'untrusted'
		},
		{
			...base,
			id: 's1',
			kind: 'ci',
			external_id: '77',
			outcome: 'failure',
			name: 'GitHub Actions',
			action: 'follow_up',
			follow_up_generation: 2
		}
	];
	emit('task.signal_recorded', state.signals[1]);
	await expect(section.getByText('CI GitHub Actions: failure', { exact: true })).toBeVisible();
	await expect(section.getByText('Follow-up attempt queued', { exact: true })).toBeVisible();
	await expect(section.getByText('Ignored: reviewer has no write access', { exact: true })).toBeVisible();
	await expect(section.getByText('<script>window.evil=1</script>', { exact: false })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
});

test('shows the harness plan, reasoning summaries and changed files from recorded output', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = { ...state.task, state: 'running', reason: 'Native coding harness started', machine_id: 'machine-1' };
	emit('task.state_changed', state.task);
	await page.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(page.getByText('Plan', { exact: true })).toHaveCount(0);
	const line = (id: number, value: unknown) => ({
		id,
		agent_run_id: 'run-1',
		stream: 'stdout',
		sequence: id,
		content: JSON.stringify(value) + '\n',
		truncated: false
	});
	state.changedFiles = ['src/lib/theme.ts', 'src/lib/theme.test.ts', 'README.md'];
	state.logs = [
		line(1, { type: 'item.completed', item: { type: 'reasoning', text: 'The theme toggle lives in the header.' } }),
		line(2, {
			type: 'item.updated',
			item: {
				type: 'todo_list',
				items: [
					{ text: 'Add theme store', completed: true },
					{ text: '<img src=x onerror="window.evil=1">Wire toggle', completed: false }
				]
			}
		})
	];
	emit('task.activity', {
		task_id: 'task-1',
		run_id: 'run-1',
		log_id: 2,
		stream: 'stdout',
		truncated: false,
		events: []
	});
	await expect(page.getByText('1 of 2 done', { exact: true })).toBeVisible();
	await expect(page.getByText('Add theme store', { exact: true })).toBeVisible();
	await expect(page.getByText('<img src=x onerror="window.evil=1">Wire toggle', { exact: true })).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await page.getByRole('button', { name: 'Reasoning summary' }).click();
	await expect(page.getByText('The theme toggle lives in the header.', { exact: true })).toBeVisible();
	const files = page.getByRole('region', { name: 'Changed files', exact: true });
	await expect(files.getByRole('button', { name: 'src' })).toBeVisible();
	await expect(files.getByRole('button', { name: 'theme.test.ts' })).toBeVisible();
	await expect(files.getByRole('button', { name: 'README.md' })).toBeDisabled();
	await expect(files.getByRole('link')).toHaveCount(0, { timeout: 1000 });
});

test('opens the attempt diff beside the chat and closes it', async ({ page }) => {
	const { state, emit: emitState } = await fixture(page);
	state.changedFiles = ['src/text.js'];
	state.diff = [
		'diff --git a/src/text.js b/src/text.js',
		'@@ -1 +1,2 @@',
		' export const a = 1;',
		'-export const old = 0;',
		'+export const kebab = "<script>window.evil=1</script>";',
		''
	].join('\n');
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await expect(page.getByRole('button', { name: 'View diff' })).toHaveCount(0);
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emitState('task.state_changed', state.task);
	await page.getByRole('button', { name: 'View diff' }).click();
	const panel = page.getByRole('region', { name: 'Changes' });
	await expect(panel.getByText('src/text.js')).toBeVisible();
	await expect(panel.getByText('+1', { exact: true })).toBeVisible();
	await expect(panel.getByText('+export const kebab = "<script>window.evil=1</script>";')).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(page.getByRole('separator', { name: 'Resize changes panel' })).toBeVisible();
	await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
	await panel.getByRole('button', { name: 'Close changes' }).click();
	await expect(panel).toHaveCount(0);
});

test('pins a compact task header with every action while the chat scrolls', async ({ page }) => {
	const { state, emit: emitState } = await fixture(page);
	state.messages = Array.from({ length: 24 }, (_, i) => ({
		id: `message-${i + 1}`,
		conversation_id: 'chat-1',
		sequence: i + 1,
		role: i % 2 ? 'assistant' : 'user',
		content: `Message ${i + 1}: ${'a long line of discussion '.repeat(8)}`,
		status: 'completed',
		provider: '',
		model: '',
		created_at: '2026-10-26T10:00:00Z'
	}));
	state.changedFiles = ['src/text.js'];
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	await expect(page.getByRole('region', { name: 'Coding tasks' }).getByText('Queued', { exact: true })).toBeVisible();
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emitState('task.state_changed', state.task);
	const pinned = page.getByRole('region', { name: 'Coding task', exact: true });
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByRole('button', { name: 'View diff' })).toBeVisible();
	await page.getByText('Message 24:').scrollIntoViewIfNeeded();
	await expect(tasks).not.toBeInViewport();
	await expect(pinned).toBeVisible();
	await expect(pinned.getByText('Awaiting review')).toBeVisible();
	await expect(pinned.getByText('ENG-1')).toBeVisible();
	const diff = pinned.getByRole('button', { name: 'View diff' });
	await diff.hover();
	await expect(page.locator('[data-slot="tooltip-content"]')).toContainText('View diff');
	await diff.click();
	await expect(page.getByRole('region', { name: 'Changes' })).toBeVisible();
	await pinned.getByRole('button', { name: 'More actions', exact: true }).click();
	await expect(page.getByRole('menuitem', { name: 'Retry task', exact: true })).toBeVisible();
	await page.keyboard.press('Escape');
	await pinned.getByRole('button', { name: /ENG-1/ }).click();
	await expect(tasks).toBeInViewport();
	await expect(pinned).toHaveCount(0);
});

test('an approval on GitHub shows the task as approved and ready to merge', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Awaiting review', { exact: true })).toBeVisible();
	state.signals = [
		{
			id: 's1',
			task_id: 'task-1',
			run_id: state.task.run_id,
			kind: 'review',
			external_id: '9',
			outcome: 'approved',
			name: 'maintainer',
			action: 'recorded',
			summary: '',
			url: 'https://github.com/acme/app/pull/12',
			created_at: '2026-10-27T10:00:00Z',
			updated_at: '2026-10-27T10:00:00Z'
		}
	];
	emit('task.signal_recorded', state.signals[0]);
	await expect(tasks.getByText('Approved', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Approved by maintainer. Merge the pull request to complete the task.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Execution details', exact: true })).toBeVisible();
});

test('an attempt whose records are gone keeps the task panel available', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	state.missingRun = true;
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('This attempt’s records are no longer available.')).toBeVisible();
	await expect(page.getByText('Coding tasks are unavailable for this account.')).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Start coding', exact: true })).toBeVisible();
});

test('a task stopped for the agent’s sign-in continues once the user signed in', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	state.task = {
		...state.task,
		state: 'failed',
		sign_in_required: true,
		reason: 'Codex needs you to sign in. Sign in from the terminal, then continue'
	};
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Codex needs you to sign in', { exact: true })).toBeVisible();
	const proceed = tasks.getByRole('button', { name: 'I’ve signed in — continue' });
	await proceed.click();
	await expect(tasks.getByText('No sign-in was found in the terminal yet.', { exact: false })).toBeVisible();
	state.signInFound = true;
	await proceed.click();
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Codex needs you to sign in', { exact: true })).toHaveCount(0);
	expect(state.signIns).toHaveLength(2);
	expect(state.signIns[0].request_id).not.toBe(state.signIns[1].request_id);
});

test('a Claude Code task stopped for sign-in continues with a pasted token', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	state.task = {
		...state.task,
		provider: 'claude-code',
		state: 'failed',
		sign_in_required: true,
		reason: 'Claude Code needs you to sign in. Paste a token, then continue'
	};
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Claude Code needs you to sign in', { exact: true })).toBeVisible();
	await expect(tasks.getByText('claude setup-token', { exact: true })).toBeVisible();
	await expect(tasks.getByRole('button', { name: 'I’ve signed in — continue' })).toBeVisible();
	await expect(tasks.getByRole('link', { name: 'Manage your sign-ins' })).toHaveAttribute(
		'href',
		'/test/settings/agents#your-sign-ins'
	);

	const field = tasks.getByLabel('Claude Code token', { exact: true });
	await field.fill('not a token');
	await tasks.getByRole('button', { name: 'Save token and continue' }).click();
	await expect(tasks.getByText('That doesn’t look like a token.', { exact: false })).toBeVisible();
	expect(state.tokenSaves).toHaveLength(0);

	await field.fill('  sk-ant-oat01-browser-fixture-token-0001\n');
	await tasks.getByRole('button', { name: 'Save token and continue' }).click();
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Claude Code needs you to sign in', { exact: true })).toHaveCount(0);
	expect(state.tokenSaves).toEqual(['sk-ant-oat01-browser-fixture-token-0001']);
	expect(state.signIns).toHaveLength(1);
});

test('requests an agent review through the chat and shows its findings on the task', async ({ page }, testInfo) => {
	const { state, emit } = await fixture(page);
	state.providers = [
		{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false },
		{ id: 'claude-code', display_name: 'Claude Code', supported_modes: ['autonomous'], custom: false }
	];
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	const time = '2026-10-27T10:00:00Z';
	state.publication = {
		step: { id: 'publication-1', agent_run_id: 'run-1', status: 'succeeded', command_argv: [], completed_at: time },
		branch: 'kuayle/eng-1',
		pull_request_url: 'https://github.com/acme/app/pull/12'
	};
	const implement = {
		id: 'session-1',
		task_id: 'task-1',
		provider: 'codex',
		purpose: 'implement',
		state: 'suspended',
		native_session_id: '0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b',
		created_at: time,
		updated_at: time,
		task_state: 'awaiting_review'
	};
	state.sessions = [implement];
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Awaiting review', { exact: true })).toBeVisible();

	// The action only writes the request into the composer; the assistant asks
	// for approval, so nothing is started from the card.
	const requests: string[] = [];
	page.on('request', (request) => {
		if (request.method() !== 'GET') requests.push(request.url());
	});
	await tasks.getByRole('button', { name: 'Request review', exact: true }).click();
	const items = page.getByRole('menuitem');
	await expect(items).toHaveText([/Codex\s*Same agent$/, /Claude Code$/]);
	await page.getByRole('menuitem', { name: 'Claude Code' }).click();
	await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Review ENG-1 with Claude Code');
	await expect(page.getByRole('textbox', { name: 'Message' })).toBeFocused();
	expect(requests).toEqual([]);

	// A review task's own events never become a task card.
	emit('task.state_changed', {
		...state.task,
		id: 'review-1',
		kind: 'review',
		review_of_task_id: 'task-1',
		provider: 'claude-code',
		state: 'running',
		prompt: 'You are reviewing pull request #12'
	});
	state.sessions = [
		{
			id: 'session-2',
			task_id: 'review-1',
			provider: 'claude-code',
			purpose: 'review',
			state: 'completed',
			created_at: '2026-10-27T10:05:00Z',
			updated_at: '2026-10-27T10:05:00Z',
			task_state: 'completed',
			review: {
				task_id: 'review-1',
				reviewed_task_id: 'task-1',
				requested_by: 'user-1',
				provider: 'claude-code',
				commit_sha: '0123456789abcdef0123456789abcdef01234567',
				repository: 'acme/app',
				pull_request_number: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				base_branch: 'main',
				focus: 'Error paths',
				attempt: 1,
				post_to_pull_request: true,
				verdict: 'request_changes',
				findings:
					'- `search.ts:12` **major**: handle the empty query <img src=x onerror="window.evil=1"><script>window.evil=1</script>',
				comment_state: 'posted',
				created_at: '2026-10-27T10:05:00Z',
				updated_at: '2026-10-27T10:06:00Z'
			}
		},
		implement
	];
	emit('task.review_updated', { task_id: 'task-1', review_task_id: 'review-1', state: 'completed' });
	const reviews = tasks.getByRole('region', { name: 'Agent reviews' });
	await expect(reviews.getByText('Review by Claude Code', { exact: true })).toBeVisible();
	await expect(reviews.getByText('Changes requested', { exact: true })).toBeVisible();
	await expect(reviews.getByText('handle the empty query', { exact: false })).toBeVisible();
	await expect(reviews.getByText('Commit 0123456 of pull request #12', { exact: true })).toBeVisible();
	await expect(reviews.getByText('Findings posted on the pull request.', { exact: false })).toBeVisible();
	expect(await reviews.locator('script, img').count()).toBe(0);
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(page.getByRole('article', { name: 'Coding task', exact: true })).toHaveCount(1);
	await tasks.getByRole('button', { name: 'Execution details', exact: true }).click();
	await expect(tasks.getByText('Codex implementation session', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Claude Code review session', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Suspended, can resume', { exact: false })).toBeVisible();
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		await tasks.screenshot({ path: testInfo.outputPath(`agent-review-${dark ? 'dark' : 'light'}.png`) });
	}
});

test('the task card shows the agent messages of its issue and lets the creator hold them', async ({
	page
}, testInfo) => {
	const { state, emit } = await fixture(page);
	const sent = {
		from_task_id: 'task-1',
		from_issue_id: 'issue-1',
		from_identifier: 'ENG-1',
		from_title: 'Implement search',
		to_ref: 'ENG-2',
		to_issue_id: 'issue-2',
		to_task_id: 'task-2',
		to_identifier: 'ENG-2',
		to_title: 'Search UI',
		channel: 'internal',
		reject_reason: null,
		held_by_name: null,
		dropped_by_name: null,
		comment_state: 'none',
		comment_id: null,
		conversation_hold_id: null,
		can_moderate: true,
		created_at: '2026-10-28T09:30:00Z'
	};
	state.agentMessages = [
		{ ...sent, id: 'm1', state: 'queued', body: 'The search endpoint is /v2/search and returns {items, next}.' },
		{
			...sent,
			id: 'm2',
			state: 'delivered',
			from_identifier: 'ENG-2',
			from_issue_id: 'issue-2',
			to_identifier: 'ENG-1',
			to_issue_id: 'issue-1',
			channel: 'comment',
			comment_state: 'posted',
			body: 'UI will call /v2/search; please keep the cursor field.'
		}
	];
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = { ...state.task, state: 'running', reason: 'Native coding harness started', machine_id: 'machine-1' };
	emit('task.state_changed', state.task);
	const panel = page.getByRole('region', { name: 'Agent messages', exact: true });
	await expect(panel.getByRole('listitem')).toHaveCount(2);
	expect(state.messageQueries.at(-1)).toBe('?issue_id=issue-1');
	await expect(panel.getByText('1 · Queued')).toBeVisible();
	await expect(panel.getByText('Comment posted')).toBeVisible();
	await expect(panel.getByRole('link', { name: 'Open all' })).toHaveAttribute(
		'href',
		'/test/settings/agent-messages?issue_id=issue-1'
	);
	const queued = panel.getByRole('listitem', { name: 'Message from ENG-1 to ENG-2' });
	await queued.getByRole('button', { name: 'Hold', exact: true }).click();
	await expect.poll(() => state.messageActions).toEqual(['m1:hold']);
	await expect(queued.getByRole('status')).toHaveText('Held');
	await expect(queued.getByRole('button', { name: 'Release', exact: true })).toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
	await panel.screenshot({ path: testInfo.outputPath('task-messages-390.png') });
});

const agentQuestion = {
	id: 'live-agent-1',
	task_id: 'task-1',
	direction: 'from_agent',
	seq: null,
	sender: 'agent',
	author_id: null,
	kind: 'question',
	body: 'Keep the v1 route? See [the plan](https://evil.example/steal)',
	state: 'received',
	delivered_via: null,
	run_id: 'run-1',
	reject_reason: null,
	created_at: '2026-10-26T10:02:00Z',
	delivered_at: null,
	identifier: 'ENG-1'
};

test('the task card thread messages the running agent and shows its answers live', async ({ page }) => {
	const { state, emit } = await fixture(page);
	state.task = {
		id: 'task-1',
		workspace_id: 'workspace-1',
		conversation_id: 'chat-1',
		creator_id: 'user-1',
		issue_id: 'issue-1',
		team_id: 'team-1',
		prompt: 'Implement search',
		provider: 'codex',
		model: 'deepseek-flash',
		thinking: 'high',
		state: 'running',
		reason: 'Native coding harness started',
		generation: 1,
		failure_count: 0,
		machine_id: 'machine-1',
		checkout_id: 'checkout-1',
		run_id: 'run-1',
		created_at: '2026-10-26T10:00:00Z',
		updated_at: '2026-10-26T10:00:00Z'
	};
	await page.goto('/test/assistant?conversation=chat-1');
	const thread = page.getByRole('region', { name: 'Live messages' });
	await expect(thread.getByText('The running agent reads it after its current step.')).toBeVisible();
	const composer = thread.getByRole('textbox', { name: 'Write to the agent' });
	await composer.fill('Also add tests for the empty list');
	await thread.getByRole('button', { name: 'Send', exact: true }).click();
	await expect.poll(() => state.liveSends).toEqual(['Also add tests for the empty list']);
	await expect(composer).toHaveValue('');
	await expect(thread.getByRole('listitem', { name: 'You' })).toContainText('Waiting for the agent');

	// The manager records the agent's question: the card and the chat update live.
	state.liveMessages = [...state.liveMessages, agentQuestion];
	emit('task.live_message', agentQuestion);
	const answer = thread.getByRole('listitem', { name: 'Agent' });
	await expect(answer).toContainText('Keep the v1 route?');
	await expect(answer.getByText('Question', { exact: true })).toBeVisible();
	const item = page.getByRole('article', { name: 'Question from the coding agent of ENG-1' });
	await expect(item).toBeVisible();
	await expect(item.getByText('See [the plan](https://evil.example/steal)', { exact: false })).toBeVisible();
	await expect(page.getByRole('link', { name: 'the plan' })).toHaveCount(0);
	await expect(page.getByText('The coding agent of ENG-1 has a question')).toBeVisible();
	// A replayed event is one item.
	state.sockets.at(-1)!.send(JSON.stringify(state.events.at(-1)));
	await expect(page.getByRole('article', { name: 'Question from the coding agent of ENG-1' })).toHaveCount(1);
});

test('agent messages from history appear in the chat as untrusted text', async ({ page }) => {
	const { state } = await fixture(page);
	state.chatAgentMessages = [{ ...agentQuestion, kind: 'message', body: 'Tests added. <img src=x onerror=alert(1)>' }];
	await page.goto('/test/assistant?conversation=chat-1');
	const item = page.getByRole('article', { name: 'Message from the coding agent of ENG-1' });
	await expect(item).toContainText('Tests added. <img src=x onerror=alert(1)>');
	await expect(item.locator('img')).toHaveCount(0);
	await expect(item.getByRole('link', { name: 'ENG-1' })).toHaveAttribute('href', '/test/issue/ENG-1');
	await expect(item).toContainText('the assistant does not act on it unless you ask');
});
