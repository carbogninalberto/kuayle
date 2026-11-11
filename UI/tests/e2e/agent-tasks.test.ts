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
		terminals: 0,
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
		diffTruncated: false,
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
		resumes: [] as string[],
		pauses: [] as string[],
		signInFound: false,
		tokenSaves: [] as any[],
		sessions: [] as any[],
		agentMessages: [] as any[],
		liveMessages: [] as any[],
		liveSends: [] as string[],
		transitions: null as any[] | null,
		chatAgentMessages: [] as any[],
		messageQueries: [] as string[],
		messageActions: [] as string[],
		providers: [{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false }] as any[],
		// More tasks of the same conversation, listed only (their routes are not served).
		otherTasks: [] as any[]
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
			return route.fulfill({ json: { tasks: [...(state.task ? [state.task] : []), ...state.otherTasks] } });
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
			if (path.endsWith('/terminal') && req.method() === 'POST') {
				state.terminals++;
				return route.fulfill({
					status: 409,
					json: {
						error: {
							code: 'TASK_BUSY',
							message: 'The agent is working on this task; open it in Claude Code when it stops'
						}
					}
				});
			}
			if (path.endsWith('/sign-in')) {
				state.signIns.push(req.postDataJSON());
				if (!state.signInFound) return route.fulfill({ status: 409, json: { error: { code: 'SIGN_IN_NOT_FOUND' } } });
				state.task = { ...state.task, state: 'queued', generation: 2, run_id: 'run-2', sign_in_required: false };
				return route.fulfill({ status: 202, json: state.task });
			}
			if (path.endsWith('/pause')) {
				const mode = req.postDataJSON().mode;
				state.pauses.push(mode);
				const updated_at = new Date(Date.parse(state.task.updated_at) + 1000).toISOString();
				// "Pause now" stops the agent; "after this step" only records the request.
				state.task =
					mode === 'now'
						? { ...state.task, state: 'blocked', reason: 'Paused', workflow_paused_at: updated_at, updated_at }
						: { ...state.task, workflow_pause_request: mode, updated_at };
				return route.fulfill({ status: 202, json: state.task });
			}
			if (path.endsWith('/resume') && (state.task.workflow_paused_at || state.task.workflow_pause_request)) {
				state.resumes.push('workflow');
				const updated_at = new Date(Date.parse(state.task.updated_at) + 1000).toISOString();
				const stopped = !!state.task.workflow_paused_at;
				state.task = {
					...state.task,
					...(stopped ? { state: 'queued', reason: 'Resumed', generation: state.task.generation + 1, run_id: 'run-2' } : {}),
					workflow_paused_at: null,
					workflow_pause_request: null,
					updated_at
				};
				return route.fulfill({ status: 202, json: state.task });
			}
			if (path.endsWith('/resume')) {
				state.resumes.push(req.method());
				if (req.method() === 'DELETE') {
					// The server stamps updated_at, so the card takes the newer task.
					const updated_at = new Date(Date.parse(state.task.updated_at) + 1000).toISOString();
					state.task = { ...state.task, usage_limit_resume_at: null, updated_at };
					return route.fulfill({ json: state.task });
				}
				state.task = {
					...state.task,
					state: 'queued',
					generation: 2,
					run_id: 'run-2',
					reason: 'Resumed by request after the usage limit',
					usage_limit_message: null,
					usage_limit_resets_at: null,
					usage_limit_resume_at: null
				};
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
						transitions: state.transitions ?? [
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
			if (path.endsWith('/runs/run-1/diff'))
				return route.fulfill({ json: { diff: state.diff, truncated: state.diffTruncated } });
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
	// A second harness, so the picker shows several tiles side by side.
	state.providers.push({ id: 'claude', display_name: 'Claude Code', supported_modes: ['autonomous'], custom: false });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	await page.getByRole('button', { name: 'Start coding', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Start coding', exact: true });
	// The harness is chosen from logo tiles; the default server fits Codex.
	await expect(dialog.getByRole('radio', { name: 'Codex' })).toHaveAttribute('aria-checked', 'true');
	// Every tile is a separate chip: a border on all four sides and the same rounded corners, never a joined bar.
	const tiles = await dialog.getByRole('radio').evaluateAll((nodes) =>
		nodes.map((node) => {
			const style = getComputedStyle(node);
			return {
				borders: [style.borderTopWidth, style.borderRightWidth, style.borderBottomWidth, style.borderLeftWidth],
				radii: [
					style.borderTopLeftRadius,
					style.borderTopRightRadius,
					style.borderBottomRightRadius,
					style.borderBottomLeftRadius
				]
			};
		})
	);
	expect(tiles.length).toBeGreaterThan(1);
	for (const tile of tiles) {
		expect(tile.borders.every((width) => width !== '0px')).toBe(true);
		expect(new Set(tile.radii).size).toBe(1);
		expect(tile.radii[0]).toBe(tiles[0].radii[0]);
	}
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
	await openOutput(page);
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

// The coding agents live in the side panel: inline on wide screens, a drawer
// that the top bar's Agents button opens on narrow ones.
async function showAgents(page: Page) {
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(page.getByRole('button', { name: 'Agents', exact: true }).first()).toBeVisible();
	if (await tasks.isVisible()) return;
	await page.getByRole('button', { name: 'Agents', exact: true }).first().click();
	await expect(tasks).toBeVisible();
}

// Raw output starts collapsed. A click that lands while the execution details
// are still opening can be lost, so click until the output is really open.
async function openOutput(page: Page) {
	const output = page.getByRole('button', { name: 'Execution output', exact: true });
	await expect(async () => {
		if ((await output.getAttribute('aria-expanded')) !== 'true') await output.click();
		await expect(output).toHaveAttribute('aria-expanded', 'true', { timeout: 1000 });
	}).toPass();
}

async function delegate(page: Page, prompt = 'Implement and test issue search') {
	await showAgents(page);
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
	await expect(
		page.getByRole('region', { name: 'Coding tasks' }).getByText('Iteration 2', { exact: false })
	).toBeVisible();
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
	await showAgents(page);
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
	await openOutput(page);
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
	await openOutput(page);
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
	await showAgents(page);
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
	await showAgents(page);
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
	const run = page.getByRole('menuitem', { name: 'Run check', exact: true });
	await openTaskMenu(page);
	await expect(run).toHaveCount(0);
	await page.keyboard.press('Escape');
	await awaitReview(state, emit);
	state.task = { ...state.task, machine_id: 'machine-1' };
	emit('task.state_changed', state.task);
	await openTaskMenu(page);
	await run.click();
	await expect.poll(() => state.checkRuns).toEqual([{}]);
	const manual = task.getByRole('region', { name: 'Manual check', exact: true });
	await expect(manual.getByText('Manual check queued')).toBeVisible();
	await expect(task.getByRole('region', { name: 'Independent checks', exact: true })).toContainText(
		'No independent check recorded'
	);
	const busy = page.getByRole('menuitem', { name: 'Running manual check' });
	state.manualChecks = [{ ...state.manualChecks[0], status: 'running', started_at: '2026-10-27T10:00:00Z' }];
	emit('task.tests_updated', { task_id: 'task-1', run_id: 'run-1', step: state.manualChecks[0] });
	await expect(manual.getByText('Running manual check')).toBeVisible();
	await openTaskMenu(page);
	await expect(busy).toBeDisabled();
	await page.keyboard.press('Escape');
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
	await openTaskMenu(page);
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

// KUA-129: a running workflow is paused from the task card's menu, where
// cancel is, now or after the current step. A paused task says "Paused" and
// has one next step, Resume.
test('pauses a running workflow from the task menu and resumes it', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	const card = page.getByRole('article', { name: 'Coding task' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	// A task on the Standard flow has no workflow to pause.
	await openTaskMenu(page);
	await expect(page.getByRole('menuitem', { name: 'Cancel task', exact: true })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: 'Pause', exact: true })).toHaveCount(0);
	await page.keyboard.press('Escape');

	state.task = {
		...state.task,
		state: 'running',
		reason: 'Workflow · Start → Implementer',
		workflow_version_id: 'version-1',
		workflow_node: 'implement',
		workflow_status: 'running'
	};
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Running', { exact: true })).toBeVisible();
	await expect(card.getByRole('button', { name: 'Resume', exact: true })).toHaveCount(0);

	// After this step: the agent keeps working, and the request can be withdrawn.
	await openTaskMenu(page);
	await page.getByRole('menuitem', { name: 'Pause', exact: true }).click();
	await expect(page.getByRole('menuitem', { name: 'Pause now', exact: true })).toBeVisible();
	await page.getByRole('menuitem', { name: 'Pause after this step', exact: true }).click();
	await expect(tasks.getByText('Pausing', { exact: true })).toBeVisible();
	await openTaskMenu(page);
	await expect(page.getByRole('menuitem', { name: 'Pause', exact: true })).toHaveCount(0);
	await page.keyboard.press('Escape');
	await card.getByRole('button', { name: 'Resume', exact: true }).click();
	await expect(tasks.getByText('Running', { exact: true })).toBeVisible();
	await expect(card.getByRole('button', { name: 'Resume', exact: true })).toHaveCount(0);

	// Now: the task is paused, not blocked or failed, and resuming is its one next step.
	await openTaskMenu(page);
	await page.getByRole('menuitem', { name: 'Pause', exact: true }).click();
	await page.getByRole('menuitem', { name: 'Pause now', exact: true }).click();
	await expect(tasks.getByText('Paused', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Blocked', { exact: true })).toHaveCount(0);
	await expect(card.getByRole('button', { name: 'Retry task', exact: true })).toHaveCount(0);
	await openTaskMenu(page);
	await expect(page.getByRole('menuitem', { name: 'Retry task', exact: true })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: 'Cancel task', exact: true })).toBeVisible();
	await page.keyboard.press('Escape');
	await card.getByRole('button', { name: 'Resume', exact: true }).click();
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	await expect(card.getByRole('button', { name: 'Resume', exact: true })).toHaveCount(0);
	expect(state.pauses).toEqual(['after_step', 'now']);
	expect(state.resumes).toEqual(['workflow', 'workflow']);

	// A pause that took effect elsewhere (another tab, or after the step)
	// shows on a waiting task too, which keeps its state underneath.
	state.task = {
		...state.task,
		state: 'awaiting_review',
		reason: 'Workflow · waiting for CI on the pull request',
		workflow_status: 'waiting',
		workflow_paused_at: '2026-11-11T10:00:00Z'
	};
	emit('task.updated', state.task);
	await expect(tasks.getByText('Paused', { exact: true })).toBeVisible();
	await expect(card.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
	await expect(card.getByRole('button', { name: 'Accept', exact: true })).toHaveCount(0);
});

// Tools and secondary actions live in the task card's one "More actions" menu.
async function openTaskMenu(page: Page) {
	await page
		.getByRole('article', { name: 'Coding task' })
		.getByRole('button', { name: 'More actions', exact: true })
		.click();
	return page.getByRole('menu').first();
}

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
	// The app is a submenu of the card's menu; its label carries the app's state.
	const app = (name: string | RegExp) => page.getByRole('menuitem', { name, exact: true });
	const sub = page.getByRole('menu').last();
	await openTaskMenu(page);
	await app(/^App/).click();
	await page.getByRole('menuitem', { name: 'Start app' }).click();
	await expect.poll(() => state.appStarts).toBe(1);
	await openTaskMenu(page);
	await expect(app('App: starting')).toBeVisible();
	// The manager reports the process running on its allocated port; the card
	// keeps checking while the app starts.
	state.ports = [
		taskPort({ id: 'port-t', port: 4100, label: 'ENG-1' }),
		taskPort({ id: 'port-a', port: 6006, label: 'storybook', source: 'agent' }),
		taskPort({ id: 'port-o', port: 4101, label: 'ENG-9', agent_task_id: 'task-9', task_identifier: 'ENG-9' }),
		taskPort({ id: 'port-u', port: 3000, label: 'web', source: 'user', agent_task_id: null, task_identifier: null })
	];
	state.task = { ...state.task, preview: { state: 'running', port_id: 'port-t', port: 4100, error: null } };
	await expect(app('App: running')).toBeVisible({ timeout: 10_000 });
	await app('App: running').click();
	await expect(sub.getByRole('menuitem', { name: /^Open/ })).toHaveCount(2);
	await expect(sub.getByRole('menuitem', { name: 'Start app' })).toHaveCount(0);
	const popupPromise = page.waitForEvent('popup');
	await sub.getByRole('menuitem', { name: 'Open ENG-1 :4100' }).click();
	const popup = await popupPromise;
	await expect.poll(() => popup.url()).toContain('machine-port-t.machines.example.test');
	expect(state.portLaunches).toEqual(['port-t']);
	await openTaskMenu(page);
	await app('App: running').click();
	await page.getByRole('menuitem', { name: 'Stop app' }).click();
	await expect.poll(() => state.appStops).toBe(1);
	await openTaskMenu(page);
	await expect(app('App')).toBeVisible();
	await app('App').click();
	await expect(sub.getByRole('menuitem', { name: 'Open storybook :6006' })).toBeVisible();
	await expect(sub.getByRole('menuitem', { name: 'Start app' })).toBeVisible();
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
	await openTaskMenu(page);
	await page.getByRole('menuitem', { name: 'App: failed' }).click();
	const menu = page.getByRole('menu').last();
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
		await openTaskMenu(page);
		await page.getByRole('menuitem', { name: 'App: running' }).click();
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
	await expect(section.getByText('Follow-up iteration queued', { exact: true })).toBeVisible();
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
	await expect(panel.getByTestId('diff-totals')).toHaveText('1 file +1 −1');
	await expect(panel.getByRole('button', { name: /src\/text\.js/ })).toContainText('+1');
	const line = panel.getByText('+export const kebab', { exact: false });
	const search = panel.getByRole('searchbox', { name: 'Search changes' });
	await search.fill('nomatch');
	await expect(panel.getByText('No files match “nomatch”.')).toBeVisible();
	await panel.getByRole('button', { name: 'Clear search' }).click();
	await expect(search).toHaveValue('');
	// A search shows matching files expanded, even ones the user collapsed, and bulk
	// collapse is unavailable while it is active; clearing restores the user's state.
	await panel.getByRole('button', { name: 'Collapse all files' }).click();
	await expect(line).not.toBeVisible();
	await search.fill('kebab');
	await expect(panel.getByTestId('diff-match-count')).toHaveText('1 of 1 files match');
	await expect(line).toBeVisible();
	await expect(panel.getByRole('button', { name: 'Expand all files' })).toBeDisabled();
	// Collapsing a match during the search lasts only for that query.
	await panel.getByRole('button', { name: /src\/text\.js/ }).click();
	await expect(line).not.toBeVisible();
	await search.fill('keba');
	await expect(line).toBeVisible();
	await search.fill('');
	await expect(line).not.toBeVisible();
	await panel.getByRole('button', { name: 'Expand all files' }).click();
	await expect(panel.getByText('+export const kebab = "<script>window.evil=1</script>";')).toBeVisible();
	expect(await page.evaluate(() => (window as any).evil)).toBeUndefined();
	await expect(page.getByRole('separator', { name: 'Resize side panel' })).toBeVisible();
	await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
	await panel.getByRole('button', { name: 'Close changes' }).click();
	await expect(panel).toHaveCount(0);
});

test('qualifies diff totals and empty searches when the diff is truncated', async ({ page }) => {
	const { state, emit: emitState } = await fixture(page);
	state.changedFiles = ['src/text.js'];
	state.diffTruncated = true;
	state.diff = ['diff --git a/src/text.js b/src/text.js', '@@ -1 +1 @@', '-old', '+new', ''].join('\n');
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emitState('task.state_changed', state.task);
	await page.getByRole('button', { name: 'View diff' }).click();
	const panel = page.getByRole('region', { name: 'Changes' });
	const totals = panel.getByTestId('diff-totals');
	await expect(totals).toContainText('At least 1 file +1 −1');
	await expect(totals).toContainText('Totals cover only the loaded part of the diff.');
	await panel.getByRole('searchbox', { name: 'Search changes' }).fill('elsewhere');
	await expect(panel.getByText('No files match “elsewhere” in the loaded part of the diff.')).toBeVisible();
	await expect(panel.getByText(/The diff is too large and was cut off/)).toBeVisible();
});

test('the task stays in view beside the chat while the conversation scrolls', async ({ page }) => {
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
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	state.task = { ...state.task, state: 'awaiting_review', reason: 'Pull request opened; waiting for human review' };
	emitState('task.state_changed', state.task);
	await expect(tasks.getByRole('button', { name: 'View diff' })).toBeVisible();
	await page.getByText('Message 1:').scrollIntoViewIfNeeded();
	await page.getByText('Message 24:').scrollIntoViewIfNeeded();
	// No up and down: the agent's task never scrolls away with the chat.
	await expect(tasks).toBeInViewport();
	await expect(tasks.getByText('Awaiting review')).toBeVisible();
	// The task also sits in the transcript where it started, and opens its thread.
	const step = page.getByRole('group', { name: 'Agent task for ENG-1' });
	await expect(step).toContainText('Awaiting review');
	await step.getByRole('button', { name: 'Open thread' }).click();
	// It opens the agent's thread in the centre; the panel keeps the task.
	await expect(page).toHaveURL(/[?&]agent=task-1/);
	await expect(page.getByRole('navigation', { name: 'Location' })).toContainText('ENG-1 · Iteration 1');
	await expect(tasks).toBeVisible();
	await tasks.getByRole('button', { name: 'View diff' }).click();
	await expect(page.getByRole('region', { name: 'Changes' })).toBeVisible();
});

test('a failed task stays in the transcript and the panel with its reason and a retry', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	state.task = { ...state.task, state: 'failed', reason: 'Execution failed; runtime cleanup confirmed' };
	emit('task.state_changed', state.task);
	const step = page.getByRole('group', { name: 'Agent task for ENG-1' });
	await expect(step).toContainText('Failed');
	await expect(step).toContainText('Stopped: Execution failed; runtime cleanup confirmed');
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Failed', { exact: true })).toBeVisible();
	// Still listed after a reload: nothing about a failure disappears.
	await page.reload();
	await expect(page.getByRole('group', { name: 'Agent task for ENG-1' })).toContainText('Failed');
	await expect(page.getByRole('region', { name: 'Coding tasks' }).getByText('Failed', { exact: true })).toBeVisible();
	await page
		.getByRole('group', { name: 'Agent task for ENG-1' })
		.getByRole('button', { name: 'Retry this task' })
		.click();
	await expect(page.getByRole('dialog', { name: 'Retry task', exact: true })).toBeVisible();
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
	await expect(tasks.getByText('This iteration’s records are no longer available.')).toBeVisible();
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

// Production (KUA-98): Claude Code stopped at its 5-hour limit. The task is
// paused, not failed: the chat says why and when it resumes, and the creator
// can keep it paused or resume it now.
test('a task paused at the usage limit shows the reset and can be resumed', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	const message = "You've hit your 5-hour limit · resets 4:10am (UTC)";
	state.task = {
		...state.task,
		provider: 'claude-code',
		state: 'blocked',
		reason: 'Paused: Claude Code usage limit reached. ' + message,
		usage_limit_message: message,
		usage_limit_resets_at: '2026-11-10T04:10:00Z',
		usage_limit_resume_at: '2026-11-10T04:12:00Z'
	};
	emit('task.state_changed', state.task);
	const paused = tasks.getByTestId('task-usage-limit');
	await expect(paused.getByText('Claude Code usage limit reached', { exact: true })).toBeVisible();
	await expect(paused.getByText(message, { exact: true })).toBeVisible();
	await expect(paused.getByText(/resumes automatically at/)).toBeVisible();
	await expect(tasks.getByText('Paused', { exact: true })).toBeVisible();
	await expect(tasks.getByText('Failed', { exact: true })).toHaveCount(0);

	await paused.getByRole('button', { name: "Don't resume" }).click();
	await expect(paused.getByText(/Automatic resume is off/)).toBeVisible();
	await expect(paused.getByRole('button', { name: "Don't resume" })).toHaveCount(0);

	await paused.getByRole('button', { name: 'Resume now' }).click();
	await expect(tasks.getByTestId('task-usage-limit')).toHaveCount(0);
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	expect(state.resumes).toEqual(['DELETE', 'POST']);
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
	await openTaskMenu(page);
	await page.getByRole('menuitem', { name: 'Request review', exact: true }).click();
	const items = page.getByRole('menu').last().getByRole('menuitem');
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
	// The card's own one-line composer writes to the agent; its hint says when it reads it.
	const reply = thread.getByRole('textbox', { name: 'Reply to the agent' });
	await expect(reply).toHaveAccessibleDescription('The running agent reads it after its current step.');
	const send = thread.getByRole('button', { name: 'Send', exact: true });
	await expect(send).toBeDisabled();
	await reply.fill('Also add tests for the empty list');
	await reply.press('Enter');
	await expect.poll(() => state.liveSends).toEqual(['Also add tests for the empty list']);
	await expect(reply).toHaveValue('');
	await expect(thread.getByRole('listitem', { name: 'You' })).toContainText('Waiting for the agent');
	// The chat composer says plainly whether it writes to the assistant or to the agent.
	const target = page.getByRole('button', { name: /^Send to: / });
	await expect(target).toHaveAccessibleName('Send to: Assistant');
	await target.click();
	await page.getByRole('menuitemcheckbox', { name: 'ENG-1 · Codex agent' }).click();
	await expect(target).toHaveAccessibleName('Send to: ENG-1 · Codex agent');
	await page.keyboard.press('Escape');
	await expect(page.getByText('Messages go to the Codex agent of ENG-1, not to the assistant.')).toBeVisible();
	const composer = page.getByRole('textbox', { name: 'Write to the agent' });
	await composer.fill('Keep the old route');
	await composer.press('Enter');
	await expect.poll(() => state.liveSends).toEqual(['Also add tests for the empty list', 'Keep the old route']);
	await expect(composer).toHaveValue('');
	// Nothing was sent to the assistant; switching back aims the composer at it again.
	await page.getByRole('button', { name: 'Back to assistant' }).click();
	await expect(target).toHaveAccessibleName('Send to: Assistant');
	await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();

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

test('opens a stopped Claude Code task in Claude Code through the task terminal', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.goto('/test/assistant?conversation=chat-1');
	await delegate(page);
	const tasks = page.getByRole('region', { name: 'Coding tasks' });
	await expect(tasks.getByText('Queued', { exact: true })).toBeVisible();
	const open = page.getByRole('menuitem', { name: 'Open in Claude Code', exact: true });
	await openTaskMenu(page);
	await expect(open).toHaveCount(0);
	await page.keyboard.press('Escape');
	// A Codex task keeps the typed session command instead.
	state.task = {
		...state.task,
		state: 'awaiting_review',
		reason: 'Pull request opened; waiting for human review',
		machine_id: 'machine-1'
	};
	emit('task.state_changed', state.task);
	await expect(tasks.getByText('Pull request opened; waiting for human review', { exact: true })).toBeVisible();
	await openTaskMenu(page);
	await expect(open).toHaveCount(0);
	await page.keyboard.press('Escape');

	state.task = { ...state.task, provider: 'claude-code' };
	emit('task.state_changed', state.task);
	await openTaskMenu(page);
	await expect(open).toBeVisible();
	await open.click();
	await expect.poll(() => state.terminals).toBe(1);
	await expect(page.getByRole('tab', { name: 'Claude Code' })).toBeVisible();
	await expect(
		page
			.getByRole('alert')
			.filter({ hasText: 'The agent is working on this task; open it in Claude Code when it stops' })
	).toBeVisible();

	state.task = { ...state.task, state: 'running', reason: 'Running' };
	emit('task.state_changed', state.task);
	await expect(tasks.getByRole('status').filter({ hasText: 'Running' })).toBeVisible();
	await openTaskMenu(page);
	await expect(open).toHaveCount(0);
});

test('each iteration says why it started: a retry after failure and review changes differ', async ({ page }) => {
	const { state } = await fixture(page);
	const time = '2026-10-26T10:00:00Z';
	const step = (sequence: number, from: string, to: string) => ({
		sequence,
		task_id: 'task-1',
		from_state: from,
		to_state: to,
		reason: '',
		created_at: time
	});
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
		generation: 2,
		failure_count: 0,
		checkout_id: 'checkout-1',
		run_id: 'run-2',
		created_at: time,
		updated_at: time
	};
	state.transitions = [
		step(1, '', 'queued'),
		step(2, 'queued', 'running'),
		step(3, 'running', 'failed'),
		step(4, 'failed', 'queued')
	];
	await page.goto('/test/assistant?conversation=chat-1');
	const card = page.getByRole('article', { name: 'Coding task' });
	await expect(card).toContainText('Iteration 2');
	await expect(card.getByTestId('iteration-reason')).toHaveText('retry after failure');

	state.transitions = [
		step(1, '', 'queued'),
		step(2, 'running', 'awaiting_review'),
		step(3, 'awaiting_review', 'queued')
	];
	state.signals = [
		{
			id: 'signal-1',
			task_id: 'task-1',
			run_id: 'run-1',
			kind: 'review',
			external_id: '1',
			outcome: 'changes_requested',
			name: 'octocat',
			summary: '',
			url: '',
			action: 'follow_up',
			follow_up_generation: 2,
			created_at: time,
			updated_at: time
		}
	];
	await page.reload();
	await expect(page.getByRole('article', { name: 'Coding task' }).getByTestId('iteration-reason')).toHaveText(
		'review changes'
	);
});

// A Claude Code run as the manager stores it: one log chunk per native line.
function claudeRun() {
	const at = (second: number) => `2026-10-26T10:01:${String(second).padStart(2, '0')}Z`;
	const lines: [any, number, boolean?][] = [
		[
			{
				type: 'system',
				subtype: 'init',
				cwd: '/workspace/repo',
				model: 'claude-opus-5-5',
				claude_code_version: '2.3.1'
			},
			0
		],
		[
			{
				type: 'assistant',
				message: { content: [{ type: 'thinking', thinking: '', signature: 'SIGNATURE-NEVER-SHOWN' }] },
				thinking_duration_ms: 2400
			},
			1
		],
		[
			{
				type: 'assistant',
				message: {
					content: [
						{
							type: 'tool_use',
							id: 'toolu_1',
							name: 'Bash',
							input: { command: 'cat AGENTS/SKILLS.md', description: 'Read skills index and tools' }
						}
					]
				}
			},
			2
		],
		[
			{
				type: 'assistant',
				message: { content: [{ type: 'tool_use', id: 'toolu_2', name: 'Bash', input: { command: 'npm test' } }] }
			},
			2
		],
		[
			{
				type: 'user',
				message: {
					content: [
						{
							tool_use_id: 'toolu_1',
							type: 'tool_result',
							content: '# Skill index\n<img src=x onerror="window.pwned=1">',
							is_error: false
						}
					]
				}
			},
			5
		],
		[
			{
				type: 'user',
				message: { content: [{ tool_use_id: 'toolu_2', type: 'tool_result', content: '1 failing', is_error: true }] }
			},
			9
		],
		['[Oversized output line omitted]', 10, true],
		[
			{
				type: 'assistant',
				message: { content: [{ type: 'text', text: 'Up to date with main; next migration is **000116**.' }] }
			},
			11
		]
	];
	return lines.map(([value, second, truncated], i) => ({
		id: i + 1,
		agent_run_id: 'run-1',
		stream: 'stdout',
		sequence: i + 1,
		content: (typeof value === 'string' ? value : JSON.stringify(value)) + '\n',
		truncated: !!truncated,
		created_at: at(second)
	}));
}

const runningTask = (over: any = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	prompt: 'Implement search',
	provider: 'claude-code',
	model: 'claude-opus-5-5',
	thinking: 'high',
	state: 'running',
	reason: 'Native coding harness started',
	generation: 1,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-1',
	created_at: '2026-10-26T10:00:00Z',
	updated_at: '2026-10-26T10:00:00Z',
	...over
});

test('the top bar opens the agent thread as a readable session, linked by the URL', async ({ page }, testInfo) => {
	const { state, emit } = await fixture(page);
	state.task = runningTask();
	state.logs = claudeRun();
	await page.goto('/test/assistant?conversation=chat-1');
	const target = page.getByRole('button', { name: /^Send to: / });
	await expect(target).toHaveAccessibleName('Send to: Assistant');
	const toggle = page.getByRole('button', { name: /^Agent · ENG-1/ });
	await expect(toggle.getByRole('img', { name: 'Running' })).toBeAttached();
	await toggle.click();
	await expect(page).toHaveURL(/conversation=chat-1&agent=task-1/);
	await expect(toggle).toHaveAttribute('aria-pressed', 'true');
	const crumbs = page.getByRole('navigation', { name: 'Location' });
	await expect(crumbs).toContainText('ENG-1 · Iteration 1');

	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const session = thread.getByRole('region', { name: 'Agent session' });
	// The header, thinking marker and the agent's message as Markdown.
	await expect(session.getByLabel('Session details')).toContainText('claude-opus-5-5');
	await expect(session.getByLabel('Session details')).toContainText('Claude Code 2.3.1');
	await expect(session.getByLabel('Session details')).toContainText('/workspace/repo');
	await expect(session.getByText('Thought for 2s')).toBeVisible();
	await expect(session.getByRole('article', { name: 'Agent' }).locator('strong')).toHaveText('000116');
	await expect(session.getByText('Output too large to show')).toBeVisible();
	await expect(page.getByText('SIGNATURE-NEVER-SHOWN')).toHaveCount(0);
	// Consecutive calls fold into one row; each opens to its command and result.
	const group = session.getByRole('button', { name: /Ran 2 commands/ });
	await expect(group).toContainText('1 failed');
	await group.click();
	const chip = session.getByRole('button', { name: /Read skills index and tools/ });
	await expect(chip).toContainText('3s');
	await chip.click();
	await expect(session.getByText('cat AGENTS/SKILLS.md')).toBeVisible();
	await expect(session.getByText('<img src=x onerror="window.pwned=1">', { exact: false })).toBeVisible();
	await expect(session.locator('img')).toHaveCount(0);
	expect(await page.evaluate(() => (window as any).pwned)).toBeUndefined();
	const failed = session.getByRole('button', { name: /Bash: npm test/ });
	await expect(failed).toHaveAccessibleName(/^Failed: /);
	await failed.click();
	await expect(session.getByText('1 failing')).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath('agent-view-desktop.png'), fullPage: true });

	// The composer writes to this agent while its thread is open.
	await expect(target).toHaveAccessibleName('Send to: ENG-1 · Claude Code agent');
	await expect(page.getByText('Messages go to the Claude Code agent of ENG-1, not to the assistant.')).toBeVisible();
	const composer = page.getByRole('textbox', { name: 'Write to the agent' });
	await composer.fill('Keep the old route');
	await composer.press('Enter');
	await expect.poll(() => state.liveSends).toEqual(['Keep the old route']);
	await expect(
		thread.getByRole('region', { name: 'Live messages' }).getByRole('listitem', { name: 'You' })
	).toContainText('Waiting for the agent');

	// New output streams in.
	state.logs = [
		...state.logs,
		{
			id: 9,
			agent_run_id: 'run-1',
			stream: 'stdout',
			sequence: 9,
			content:
				JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'Tests pass now.' }] } }) + '\n',
			truncated: false,
			created_at: '2026-10-26T10:01:20Z'
		}
	];
	emit('task.activity', {
		task_id: 'task-1',
		run_id: 'run-1',
		log_id: 9,
		stream: 'stdout',
		truncated: false,
		events: []
	});
	await expect(session.getByText('Tests pass now.')).toBeVisible();

	// Raw keeps the original output for debugging.
	const raw = session.getByRole('button', { name: 'Raw', exact: true });
	await raw.click();
	await expect(raw).toHaveAttribute('aria-pressed', 'true');
	await expect(session.getByText('"subtype":"init"', { exact: false })).toBeVisible();
	await expect(session.getByText('Thought for 2s')).toHaveCount(0);
	await raw.click();
	await expect(session.getByText('Thought for 2s')).toBeVisible();

	// A reload opens the same view; the breadcrumb goes back to the assistant.
	await page.reload();
	await expect(page.getByRole('navigation', { name: 'Location' })).toContainText('ENG-1 · Iteration 1');
	await expect(page.getByRole('region', { name: 'Agent session' }).getByText('Tests pass now.')).toBeVisible();
	await page.getByRole('navigation', { name: 'Location' }).getByRole('link', { name: 'Assistant' }).click();
	await expect(page).not.toHaveURL(/agent=/);
	await expect(page.getByRole('navigation', { name: 'Location' })).toHaveCount(0);
	await expect(page.getByRole('group', { name: 'Agent task for ENG-1' })).toBeVisible();
	await expect(target).toHaveAccessibleName('Send to: Assistant');
});

