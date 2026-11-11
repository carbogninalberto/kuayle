import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type Page, type TestInfo, type WebSocketRoute } from '@playwright/test';

// Talking to the running agent (KUA-132): the conversation with a task's
// agent lives in its timeline, written in the composer under it. Served from
// mocked API routes.
const time = '2026-11-11T10:00:00Z';
const at = (minute: number, second = 0) =>
	`2026-11-11T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`;

// Screenshots for a review go where KUA132_SHOTS says; otherwise with the test's output.
const shot = (testInfo: TestInfo, name: string) => {
	const dir = process.env.KUA132_SHOTS;
	if (!dir) return testInfo.outputPath(name);
	mkdirSync(dir, { recursive: true });
	return join(dir, name);
};

const text = (value: string) => ({ type: 'assistant', message: { content: [{ type: 'text', text: value }] } });
const bash = (id: string, command: string, description: string) => ({
	type: 'assistant',
	message: { content: [{ type: 'tool_use', id, name: 'Bash', input: { command, description } }] }
});
const result = (id: string, content: string) => ({
	type: 'user',
	message: { content: [{ tool_use_id: id, type: 'tool_result', content, is_error: false }] }
});
// One recorded line a minute, from `minute` on.
const lines = (run: string, minute: number, values: any[]) =>
	values.map((value, i) => ({
		id: i + 1,
		agent_run_id: run,
		stream: 'stdout',
		sequence: i + 1,
		content: JSON.stringify(value) + '\n',
		truncated: false,
		created_at: at(minute + i)
	}));

const baseTask = (over: any = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	kind: 'implement',
	prompt: 'Add search to the issue list.',
	provider: 'claude-code',
	model: 'claude-opus-5-5',
	thinking: '',
	state: 'running',
	reason: 'Native coding harness started',
	generation: 2,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl',
	workflow_version_id: 'version-1',
	workflow_node: 'implement',
	workflow_status: 'running',
	created_at: time,
	updated_at: at(4),
	...over
});
const node = (id: string, type: string, label: string, profile_id?: string) => ({
	id,
	type,
	label,
	profile_id,
	position: { x: 0, y: 0 }
});
const step = (sequence: number, node_id: string, node_type: string, entered: number, over: any = {}) => ({
	task_id: 'task-1',
	sequence,
	node_id,
	node_type,
	visit: 1,
	reason: '',
	detail: '',
	entered_at: at(entered),
	...over
});
const run = (id: string, sequence: number, provider: string, status: string, over: any = {}) => ({
	id,
	task_id: 'task-1',
	step: sequence,
	provider,
	model: provider === 'claude-code' ? 'claude-opus-5-5' : 'gpt-5',
	status,
	created_at: at(4),
	...over
});
const profile = (id: string, name: string, harness: string, behaviour: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: '',
	behaviour,
	harness,
	model: '',
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: time,
	updated_at: time
});
// A planner finished; the implementer is on its step.
const workflow = (harness = 'claude-code') => ({
	workflow_id: 'workflow-1',
	name: 'Plan and build',
	version: 1,
	builtin: false,
	graph: {
		nodes: [node('start', 'start', 'Start'), node('plan', 'agent', 'Plan', 'p-plan'), node('implement', 'agent', 'Implement', 'p-impl')],
		edges: [],
		budget: {}
	},
	node: 'implement',
	status: 'running',
	visits: { start: 1, plan: 1, implement: 1 },
	iterations: 2,
	sequence: 3,
	steps: [
		step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
		step(2, 'plan', 'agent', 0, { generation: 1, outcome: 'agent_finished', finished_at: at(4), plan: 'Add the endpoint, then the tests.' }),
		step(3, 'implement', 'agent', 4, { generation: 2 })
	] as any[],
	runs: [
		run('run-plan', 2, 'claude-code', 'succeeded', { created_at: at(0), completed_at: at(4) }),
		run('run-impl', 3, harness, 'running')
	] as any[],
	profiles: [profile('p-plan', 'Planner', 'claude-code', 'plans'), profile('p-impl', 'Builder', harness, 'changes_code')],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 2
});

