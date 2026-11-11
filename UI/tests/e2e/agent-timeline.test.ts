import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

// The agent view as a timeline of workflow steps (KUA-126): the conversation
// with the coding agents of one task, served from mocked API routes.
const time = '2026-10-26T10:00:00Z';
const at = (minute: number, second = 0) =>
	`2026-10-26T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`;

const text = (value: string) => ({ type: 'assistant', message: { content: [{ type: 'text', text: value }] } });
const bash = (id: string, command: string, description: string) => ({
	type: 'assistant',
	message: { content: [{ type: 'tool_use', id, name: 'Bash', input: { command, description } }] }
});
const result = (id: string, content: string) => ({
	type: 'user',
	message: { content: [{ tool_use_id: id, type: 'tool_result', content, is_error: false }] }
});
const lines = (run: string, minute: number, values: any[]) =>
	values.map((value, i) => ({
		id: i + 1,
		agent_run_id: run,
		stream: 'stdout',
		sequence: i + 1,
		content: JSON.stringify(value) + '\n',
		truncated: false,
		created_at: at(minute, i)
	}));

const prompt = ['Add search to the issue list.', 'Keep the old route working.', 'Cover it with tests.', 'Write the docs.', 'LAST-LINE-OF-THE-REQUEST'].join(
	'\n'
);
const baseTask = (over: any = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	prompt,
	provider: 'opencode',
	model: 'kimi-k2',
	thinking: '',
	state: 'running',
	reason: 'Native coding harness started',
	generation: 3,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl-2',
	created_at: time,
	updated_at: at(18),
	...over
});