// An OpenCode run (`opencode run --format json`) as the manager stores it.
function openCodeRun() {
	const start = Date.parse('2026-10-26T10:01:00Z');
	const ids = { sessionID: 'ses_example0001', messageID: 'msg_example0001' };
	const event = (type: string, offset: number, part?: any) => ({
		type,
		timestamp: start + offset,
		sessionID: ids.sessionID,
		...(part ? { part: { id: `prt_${offset}`, ...ids, ...part } } : {})
	});
	const tool = (offset: number, name: string, callID: string, state: any) =>
		event('tool_use', offset, { type: 'tool', tool: name, callID, state });
	const stepEnd = (offset: number, reason: string) =>
		event('step_finish', offset, {
			reason,
			type: 'step-finish',
			tokens: { total: 1200, input: 1000, output: 150, reasoning: 50, cache: { write: 0, read: 800 } },
			cost: 0.0012
		});
	const lines = [
		event('step_start', 0, { type: 'step-start' }),
		event('text', 1000, {
			type: 'text',
			text: 'I will add the **search** filter.',
			time: { start, end: start + 1000 }
		}),
		tool(5000, 'bash', 'call_1', {
			status: 'completed',
			input: { command: 'npm test', description: 'Run the unit tests' },
			output: '12 passing\n<img src=x onerror="window.pwned=1">',
			metadata: { exit: 0, truncated: false },
			title: 'Run the unit tests',
			time: { start: start + 2000, end: start + 5000 }
		}),
		tool(5200, 'edit', 'call_2', {
			status: 'completed',
			input: { filePath: '/workspace/repo/src/search.ts', oldString: 'a', newString: 'b' },
			output: 'Edit applied successfully.',
			metadata: { diff: '-a\n+b\n', truncated: false },
			title: 'src/search.ts',
			time: { start: start + 5100, end: start + 5200 }
		}),
		tool(5300, 'bash', 'call_3', {
			status: 'error',
			input: { command: 'git push' },
			error: 'The user rejected permission to use this specific tool call.',
			time: { start: start + 5250, end: start + 5300 }
		}),
		stepEnd(5400, 'tool-calls'),
		event('session_compacted', 5450, { type: 'compaction', auto: true }),
		event('step_start', 5500, { type: 'step-start' }),
		event('text', 8000, {
			type: 'text',
			text: 'Search is in place.\n\n- filter added\n- tests pass',
			time: { start: start + 7000, end: start + 8000 }
		}),
		stepEnd(9000, 'stop')
	];
	return lines.map((value, i) => ({
		id: i + 1,
		agent_run_id: 'run-1',
		stream: 'stdout',
		sequence: i + 1,
		content: JSON.stringify(value) + '\n',
		truncated: false,
		// Recorded in one batch: durations come from OpenCode's own clock.
		created_at: '2026-10-26T10:01:09Z'
	}));
}