async function fixture(page: Page) {
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
	const issue = { id: 'issue-1', identifier: 'ENG-1', title: 'Implement search', status: 'todo', description: '' };
	const state = {
		task: baseTask() as any,
		workflow: workflow() as any,
		logs: {
			'run-plan': lines('run-plan', 1, [text('Add the endpoint, then the tests.')]),
			'run-impl': lines('run-impl', 5, [
				text('Starting with the endpoint.'),
				bash('c1', 'rg -n router src', 'Find the router'),
				result('c1', 'src/router.ts'),
				bash('c2', 'npm test', 'Run the unit tests'),
				result('c2', '12 passing')
			])
		} as Record<string, any[]>,
		liveMessages: [] as any[],
		// A task without a workflow lists its attempts.
		runs: [] as any[],
		// What the composer posted: the body and how it was sent.
		sends: [] as { body: string; mode?: string }[],
		// The next send is refused with this.
		refuse: null as null | { status: number; json: any },
		// What a send with a mode does to the task, as the server would.
		onSend: null as null | ((mode: string | undefined) => void),
		sockets: [] as WebSocketRoute[],
		events: [] as any[]
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', (socket) => state.sockets.push(socket));
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url()),
			path = url.pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status'))
			return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path.endsWith('/task-options')) return route.fulfill({ json: { providers: [], model_servers: [] } });
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [state.task] } });
		if (path.endsWith('/events'))
			return route.fulfill({ json: state.events.filter((e) => e.cursor > Number(url.searchParams.get('after') ?? 0)) });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({
				json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false }
			});
		const match = path.match(/\/agent-tasks\/task-1(\/.*)?$/);
		if (match) {
			const rest = match[1] ?? '';
			if (rest === '/workflow') return route.fulfill({ json: state.workflow });
			if (rest === '/runs') return route.fulfill({ json: { runs: state.runs } });
			if (rest === '/live-messages' && route.request().method() === 'POST') {
				const input = route.request().postDataJSON();
				if (state.refuse) {
					const refused = state.refuse;
					state.refuse = null;
					return route.fulfill(refused);
				}
				state.sends.push(input);
				const seq = state.liveMessages.filter((item) => item.direction === 'to_agent').length + 1;
				const sent = {
					id: `to-${seq}`,
					task_id: 'task-1',
					direction: 'to_agent',
					seq,
					sender: 'user',
					author_id: 'user-1',
					kind: 'message',
					body: input.body,
					state: 'queued',
					delivered_via: null,
					run_id: null,
					reject_reason: null,
					created_at: at(9, seq),
					delivered_at: null,
					reply_to: null,
					deliveries: 0
				};
				state.liveMessages = [...state.liveMessages, sent];
				state.onSend?.(input.mode);
				return route.fulfill({ status: 201, json: sent });
			}
			if (rest === '/live-messages') return route.fulfill({ json: { messages: state.liveMessages } });
			const logs = rest.match(/^\/runs\/([^/]+)\/logs$/);
			if (logs) {
				const all = (state.logs[logs[1]] ?? []).filter((log) => log.id > Number(url.searchParams.get('after') ?? 0));
				return route.fulfill({ json: { logs: all } });
			}
			if (rest.endsWith('/validation')) return route.fulfill({ json: null });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/history') return route.fulfill({ json: { transitions: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/publication') return route.fulfill({ json: { step: null } });
			if (rest === '/signals') return route.fulfill({ json: [] });
			return route.fulfill({ json: state.task });
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
	};
	// The machine manager writes the message to the running attempt's inbox.
	const deliver = (seq: number, when: string, runId = 'run-impl', via = 'inbox') => {
		state.liveMessages = state.liveMessages.map((item) =>
			item.seq === seq
				? { ...item, state: 'delivered', delivered_via: via, run_id: runId, delivered_at: when, deliveries: 1 }
				: item
		);
		emit('task.live_message', state.liveMessages.find((item) => item.seq === seq));
	};
	// The agent's outbox file is recorded: a reply when it names a message.
	const agentSays = (id: string, body: string, minute: number, over: any = {}) => {
		const message = {
			id,
			task_id: 'task-1',
			direction: 'from_agent',
			seq: null,
			sender: 'agent',
			author_id: null,
			kind: 'message',
			body,
			state: 'received',
			delivered_via: null,
			run_id: 'run-impl',
			reject_reason: null,
			created_at: at(minute),
			delivered_at: null,
			reply_to: null,
			deliveries: 0,
			identifier: 'ENG-1',
			...over
		};
		state.liveMessages = [...state.liveMessages, message];
		emit('task.live_message', message);
	};
	return { state, emit, deliver, agentSays };
}