const profile = (id: string, name: string, harness: string, model: string, behaviour: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: '',
	behaviour,
	harness,
	model,
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: time,
	updated_at: time
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
const run = (id: string, sequence: number, provider: string, model: string, status: string, over: any = {}) => ({
	id,
	task_id: 'task-1',
	step: sequence,
	provider,
	model,
	status,
	created_at: at(0),
	...over
});

// The path of a task like KUA-122: an orchestrator plans, a cheap implementer
// codes, the work is published and reviewed, and the implementer is on its
// second visit.
const workflow = () => ({
	workflow_id: 'workflow-1',
	name: 'Plan and review',
	version: 2,
	builtin: false,
	graph: {
		nodes: [
			node('start', 'start', 'Start'),
			node('plan', 'agent', 'Plan', 'p-orch'),
			node('implement', 'agent', 'Implement', 'p-impl'),
			node('publish', 'publish', 'Publish'),
			node('review', 'agent', 'Review', 'p-rev')
		],
		edges: [],
		budget: {}
	},
	node: 'implement',
	status: 'running',
	visits: { start: 1, plan: 1, implement: 2, publish: 1, review: 1 },
	iterations: 3,
	sequence: 6,
	steps: [
		step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
		step(2, 'plan', 'agent', 0, {
			generation: 1,
			outcome: 'agent_finished',
			finished_at: at(4),
			plan: 'Add a **search** box, then:\n\n1. the endpoint\n2. the tests'
		}),
		step(3, 'implement', 'agent', 4, { generation: 2, outcome: 'agent_finished', finished_at: at(12, 30) }),
		step(4, 'publish', 'publish', 12, { outcome: 'published', finished_at: at(13) }),
		step(5, 'review', 'agent', 13, {
			review_task_id: 'review-1',
			outcome: 'review_changes_requested',
			finished_at: at(18)
		}),
		step(6, 'implement', 'agent', 18, { generation: 3, visit: 2, reason: 'Changes requested' })
	],
	runs: [
		run('run-plan', 2, 'claude-code', 'claude-opus-5-5', 'succeeded', { completed_at: at(4) }),
		run('run-impl-1', 3, 'opencode', 'kimi-k2', 'succeeded', { completed_at: at(12) }),
		run('run-review', 5, 'codex', 'gpt-5', 'succeeded', { task_id: 'review-1', completed_at: at(17) }),
		run('run-impl-2', 6, 'opencode', 'kimi-k2', 'running')
	],
	profiles: [
		profile('p-orch', 'Orchestrator', 'claude-code', 'claude-opus-5-5', 'plans'),
		profile('p-impl', 'Cheap Implementer', 'opencode', 'kimi-k2', 'changes_code'),
		profile('p-rev', 'Reviewer', 'codex', 'gpt-5', 'reviews')
	],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 3
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
		// Tasks the conversation does not list but serves by id (a step's review).
		tasks: {} as Record<string, any>,
		workflow: null as any,
		workflowRequests: 0,
		cancels: 0,
		cancelRefused: false,
		// Every agent-task request, as "<task>/<rest>?<query>".
		requests: [] as string[],
		// Runs whose records do not exist (yet, or any more).
		missing: [] as string[],
		// A task without a workflow lists its attempts.
		runs: [] as any[],
		logs: {} as Record<string, any[]>,
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
		const match = path.match(/\/agent-tasks\/([^/]+)(\/.*)?$/);
		if (match) {
			const [, id, rest = ''] = match;
			const task = id === state.task.id ? state.task : state.tasks[id];
			if (!task) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			state.requests.push(`${id}${rest}${url.search}`);
			if (rest === '/workflow') {
				state.workflowRequests++;
				// Summaries of runs the reader already has are not sent again.
				const since = url.searchParams.get('summaries_since');
				const runs = state.workflow.runs.map((item: any) =>
					since && item.completed_at && Date.parse(item.completed_at) <= Date.parse(since)
						? { ...item, summary: undefined }
						: item
				);
				return route.fulfill({ json: { ...state.workflow, runs } });
			}
			if (rest === '/cancel' && route.request().method() === 'POST') {
				state.cancels++;
				if (state.cancelRefused)
					return route.fulfill({ status: 409, json: { error: { code: 'TASK_CONFLICT', message: 'This request conflicts with the current task' } } });
				state.task = { ...state.task, state: 'cancelling', reason: 'Stopping native execution' };
				return route.fulfill({ status: 204 });
			}
			if (rest === '/runs') return route.fulfill({ json: { runs: id === state.task.id ? state.runs : [] } });
			const logs = rest.match(/^\/runs\/([^/]+)\/logs$/);
			if (logs) {
				if (state.missing.includes(logs[1]))
					return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
				const all = (state.logs[logs[1]] ?? []).filter((log) => log.id > Number(url.searchParams.get('after') ?? 0));
				return route.fulfill({ json: { logs: all } });
			}
			if (rest.endsWith('/validation')) return route.fulfill({ json: null });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/history') return route.fulfill({ json: { transitions: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/publication') return route.fulfill({ json: { step: null } });
			if (rest === '/signals') return route.fulfill({ json: [] });
			return route.fulfill({ json: task });
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
	return { state, emit };
}

function workflowFixture(state: Awaited<ReturnType<typeof fixture>>['state']) {
	state.task = baseTask({ workflow_version_id: 'version-1', workflow_node: 'implement', workflow_status: 'running' });
	state.workflow = workflow();
	state.tasks['review-1'] = baseTask({
		id: 'review-1',
		kind: 'review',
		review_of_task_id: 'task-1',
		provider: 'codex',
		model: 'gpt-5',
		prompt: 'Review the pull request',
		state: 'completed',
		reason: 'Review finished',
		generation: 1,
		run_id: 'run-review'
	});
	state.logs = {
		'run-plan': lines('run-plan', 1, [
			text('Reading the list first.'),
			bash('p1', 'rg -n list src', 'Find the list'),
			result('p1', 'src/List.svelte'),
			text('Add a **search** box, then:\n\n1. the endpoint\n2. the tests')
		]),
		'run-impl-1': lines('run-impl-1', 5, [
			text('Starting with the endpoint.'),
			bash('i1', 'npm test', 'Run the unit tests'),
			result('i1', '12 passing'),
			text('Search is **added** and covered.')
		]),
		'run-review': lines('run-review', 14, [text('Please rename `q` to `query`.')]),
		'run-impl-2': lines('run-impl-2', 19, [text('Renaming the parameter.')])
	};
}

const stepButton = (page: Page, sequence: number) => page.locator(`#step-${sequence}`).getByRole('button').first();

test('a workflow task reads as a timeline: one labelled section per step, the running one open', async ({
	page
}, testInfo) => {
	const { state, emit } = await fixture(page);
	workflowFixture(state);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const timeline = thread.getByRole('list', { name: 'Path taken' });
	await expect(timeline.getByRole('listitem')).toHaveCount(6);
	// No dropdown of runs: each agent is a section of its own, named by its
	// step and the profile that ran it.
	await expect(thread.getByRole('combobox')).toHaveCount(0);
	await expect(thread.getByRole('region', { name: 'Plan · Orchestrator' })).toHaveCount(1);
	await expect(thread.getByRole('region', { name: 'Implement · Cheap Implementer' })).toHaveCount(2);
	await expect(thread.getByRole('region', { name: 'Review · Reviewer' })).toHaveCount(1);

	// A step is one line: what, who, model, how long and how it went.
	const plan = stepButton(page, 2);
	await expect(plan).toContainText('Plan · Orchestrator');
	await expect(plan).toContainText('claude-opus-5-5');
	await expect(plan).toContainText('4m 0s');
	await expect(plan.getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(plan).toHaveAttribute('aria-expanded', 'false');
	// Which harness ran and when it started are not on the line: a screen
	// reader hears them, and focus or hover shows them.
	await expect(plan).toHaveAccessibleName(/Claude Code/);
	await expect(plan).toHaveAccessibleName(/2026/);
	const tooltip = page.locator('[data-slot="tooltip-content"]').filter({ hasText: 'Claude Code' });
	await expect(tooltip).toHaveCount(0);
	await stepButton(page, 3).focus();
	await page.keyboard.press('Shift+Tab');
	await expect(plan).toBeFocused();
	await expect(tooltip).toContainText('2026');
	await expect(plan).toHaveAttribute('aria-describedby', /.+/);
	await plan.blur();
	const review = stepButton(page, 5);
	await expect(review.getByRole('img', { name: 'Changes requested' })).toBeVisible();
	await expect(review).toHaveAccessibleName(/Codex/);

	// The running step is open, shows its visit and streams.
	const running = page.locator('#step-6');
	await expect(stepButton(page, 6)).toHaveAttribute('aria-expanded', 'true');
	await expect(stepButton(page, 6)).toContainText('×2');
	await expect(stepButton(page, 6).getByRole('img', { name: 'Running' })).toBeVisible();
	await expect(stepButton(page, 6)).toHaveAccessibleName(/OpenCode/);
	await expect(running.getByText('Renaming the parameter.')).toBeVisible();
	await expect(thread.getByText('Search is added and covered.')).toHaveCount(0);
	state.logs['run-impl-2'] = [
		...state.logs['run-impl-2'],
		...lines('run-impl-2', 20, [text('placeholder'), text('Tests pass now.')]).slice(1)
	];
	emit('task.activity', {
		task_id: 'task-1',
		run_id: 'run-impl-2',
		log_id: 2,
		stream: 'stdout',
		truncated: false,
		events: []
	});
	await expect(running.getByText('Tests pass now.')).toBeVisible();

	// A step that is not an agent's is a compact row with nothing to open.
	const publish = page.locator('#step-4');
	await expect(publish).toHaveAttribute('data-step', 'row');
	await expect(publish).toContainText('Publish');
	await expect(publish.getByRole('button')).toHaveCount(0);
	const [rowHeight, stepHeight] = await Promise.all([
		publish.evaluate((element) => element.getBoundingClientRect().height),
		page.locator('#step-3').evaluate((element) => element.getBoundingClientRect().height)
	]);
	expect(rowHeight).toBeLessThan(stepHeight);

	// A finished step opens with the keyboard: its result first, as Markdown,
	// the trace that led to it below.
	const implement = stepButton(page, 3);
	await implement.focus();
	await page.keyboard.press('Enter');
	await expect(implement).toHaveAttribute('aria-expanded', 'true');
	const first = page.locator('#step-3');
	const messages = first.getByRole('article', { name: 'Agent' });
	await expect(messages).toHaveCount(2);
	await expect(messages.first().locator('strong')).toHaveText('added');
	await expect(messages.last()).toContainText('Starting with the endpoint.');
	const chip = first.getByRole('button', { name: /Run the unit tests/ });
	await chip.click();
	await expect(first.getByText('12 passing')).toBeVisible();
	// It closes again; the running step stayed open.
	await implement.focus();
	await page.keyboard.press('Space');
	await expect(implement).toHaveAttribute('aria-expanded', 'false');
	await expect(stepButton(page, 6)).toHaveAttribute('aria-expanded', 'true');

	// A planning step shows its plan, once.
	await plan.click();
	const planned = page.locator('#step-2').getByRole('article', { name: 'Plan' });
	await expect(planned.locator('strong')).toHaveText('search');
	await expect(planned.getByRole('listitem')).toHaveText(['the endpoint', 'the tests']);
	await expect(page.locator('#step-2').getByRole('article', { name: 'Agent' })).toHaveCount(1);
	await expect(page.locator('#step-2').getByRole('button', { name: /Find the list/ })).toBeVisible();

	// The whole request is one click away.
	const request = thread.getByText('LAST-LINE-OF-THE-REQUEST');
	const cut = () => request.evaluate((element) => element.scrollHeight > element.clientHeight + 1);
	expect(await cut()).toBe(true);
	const expand = thread.getByRole('button', { name: 'Expand' });
	await expect(expand).toHaveAttribute('aria-expanded', 'false');
	await expand.click();
	await expect(thread.getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true');
	expect(await cut()).toBe(false);
	await page.screenshot({ path: testInfo.outputPath('timeline-desktop.png'), fullPage: true });

	// The review a step started is read in the step and opens from it.
	await review.click();
	await expect(page.locator('#step-5').getByText('Please rename')).toBeVisible();
	const link = page.locator('#step-5').getByRole('link', { name: 'Review by Reviewer' });
	await expect(link).toHaveAttribute('href', /agent=review-1/);
	await link.click();
	await expect(page).toHaveURL(/agent=review-1/);
	await expect(thread.getByText('Review the pull request')).toBeVisible();
	await expect(thread.getByRole('region', { name: 'Codex' }).getByText('Please rename')).toBeVisible();
});

test('the step the task moves to opens and the one it left closes', async ({ page }) => {
	const { state, emit } = await fixture(page);
	workflowFixture(state);
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1#step-3');
	// A link to a step opens that step beside the running one.
	await expect(stepButton(page, 3)).toHaveAttribute('aria-expanded', 'true');
	await expect(stepButton(page, 6)).toHaveAttribute('aria-expanded', 'true');
	await expect(page.locator('#step-3').getByText('Search is added and covered.')).toBeVisible();
	// The reader opens another step and scrolls back up to read it.
	await stepButton(page, 2).click();
	await expect(page.locator('#step-2').getByRole('article', { name: 'Plan' })).toBeVisible();
	const viewport = page.getByLabel('Messages', { exact: true });
	const scrollTop = () => viewport.evaluate((element) => element.scrollTop);
	// (A wheel, as a person scrolls: the view follows new output only until then.
	// The view first has to have followed down, or there is nothing to scroll up.)
	await expect.poll(scrollTop).toBeGreaterThan(0);
	await viewport.hover();
	await expect(async () => {
		const top = await scrollTop();
		await page.mouse.wheel(0, -200);
		expect(await scrollTop()).toBeLessThan(top);
	}).toPass();
	const before = await scrollTop();

	// The implementer finishes and the work is published again.
	const view = workflow();
	view.steps[5] = { ...view.steps[5], outcome: 'agent_finished', finished_at: at(25) };
	view.steps.push(step(7, 'publish', 'publish', 25));
	view.runs[3] = { ...view.runs[3], status: 'succeeded' };
	view.node = 'publish';
	state.workflow = view;
	state.task = { ...state.task, state: 'publishing', workflow_node: 'publish', updated_at: at(25) };
	emit('task.state_changed', state.task);
	await expect(page.locator('#step-7')).toContainText('Publish');
	await expect(page.locator('#step-7').getByRole('img', { name: 'Running' })).toBeVisible();
	await expect(stepButton(page, 6).getByRole('img', { name: 'Done' })).toBeVisible();
	// With no agent running, the last one that worked stays in view, and what
	// the reader opened and where they were reading is left alone.
	await expect(stepButton(page, 6)).toHaveAttribute('aria-expanded', 'true');
	await expect(stepButton(page, 2)).toHaveAttribute('aria-expanded', 'true');
	await expect(stepButton(page, 3)).toHaveAttribute('aria-expanded', 'true');
	expect(Math.abs((await scrollTop()) - before)).toBeLessThanOrEqual(2);
	await expect(page.locator('#step-6').getByRole('article', { name: 'Agent' })).toContainText('Renaming the parameter.');
});

// A running task can be stopped from where it is watched: one click, one
// confirmation. It reads as cancelling until the task reports cancelled.
test('a running task is stopped from the agent view in two clicks', async ({ page }) => {
	const { state, emit } = await fixture(page);
	workflowFixture(state);
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const stop = thread.getByRole('button', { name: 'Stop', exact: true });
	await expect(stop).toBeVisible();

	// Backing out of the confirmation stops nothing.
	await stop.click();
	const dialog = page.getByRole('alertdialog', { name: 'Stop ENG-1?' });
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(dialog).toBeHidden();
	expect(state.cancels).toBe(0);

	await stop.click();
	await dialog.getByRole('button', { name: 'Stop', exact: true }).click();
	await expect(dialog).toBeHidden();
	await expect.poll(() => state.cancels).toBe(1);
	await expect(thread.getByRole('status').filter({ hasText: 'Cancelling' })).toBeVisible();
	await expect(thread.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);

	// A cleanup that did not finish can be asked for again from here.
	await thread.getByRole('button', { name: 'Retry cancellation' }).click();
	await expect.poll(() => state.cancels).toBe(2);

	state.task = { ...state.task, state: 'cancelled', reason: 'Runtime cancellation confirmed' };
	emit('task.state_changed', state.task);
	await expect(thread.getByRole('status').filter({ hasText: 'Cancelled' })).toBeVisible();
	await expect(thread.getByRole('button', { name: 'Retry cancellation' })).toHaveCount(0);
	await expect(thread.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
});

test('a finished task and another person\'s task have no Stop', async ({ page }) => {
	const { state } = await fixture(page);
	workflowFixture(state);
	state.task = { ...state.task, state: 'completed', reason: 'Pull request #1 was merged on GitHub' };
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(thread.getByRole('list', { name: 'Path taken' })).toBeVisible();
	await expect(thread.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);

	// Someone else's running task: only its creator may stop it.
	state.task = { ...state.task, state: 'running', creator_id: 'user-2', reason: 'Native coding harness started' };
	await page.reload();
	await expect(page.getByRole('region', { name: 'Agent thread of ENG-1' }).getByRole('list', { name: 'Path taken' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
});

test('a stop that is refused says so and leaves the task running', async ({ page }) => {
	const { state } = await fixture(page);
	workflowFixture(state);
	state.cancelRefused = true;
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await thread.getByRole('button', { name: 'Stop', exact: true }).click();
	await page.getByRole('alertdialog', { name: 'Stop ENG-1?' }).getByRole('button', { name: 'Stop', exact: true }).click();
	await expect(thread.getByRole('alert')).toContainText('This request conflicts with the current task');
	await expect(thread.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
});

test('a task without a workflow is one step per attempt', async ({ page }) => {
	const { state } = await fixture(page);
	state.task = baseTask({ state: 'awaiting_review', reason: 'Ready for review', provider: 'codex', model: 'gpt-5', run_id: 'run-2', generation: 2 });
	state.runs = [
		{ id: 'run-2', provider_id: 'codex', model: 'gpt-5', status: 'succeeded', created_at: at(9), started_at: at(10), completed_at: at(13) },
		{ id: 'run-1', provider_id: 'claude-code', model: '', status: 'failed', created_at: at(1), started_at: at(2), completed_at: at(5) }
	];
	state.logs = {
		'run-1': lines('run-1', 2, [text('The build is broken.')]),
		'run-2': lines('run-2', 10, [bash('c1', 'npm test', 'Run the unit tests'), result('c1', 'ok'), text('Fixed the **build**.')])
	};
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(page.getByRole('navigation', { name: 'Location' })).toContainText('ENG-1 · Iteration 2');
	const steps = thread.getByRole('list', { name: 'Path taken' }).getByRole('listitem');
	await expect(steps).toHaveCount(2);
	expect(state.workflowRequests).toBe(0);
	// Oldest first, named by the harness that ran; the last one is open.
	const [older, latest] = [steps.nth(0), steps.nth(1)];
	await expect(older.getByRole('region', { name: 'Claude Code' })).toBeVisible();
	await expect(older.getByRole('button').first()).toHaveAttribute('aria-expanded', 'false');
	await expect(older.getByRole('img', { name: 'Failed' })).toBeVisible();
	await expect(older.getByRole('button').first()).toContainText('3m 0s');
	await expect(latest.getByRole('region', { name: 'Codex' })).toBeVisible();
	await expect(latest.getByRole('button').first()).toHaveAttribute('aria-expanded', 'true');
	await expect(latest.getByRole('button').first()).toContainText('gpt-5');
	await expect(latest.getByRole('article', { name: 'Agent' }).locator('strong')).toHaveText('build');
	await expect(thread.getByText('The build is broken.')).toHaveCount(0);
	await older.getByRole('button').first().click();
	await expect(older.getByText('The build is broken.')).toBeVisible();
});

test('the timeline fits a phone', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	workflowFixture(state);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(thread.getByRole('list', { name: 'Path taken' }).getByRole('listitem')).toHaveCount(6);
	await expect(page.locator('#step-6').getByText('Renaming the parameter.')).toBeVisible();
	// The model leaves the line and shows inside the open step instead.
	await expect(stepButton(page, 6).getByText('kimi-k2')).toBeHidden();
	await expect(page.locator('#step-6').getByText('kimi-k2').last()).toBeVisible();
	await stepButton(page, 3).click();
	await expect(page.locator('#step-3').getByRole('article', { name: 'Agent' }).first()).toContainText('Search is added');
	for (const sequence of [2, 3, 5, 6]) {
		const box = await stepButton(page, sequence).boundingBox();
		expect(box!.x).toBeGreaterThanOrEqual(0);
		expect(box!.x + box!.width).toBeLessThanOrEqual(390);
		// The name stays readable beside the status and the time.
		await expect(stepButton(page, sequence).locator('.font-medium')).toBeVisible();
	}
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
		await page.screenshot({ path: testInfo.outputPath(`timeline-390-${dark ? 'dark' : 'light'}.png`), fullPage: true });
	}
});

const count = (requests: string[], pattern: RegExp) => requests.filter((request) => pattern.test(request)).length;

test('only the running step refetches while the agent streams', async ({ page }) => {
	const { state, emit } = await fixture(page);
	workflowFixture(state);
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	await stepButton(page, 2).click();
	await stepButton(page, 3).click();
	await expect(page.locator('#step-3').getByText('Search is added and covered.')).toBeVisible();
	await expect(page.locator('#step-2').getByText('Reading the list first.')).toBeVisible();
	await expect(page.locator('#step-6').getByText('Renaming the parameter.')).toBeVisible();
	// A finished run was read once: its output, its check and its manual checks.
	const finished = /^task-1\/runs\/(run-plan|run-impl-1)\//;
	const running = /^task-1\/runs\/run-impl-2\/logs/;
	expect(count(state.requests, finished)).toBe(6);
	const streamed = count(state.requests, running);
	for (const [id, value] of [
		[2, 'Still renaming.'],
		[3, 'Tests pass now.']
	] as const) {
		state.logs['run-impl-2'] = [
			...state.logs['run-impl-2'],
			{ ...lines('run-impl-2', 20, [text(value)])[0], id, sequence: id }
		];
		emit('task.activity', { task_id: 'task-1', run_id: 'run-impl-2', log_id: id, stream: 'stdout', truncated: false, events: [] });
		await expect(page.locator('#step-6').getByText(value)).toBeVisible();
	}
	expect(count(state.requests, running)).toBeGreaterThan(streamed);
	expect(count(state.requests, finished)).toBe(6);
});

test('a waiting task is not polled like a running one, and known summaries are not fetched again', async ({ page }) => {
	const { state, emit } = await fixture(page);
	workflowFixture(state);
	// The work is published and waits for a person's approval.
	const view = workflow();
	view.graph.nodes.push(node('approve', 'approval', 'Approve'));
	view.steps[5] = { ...view.steps[5], outcome: 'agent_finished', finished_at: at(25) };
	view.steps.push(step(7, 'approve', 'approval', 25));
	view.runs = view.runs.map((item, index) => ({
		...item,
		status: 'succeeded',
		completed_at: at(4 + index * 7),
		...(item.id === 'run-impl-2' ? { summary: 'Renamed, **as asked**.' } : {})
	}));
	view.node = 'approve';
	view.status = 'waiting';
	state.workflow = view;
	state.logs['run-impl-2'] = [];
	state.task = { ...state.task, state: 'awaiting_review', workflow_node: 'approve', workflow_status: 'waiting' };
	await page.clock.install();
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	// With no output recorded, the run's own summary is what the step shows.
	const last = page.locator('#step-6');
	await expect(last.getByRole('article', { name: 'Agent' }).locator('strong')).toHaveText('as asked');
	await expect(page.locator('#step-7').getByRole('img', { name: 'Waiting' })).toBeVisible();
	// The timeline's reloads are the requests that say which summaries it has
	// (the side panel reads the same route for its graph).
	const reloads = () => count(state.requests, /^task-1\/workflow\?summaries_since=/);
	// A minute of waiting asks nothing: the task's events say when it moves.
	await page.clock.fastForward(60_000);
	expect(reloads()).toBe(0);
	// The approval arrives as an event; the reload does not ask for the
	// summaries the view already has, and keeps showing them.
	state.workflow = {
		...view,
		status: 'done',
		node: 'approve',
		steps: view.steps.map((item) => (item.sequence === 7 ? { ...item, outcome: 'approval_approved', finished_at: at(40) } : item))
	};
	emit('task.workflow_updated', { task_id: 'task-1', sequence: 7, node: 'approve', status: 'done' });
	await expect(page.locator('#step-7').getByRole('img', { name: 'Waiting' })).toHaveCount(0);
	expect(reloads()).toBe(1);
	expect(state.requests).toContain(`task-1/workflow?summaries_since=${encodeURIComponent(at(25))}`);
	await expect(last.getByRole('article', { name: 'Agent' }).locator('strong')).toHaveText('as asked');
	// A settled workflow is not polled at all.
	await page.clock.fastForward(600_000);
	expect(reloads()).toBe(1);
});

test('a running workflow is still polled', async ({ page }) => {
	const { state } = await fixture(page);
	workflowFixture(state);
	await page.clock.install();
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	await expect(page.locator('#step-6').getByText('Renaming the parameter.')).toBeVisible();
	const reloads = () => count(state.requests, /^task-1\/workflow\?summaries_since=/);
	const loaded = reloads();
	await page.clock.fastForward(16_000);
	await expect.poll(reloads).toBeGreaterThan(loaded);
});

test('a planning step that was resumed keeps what its earlier run said', async ({ page }) => {
	const { state } = await fixture(page);
	workflowFixture(state);
	state.workflow.runs = [
		run('run-plan-0', 2, 'claude-code', 'claude-opus-5-5', 'failed'),
		...state.workflow.runs
	];
	state.logs['run-plan-0'] = lines('run-plan-0', 0, [
		bash('q1', 'ls', 'List the files'),
		result('q1', 'src'),
		text('The usage limit stopped me **here**.')
	]);
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	await stepButton(page, 2).click();
	const planning = page.locator('#step-2');
	// The plan is shown once, above the runs; only the run that wrote it
	// leaves its final message out of its trace.
	await expect(planning.getByRole('article', { name: 'Plan' })).toHaveCount(1);
	const messages = planning.getByRole('article', { name: 'Agent' });
	await expect(messages).toHaveCount(2);
	await expect(messages.first().locator('strong')).toHaveText('here');
	await expect(messages.last()).toContainText('Reading the list first.');
});

test('a run that does not exist yet is waited for, not reported as missing', async ({ page }) => {
	const { state, emit } = await fixture(page);
	// Blocked right after a new attempt was set, before its run was recorded.
	state.task = baseTask({ state: 'blocked', reason: 'Waiting for a machine', provider: 'codex', model: 'gpt-5', run_id: 'run-9' });
	state.runs = [
		{ id: 'run-1', provider_id: 'codex', model: 'gpt-5', status: 'failed', created_at: at(1), started_at: at(2), completed_at: at(5) }
	];
	state.missing = ['run-9'];
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const steps = thread.getByRole('list', { name: 'Path taken' }).getByRole('listitem');
	await expect(steps).toHaveCount(2);
	await expect(steps.nth(1).getByRole('img', { name: 'Waiting' })).toBeVisible();
	await expect.poll(() => count(state.requests, /^task-1\/runs\/run-9\/logs/)).toBeGreaterThan(0);
	await expect(steps.nth(1).getByText('The agent has not written anything yet.')).toBeVisible();
	await expect(thread.getByRole('alert')).toHaveCount(0);
	// Records that are gone for a task that ended are still said to be gone.
	state.task = { ...state.task, state: 'failed', reason: 'Execution failed', updated_at: at(30) };
	emit('task.state_changed', state.task);
	await expect(thread.getByRole('alert')).toHaveText('This iteration’s records are no longer available.');
});