test('an OpenCode run reads as a session: Markdown messages and one chip per tool call', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = runningTask({ provider: 'opencode', model: 'opencode-go/example-model', state: 'awaiting_review' });
	state.logs = openCodeRun();
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const session = page.getByRole('region', { name: 'Agent session' });
	const messages = session.getByRole('article', { name: 'Agent' });
	await expect(messages).toHaveCount(2);
	await expect(messages.first().locator('strong')).toHaveText('search');
	await expect(messages.last().getByRole('listitem')).toHaveText(['filter added', 'tests pass']);
	await expect(session.getByText('Finished in 9s')).toBeVisible();
	// No native JSON in the readable view; what is not understood stays inspectable.
	await expect(session.getByText('step_finish', { exact: false })).toHaveCount(0);
	await session.getByRole('button', { name: '1 other events' }).click();
	await expect(session.getByText('session_compacted', { exact: true })).toBeVisible();

	// The calls fold into one row; each chip opens to its input and result.
	const group = session.getByRole('button', { name: /Used 3 tools/ });
	await expect(group).toContainText('1 failed');
	await group.click();
	const chip = session.getByRole('button', { name: /Run the unit tests/ });
	await expect(chip).toContainText('3s');
	await chip.click();
	await expect(session.getByText('npm test', { exact: true })).toBeVisible();
	await expect(session.getByText('12 passing', { exact: false })).toBeVisible();
	await expect(session.locator('img')).toHaveCount(0);
	expect(await page.evaluate(() => (window as any).pwned)).toBeUndefined();
	await expect(session.getByRole('button', { name: /edit: src\/search\.ts/ })).toBeVisible();
	const failed = session.getByRole('button', { name: /git push/ });
	await expect(failed).toHaveAccessibleName(/^Failed: /);
	await failed.click();
	await expect(session.getByText('The user rejected permission to use this specific tool call.')).toBeVisible();

	const raw = session.getByRole('button', { name: 'Raw', exact: true });
	await raw.click();
	await expect(session.getByText('"type":"step_finish"', { exact: false })).toBeVisible();
	await expect(session.getByText('"type":"session_compacted"', { exact: false })).toBeVisible();
});