const agentUrl = '/test/assistant?conversation=chat-1&agent=task-1';
const thread = (page: Page) => page.getByRole('region', { name: 'Agent thread of ENG-1' });
const composer = (page: Page) => page.getByRole('textbox', { name: 'Write to the agent' });
// The view's own controls: the side panel's task card has a thread and a Send of its own.
const composerButton = (page: Page, name: string) => page.getByTestId('agent-composer').getByRole('button', { name, exact: true });
const tooltip = (page: Page) => page.locator('[data-slot="tooltip-content"]');
const noOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test('a message sent from the running step is shown inline with its state, and the reply is the next item', async ({
	page
}, testInfo) => {
	const { state, deliver, agentSays } = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto(agentUrl);
	const running = page.locator('#step-3');
	await expect(running.getByRole('button', { name: /Ran 2 commands/ })).toBeVisible();

	// A transcript and a composer: no thread of its own, no target chip, no helper sentence.
	await expect(composer(page)).toBeVisible();
	await expect(thread(page).getByRole('region', { name: 'Live messages' })).toHaveCount(0);
	await expect(page.getByRole('button', { name: /^Send to: / })).toHaveCount(0);
	await expect(page.getByTestId('agent-composer').getByText('The running agent reads it after its current step.')).toHaveCount(0);
	await expect(thread(page).getByText('The running agent reads it after its current step.')).toHaveCount(0);
	await expect(page.getByText(/not to the assistant/)).toHaveCount(0);
	await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(0);
	await expect(composerButton(page, 'Send')).toBeDisabled();

	// Enter sends straight to the agent of the step: no approval, no mode.
	await composer(page).fill('Keep the v1 route working');
	await composer(page).press('Enter');
	await expect.poll(() => state.sends).toEqual([{ body: 'Keep the v1 route working' }]);
	await expect(composer(page)).toHaveValue('');
	await expect(composer(page)).toBeFocused();
	const mine = running.getByRole('article', { name: 'You' });
	await expect(mine).toContainText('Keep the v1 route working');
	await expect(mine).toHaveAttribute('data-state', 'queued');
	await expect(mine.getByRole('img', { name: 'Waiting for the agent' })).toBeVisible();
	await expect(page.locator('#step-2').getByRole('article', { name: 'You' })).toHaveCount(0);

	// Delivered: the icon changes, and the message takes its place in the session.
	deliver(1, at(5, 30));
	await expect(mine).toHaveAttribute('data-state', 'delivered');
	await expect(mine.getByRole('img', { name: 'Delivered while it worked' })).toBeVisible();
	await expect(mine.locator('time')).toHaveAttribute('datetime', at(5, 30));

	// The agent's reply names the message: it is the next item, and the message reads as replied.
	agentSays('reply-1', 'Understood: the v1 route stays. I am reverting its removal <b>now</b>.', 8, { reply_to: 1 });
	await expect(mine).toHaveAttribute('data-state', 'replied');
	await expect(mine.getByRole('img', { name: 'Replied' })).toBeVisible();
	const reply = running.getByRole('article', { name: 'Agent' }).filter({ hasText: 'the v1 route stays' });
	await expect(reply).toBeVisible();
	// Agent text is untrusted: shown as written, never as markup.
	await expect(reply).toContainText('<b>now</b>');
	await expect(reply.locator('b')).toHaveCount(0);
	const order = await running
		.locator('[role="article"], [data-slot="trace"]')
		.evaluateAll((items) => items.map((item) => item.getAttribute('aria-label') ?? 'trace'));
	const sent = order.indexOf('You');
	expect(sent).toBeGreaterThan(-1);
	expect(order[sent + 1]).toBe('Agent');
	// It was delivered between the agent's first message and its commands.
	expect(order.slice(0, sent)).toContain('Agent');
	expect(order.slice(sent + 2)).toContain('trace');
	// The state is named on demand.
	await mine.getByRole('img', { name: 'Replied' }).hover();
	await expect(tooltip(page)).toContainText('Replied');
	await page.mouse.move(0, 0);
	await expect(tooltip(page)).toHaveCount(0);
	expect(await noOverflow(page)).toBeTruthy();
	await page.screenshot({ path: shot(testInfo, '1-message-and-reply-desktop.png') });

	// A second message is delivered and still waits for its reply.
	await composer(page).fill('And use the shared HTTP client');
	await composer(page).press('Enter');
	deliver(2, at(11));
	const waiting = running.getByRole('article', { name: 'You' }).filter({ hasText: 'shared HTTP client' });
	await expect(waiting).toHaveAttribute('data-state', 'delivered');
	await expect(waiting.getByRole('img', { name: 'Delivered while it worked' })).toBeVisible();
	await expect(running.getByRole('img', { name: 'No reply' })).toHaveCount(0);
	await page.mouse.move(0, 0);
	await expect(tooltip(page)).toHaveCount(0);
	await page.screenshot({ path: shot(testInfo, '2-waiting-for-reply.png') });

	// A reload shows the same conversation.
	await page.reload();
	await expect(page.locator('#step-3').getByRole('article', { name: 'You' })).toHaveCount(2);
	await expect(page.locator('#step-3').getByRole('img', { name: 'Replied' })).toBeVisible();
});

test('a message the agent never answered is flagged while the work goes on, also on a closed step', async ({ page }) => {
	const { state } = await fixture(page);
	// The attempt succeeded and its work was published: a missing reply never fails it.
	state.task = baseTask({ state: 'awaiting_review', reason: 'Pull request opened; waiting for review', workflow_status: 'waiting' });
	state.workflow = {
		...workflow(),
		status: 'waiting',
		steps: workflow().steps.map((item: any) => (item.sequence === 3 ? { ...item, outcome: 'agent_finished', finished_at: at(12) } : item)),
		runs: [workflow().runs[0], run('run-impl', 3, 'claude-code', 'succeeded', { completed_at: at(12) })]
	};
	state.liveMessages = [
		{
			id: 'to-1',
			task_id: 'task-1',
			direction: 'to_agent',
			seq: 1,
			sender: 'orchestrator',
			author_id: 'user-1',
			kind: 'message',
			body: 'Keep the v1 route working',
			state: 'delivered',
			delivered_via: 'inbox',
			run_id: 'run-impl',
			reject_reason: null,
			created_at: at(6),
			delivered_at: at(6),
			reply_to: null,
			deliveries: 1
		}
	];
	await page.goto(agentUrl);
	const step = page.locator('#step-3');
	const message = step.getByRole('article', { name: 'Assistant for you' });
	await expect(message).toHaveAttribute('data-state', 'unanswered');
	await expect(message.getByRole('img', { name: 'No reply' })).toBeVisible();
	await expect(step).toHaveAttribute('data-status', 'done');
	await expect(thread(page).getByRole('status').filter({ hasText: 'Awaiting review' })).toBeVisible();
	// Closed, the step still says a message went unanswered.
	const header = step.getByRole('button').first();
	await header.click();
	await expect(message).toHaveCount(0);
	await expect(header.getByRole('img', { name: 'No reply' })).toBeVisible();
});