test('a question from the agent is answered right under it in the agent view', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	state.task = runningTask({ provider: 'codex', model: 'deepseek-flash' });
	state.liveMessages = [agentQuestion];
	state.logs = [
		{
			id: 1,
			agent_run_id: 'run-1',
			stream: 'stdout',
			sequence: 1,
			content:
				[
					{ type: 'thread.started', thread_id: 'thread-1' },
					{
						type: 'item.completed',
						item: {
							id: 'i1',
							type: 'command_execution',
							command: "bash -lc 'rg -n search'",
							aggregated_output: 'src/a.ts',
							exit_code: 0,
							status: 'completed'
						}
					},
					{ type: 'item.completed', item: { id: 'i2', type: 'agent_message', text: 'Found the router.' } }
				]
					.map((event) => JSON.stringify(event))
					.join('\n') + '\n',
			truncated: false,
			created_at: '2026-10-26T10:01:00Z'
		}
	];
	// The issue identifier is a deep link to the conversation's newest task.
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1&agent=ENG-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(thread.getByRole('button', { name: /rg -n search/ })).toBeVisible();
	await expect(thread.getByRole('article', { name: 'Agent' })).toContainText('Found the router.');
	const question = thread.getByRole('region', { name: 'Live messages' }).getByRole('listitem', { name: 'Agent' });
	await expect(question).toContainText('Keep the v1 route?');
	// Pointed elsewhere first, the answer button aims the composer back at the agent.
	await page.getByRole('button', { name: 'Back to assistant' }).click();
	await expect(page.getByRole('button', { name: /^Send to: / })).toHaveAccessibleName('Send to: Assistant');
	await question.getByRole('button', { name: 'Answer the question' }).click();
	await expect(page.getByRole('button', { name: /^Send to: / })).toHaveAccessibleName('Send to: ENG-1 · Codex agent');
	await expect(page.getByRole('textbox', { name: 'Write to the agent' })).toBeFocused();
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
		await page.screenshot({ path: testInfo.outputPath(`agent-view-390-${dark ? 'dark' : 'light'}.png`) });
	}
});