test('interrupt and send stops the turn and the message is taken up in the same step', async ({ page }, testInfo) => {
	const { state, emit, deliver } = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto(agentUrl);
	await expect(page.locator('#step-3').getByRole('button', { name: /Ran 2 commands/ })).toBeVisible();
	const interrupt = composerButton(page, 'Interrupt and send');
	await expect(interrupt).toBeDisabled();
	await expect(composerButton(page, 'Send and start now')).toHaveCount(0);
	await composer(page).fill('Stop: the migration must not drop the column');
	await expect(interrupt).toBeEnabled();
	// Its shortcut is shown with its name, and announced.
	await expect(interrupt).toHaveAttribute('aria-keyshortcuts', 'Control+Enter Meta+Enter');
	await interrupt.hover();
	await expect(tooltip(page)).toContainText('Interrupt and send');
	await expect(tooltip(page).locator('[data-slot="kbd"]')).toHaveText([/^(Ctrl|⌘)$/, '↵']);
	await page.screenshot({ path: shot(testInfo, '3-interrupt-and-send.png') });

	// One action: the server stops the attempt and resumes its session.
	state.onSend = (mode) => {
		if (mode !== 'interrupt') return;
		state.task = { ...state.task, state: 'cancelling', reason: 'Interrupting: stopping the agent to deliver a message', interrupting: true };
	};
	await interrupt.click();
	await expect.poll(() => state.sends).toEqual([{ body: 'Stop: the migration must not drop the column', mode: 'interrupt' }]);
	await expect(composer(page)).toHaveValue('');
	await expect(thread(page).getByText('Interrupting: stopping the agent to deliver a message')).toBeVisible();
	// The composer stays: what is said meanwhile joins the message.
	await expect(composer(page)).toBeVisible();
	await expect(interrupt).toHaveCount(0);

	// The step continues as a new attempt, whose prompt carries the message.
	state.task = baseTask({ generation: 3, run_id: 'run-impl-2', state: 'running', reason: 'Native coding harness started', updated_at: at(11) });
	state.workflow = {
		...state.workflow,
		steps: state.workflow.steps.map((item: any) => (item.sequence === 3 ? { ...item, generation: 3 } : item)),
		runs: [
			state.workflow.runs[0],
			run('run-impl', 3, 'claude-code', 'cancelled', { completed_at: at(10) }),
			run('run-impl-2', 3, 'claude-code', 'running', { created_at: at(11) })
		]
	};
	state.logs['run-impl-2'] = lines('run-impl-2', 12, [text('Keeping the column; rewriting the migration.')]);
	emit('task.state_changed', state.task);
	deliver(1, at(11), 'run-impl-2', 'prompt');
	const step = page.locator('#step-3');
	const mine = step.getByRole('article', { name: 'You' });
	await expect(mine).toContainText('the migration must not drop the column');
	await expect(mine.getByRole('img', { name: 'Delivered at the next iteration' })).toBeVisible();
	await expect(step.getByText('Keeping the column; rewriting the migration.')).toBeVisible();
	await expect(page.locator('ol[aria-label] > li section[data-step="agent"]')).toHaveCount(2, { timeout: 10_000 });
	await expect(interrupt).toBeVisible();

	// Ctrl+Enter is the same action from the keyboard.
	state.onSend = null;
	await composer(page).fill('Also keep the index');
	await composer(page).press('Control+Enter');
	await expect.poll(() => state.sends.at(-1)).toEqual({ body: 'Also keep the index', mode: 'interrupt' });
});

test('a task without a workflow is interrupted like any other', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = baseTask({ workflow_version_id: null, workflow_node: null, workflow_status: null, run_id: 'run-1', generation: 1 });
	state.runs = [{ id: 'run-1', provider_id: 'claude-code', model: 'claude-opus-5-5', status: 'running', created_at: at(4), started_at: at(4) }];
	state.logs['run-1'] = lines('run-1', 5, [text('Starting with the endpoint.')]);
	await page.goto(agentUrl);
	await expect(page.locator('#run-run-1').getByText('Starting with the endpoint.')).toBeVisible();
	const interrupt = composerButton(page, 'Interrupt and send');
	await composer(page).fill('Stop: wrong branch');
	await expect(interrupt).toBeEnabled();
	await interrupt.click();
	await expect.poll(() => state.sends).toEqual([{ body: 'Stop: wrong branch', mode: 'interrupt' }]);
	await expect(page.locator('#run-run-1').getByRole('article', { name: 'You' })).toContainText('Stop: wrong branch');
});

test('starting now on a waiting task asks once, naming what it gives up', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = baseTask({ state: 'awaiting_review', reason: 'Workflow · waiting for CI on the pull request', workflow_status: 'waiting' });
	state.workflow = {
		...workflow(),
		status: 'waiting',
		steps: workflow().steps.map((item: any) => (item.sequence === 3 ? { ...item, outcome: 'agent_finished', finished_at: at(12) } : item)),
		runs: [workflow().runs[0], run('run-impl', 3, 'claude-code', 'succeeded', { completed_at: at(12) })]
	};
	await page.goto(agentUrl);
	await expect(page.locator('#step-3')).toBeVisible();
	const start = composerButton(page, 'Send and start now');
	await composer(page).fill('CI is flaky: rerun with the retry flag');
	// The shortcut does not act behind the person's back: it asks, as the button does.
	await composer(page).press('Control+Enter');
	const dialog = page.getByRole('alertdialog', { name: 'Start a new iteration now?' });
	await expect(dialog).toContainText('The task is waiting: Workflow · waiting for CI on the pull request.');
	await expect(dialog).toContainText('Starting now gives that up');
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).toHaveCount(0);
	expect(state.sends).toEqual([]);
	await expect(composer(page)).toHaveValue('CI is flaky: rerun with the retry flag');
	// Enter alone only queues it; nothing is started.
	await start.click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole('button', { name: 'Send and start now' }).click();
	await expect.poll(() => state.sends).toEqual([{ body: 'CI is flaky: rerun with the retry flag', mode: 'start' }]);
	await expect(dialog).toHaveCount(0);
	await expect(composer(page)).toHaveValue('');
});

test('an agent question is answered from where it was asked, and the answer names it', async ({ page }) => {
	const { state, agentSays } = await fixture(page);
	await page.goto(agentUrl);
	const running = page.locator('#step-3');
	await expect(running.getByRole('button', { name: /Ran 2 commands/ })).toBeVisible();
	agentSays('question-1', 'Keep the v1 route?\nIt is still used by the mobile app.', 8, { kind: 'question' });
	const question = running.getByRole('article', { name: 'Agent' }).filter({ hasText: 'Keep the v1 route?' });
	await expect(question.getByRole('img', { name: 'Question' })).toBeVisible();
	// One obvious way to answer: the button brings the cursor to the composer with the question as its target.
	await question.getByRole('button', { name: 'Answer the question' }).click();
	await expect(composer(page)).toBeFocused();
	const target = page.getByTestId('agent-question');
	await expect(target).toContainText('Keep the v1 route?');
	await page.keyboard.type('Yes, keep it.');
	await page.keyboard.press('Enter');
	await expect
		.poll(() => state.sends)
		.toEqual([{ body: 'In answer to your question "Keep the v1 route? It is still used by the mobile app.":\nYes, keep it.' }]);
	await expect(target).toHaveCount(0);
	await expect(running.getByRole('article', { name: 'You' })).toContainText('Yes, keep it.');
	// The target can be dropped: the next message is then a plain one.
	await question.getByRole('button', { name: 'Answer the question' }).click();
	await target.getByRole('button', { name: 'Dismiss' }).click();
	await expect(target).toHaveCount(0);
	await composer(page).fill('Unrelated');
	await composer(page).press('Enter');
	await expect.poll(() => state.sends.at(-1)).toEqual({ body: 'Unrelated' });
});

test('a harness without a mid-turn hook has one action: its message interrupts it', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = baseTask({ provider: 'codex', model: 'gpt-5' });
	state.workflow = workflow('codex');
	state.logs['run-impl'] = [];
	await page.goto(agentUrl);
	await expect(composer(page)).toBeVisible();
	await composer(page).fill('Use the shared client');
	await expect(composerButton(page, 'Send')).toBeEnabled();
	await expect(composerButton(page, 'Interrupt and send')).toHaveCount(0);
	await composer(page).press('Control+Enter');
	await expect.poll(() => state.sends).toEqual([{ body: 'Use the shared client' }]);
});