test('several agent tasks are offered in a menu with their status', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = runningTask({ state: 'waiting_input', reason: 'Waiting for your answer', generation: 3 });
	state.otherTasks = [
		runningTask({
			id: 'task-2',
			run_id: 'run-9',
			state: 'failed',
			reason: 'Execution failed',
			provider: 'codex',
			created_at: '2026-10-26T09:00:00Z',
			updated_at: '2026-10-26T09:30:00Z'
		})
	];
	state.logs = claudeRun();
	await page.goto('/test/assistant?conversation=chat-1');
	await page.getByRole('button', { name: /^Agent · ENG-1/ }).click();
	const waiting = page.getByRole('menuitemcheckbox', { name: /ENG-1 · Claude Code · Iteration 3/ });
	await expect(waiting.getByRole('img', { name: 'Waiting for input' })).toBeAttached();
	const failed = page.getByRole('menuitemcheckbox', { name: /ENG-1 · Codex · Iteration 1/ });
	await expect(failed.getByRole('img', { name: 'Failed' })).toBeAttached();
	await waiting.click();
	await expect(page).toHaveURL(/agent=task-1/);
	await expect(page.getByRole('navigation', { name: 'Location' })).toContainText('ENG-1 · Iteration 3');
	await expect(page.getByRole('region', { name: 'Agent session' }).getByText('Thought for 2s')).toBeVisible();
});