test('with no step running a message waits after the last step, or starts an iteration now', async ({ page }) => {
	const { state, emit, deliver } = await fixture(page);
	state.task = baseTask({ state: 'failed', reason: 'Native coding iteration failed; stopping execution before retry' });
	state.workflow = {
		...workflow(),
		status: 'failed',
		runs: [workflow().runs[0], run('run-impl', 3, 'claude-code', 'failed', { completed_at: at(8) })]
	};
	await page.goto(agentUrl);
	await expect(page.locator('#step-3').getByRole('button', { name: /Ran 2 commands/ })).toBeVisible();
	const start = composerButton(page, 'Send and start now');
	await expect(start).toBeDisabled();
	await expect(composerButton(page, 'Interrupt and send')).toHaveCount(0);

	// A plain send waits for the next iteration, where that one will pick it up.
	await composer(page).fill('Try the other fixture');
	await composer(page).press('Enter');
	await expect.poll(() => state.sends).toEqual([{ body: 'Try the other fixture' }]);
	const pending = thread(page).locator('[data-step="pending"]');
	await expect(pending.getByRole('article', { name: 'You' })).toContainText('Try the other fixture');
	await expect(pending.getByRole('img', { name: 'Waiting for the agent' })).toBeVisible();
	await expect(page.locator('#step-3').getByRole('article', { name: 'You' })).toHaveCount(0);
	// It follows the last step.
	expect(
		await thread(page)
			.locator('ol[aria-label] > li')
			.evaluateAll((items) => items.at(-1)?.getAttribute('data-step'))
	).toBe('pending');

	// A refusal is said where it happened, and the text is kept.
	state.refuse = { status: 409, json: { error: { code: 'TASK_CONFLICT', message: 'This request conflicts with the current task' } } };
	await composer(page).fill('And skip the flaky test');
	await start.click();
	await expect(page.getByTestId('agent-composer').getByRole('alert')).toContainText('This request conflicts with the current task');
	await expect(composer(page)).toHaveValue('And skip the flaky test');

	// Send and start now: an iteration starts and takes both messages.
	state.onSend = (mode) => {
		if (mode !== 'start') return;
		state.task = baseTask({ generation: 3, run_id: 'run-impl-2', state: 'queued', reason: "Started to deliver a message from the task's owner" });
	};
	await start.click();
	await expect.poll(() => state.sends.at(-1)).toEqual({ body: 'And skip the flaky test', mode: 'start' });
	await expect(page.getByTestId('agent-composer').getByRole('alert')).toHaveCount(0);
	await expect(thread(page).getByText("Started to deliver a message from the task's owner")).toBeVisible();
	state.task = { ...state.task, state: 'running' };
	state.workflow = {
		...state.workflow,
		status: 'running',
		sequence: 4,
		steps: [
			...state.workflow.steps.map((item: any) => (item.sequence === 3 ? { ...item, outcome: 'retried', finished_at: at(9) } : item)),
			step(4, 'implement', 'agent', 10, { generation: 3, visit: 2 })
		],
		runs: [...state.workflow.runs, run('run-impl-2', 4, 'claude-code', 'running', { created_at: at(10) })]
	};
	state.logs['run-impl-2'] = lines('run-impl-2', 11, [text('Switching fixtures.')]);
	emit('task.state_changed', state.task);
	deliver(1, at(10), 'run-impl-2', 'prompt');
	deliver(2, at(10, 1), 'run-impl-2', 'prompt');
	const resumed = page.locator('#step-4');
	await expect(resumed.getByRole('article', { name: 'You' })).toHaveCount(2);
	await expect(pending).toHaveCount(0);
	await expect(start).toHaveCount(0);
	await expect(composerButton(page, 'Interrupt and send')).toBeVisible();
});

test('a finished task and another person\'s task have no composer', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = baseTask({ state: 'completed', reason: 'Merged' });
	state.workflow = { ...workflow(), status: 'done' };
	await page.goto(agentUrl);
	await expect(page.locator('#step-3')).toBeVisible();
	await expect(composer(page)).toHaveCount(0);
	// The assistant's composer does not stand in for it.
	await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(0);

	state.task = baseTask({ creator_id: 'user-2' });
	state.workflow = workflow();
	await page.reload();
	await expect(page.locator('#step-3')).toBeVisible();
	await expect(composer(page)).toHaveCount(0);
});

test('the conversation fits a phone', async ({ page }, testInfo) => {
	const { state, deliver, agentSays } = await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(agentUrl);
	const running = page.locator('#step-3');
	await expect(running.getByRole('button', { name: /Ran 2 commands/ })).toBeVisible();
	await composer(page).fill('Keep the v1 route working, and do not rename the query parameter that the mobile app still sends');
	await composerButton(page, 'Send').click();
	await expect.poll(() => state.sends.length).toBe(1);
	deliver(1, at(5, 30));
	agentSays('reply-1', 'Understood: the v1 route stays and `q` keeps its name. I am reverting the rename now.', 8, { reply_to: 1 });
	const mine = running.getByRole('article', { name: 'You' });
	await expect(mine.getByRole('img', { name: 'Replied' })).toBeVisible();
	await expect(running.getByRole('article', { name: 'Agent' }).filter({ hasText: 'keeps its name' })).toBeVisible();
	await expect(composer(page)).toBeInViewport();
	await expect(composerButton(page, 'Interrupt and send')).toBeVisible();
	await page.mouse.move(0, 0);
	await expect(tooltip(page)).toHaveCount(0);
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		expect(await noOverflow(page)).toBeTruthy();
		if (!dark) await page.screenshot({ path: shot(testInfo, '4-message-and-reply-phone.png') });
	}
	// The bubble does not push the page wider than the screen.
	const box = await mine.boundingBox();
	expect(box!.x).toBeGreaterThanOrEqual(0);
	expect(box!.x + box!.width).toBeLessThanOrEqual(390);
});