test('an unknown agent in the link says so and leads back to the assistant', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = runningTask();
	await page.goto('/test/assistant?conversation=chat-1&agent=task-404');
	await expect(page.getByText('This agent task is not part of this conversation.')).toBeVisible();
	await page.getByRole('link', { name: 'Back to assistant' }).click();
	await expect(page).not.toHaveURL(/agent=/);
});

test('coding agent messages copy in full from the chat, the live thread and the session', async ({
	page,
	context
}) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	const { state } = await fixture(page);
	// Agent text is plain text: it is copied exactly as written, markup and all.
	const body = `Tests added.\n\n\`\`\`sh\nnpm test\n\`\`\`\n<b>raw</b> https://example.com/pr/1\n${'x'.repeat(5000)} end`;
	const message = { ...agentQuestion, kind: 'message', body };
	state.task = runningTask();
	state.logs = claudeRun();
	state.chatAgentMessages = [message];
	state.liveMessages = [message];
	await page.goto('/test/assistant?conversation=chat-1');
	const clipboard = () => page.evaluate(() => navigator.clipboard.readText());
	const copy = { name: 'Copy message' };

	const item = page.getByRole('article', { name: 'Message from the coding agent of ENG-1' });
	await item.getByRole('button', copy).focus();
	await page.keyboard.press('Enter');
	await expect.poll(clipboard).toBe(body);
	await expect(item.getByText('Copied', { exact: true })).toHaveCount(1);

	await page.getByRole('button', { name: /^Agent · ENG-1/ }).click();
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	// The session's message is copied as it reads, without its Markdown markers.
	await thread.getByRole('region', { name: 'Agent session' }).getByRole('article', { name: 'Agent' }).getByRole('button', copy).click();
	await expect.poll(clipboard).toBe('Up to date with main; next migration is 000116.');
	const live = thread.getByRole('region', { name: 'Live messages' }).getByRole('listitem', { name: 'Agent' });
	await live.getByRole('button', copy).click();
	await expect.poll(clipboard).toBe(body);
});
