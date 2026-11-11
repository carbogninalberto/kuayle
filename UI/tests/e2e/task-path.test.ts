import { copyFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type Page, type TestInfo, type WebSocketRoute } from '@playwright/test';

// The sidebar's task card shows the path the task took (KUA-133): compact by
// default, one control for the whole path, each step linking to its section
// in the agent view. Messages and reviews are in the agent view only.
test.use({ timezoneId: 'UTC', locale: 'en-US' });

const at = (minute: number, second = 0) =>
	`2026-10-26T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`;
const time = at(0);

const baseTask = (over: any = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	prompt: 'Add search to the issue list',
	provider: 'opencode',
	model: 'kimi-k2',
	thinking: '',
	state: 'awaiting_review',
	reason: 'Workflow · Review → CI: approved; waiting for CI on the pull request',
	generation: 3,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl-2',
	created_at: time,
	updated_at: at(29),
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
let column = 0;
const node = (id: string, type: string, label: string, over: any = {}) => ({
	id,
	type,
	label,
	position: { x: type === 'start' ? (column = 0) : (column += 300), y: 0 },
	...over
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

// A task like KUA-122: an orchestrator plans, a cheap implementer codes, the
// work is published and reviewed, the review sends it back once, and it now
// waits for CI. It was paused and resumed during the second implementation
// and moved to a newer version of its workflow while waiting.
const workflow = () => ({
	workflow_id: 'workflow-1',
	name: 'Plan and review',
	version: 3,
	builtin: false,
	graph: {
		nodes: [
			node('start', 'start', 'Start'),
			node('plan', 'agent', 'Plan', { profile_id: 'p-orch' }),
			node('implement', 'agent', 'Implement', { profile_id: 'p-impl' }),
			node('publish', 'publish', 'Publish'),
			node('review', 'agent', 'Review', { profile_id: 'p-rev' }),
			node('ci', 'check', 'CI', { check: 'ci' }),
			node('end', 'end', 'Done', { outcome: 'done' })
		],
		edges: [],
		budget: {}
	},
	node: 'ci',
	status: 'waiting',
	visits: { start: 1, plan: 1, implement: 2, publish: 2, review: 2, ci: 1 },
	iterations: 3,
	sequence: 9,
	steps: [
		step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
		step(2, 'plan', 'agent', 0, { from_node: 'start', condition: 'else', generation: 1, outcome: 'agent_finished', finished_at: at(4) }),
		step(3, 'implement', 'agent', 4, {
			from_node: 'plan',
			condition: 'agent_finished',
			generation: 2,
			outcome: 'agent_finished',
			finished_at: at(12, 30)
		}),
		step(4, 'publish', 'publish', 12, { from_node: 'implement', condition: 'agent_finished', outcome: 'published', finished_at: at(13) }),
		step(5, 'review', 'agent', 13, {
			from_node: 'publish',
			condition: 'published',
			review_task_id: 'review-1',
			outcome: 'review_changes_requested',
			detail: 'Rename q to query',
			finished_at: at(18)
		}),
		step(6, 'implement', 'agent', 18, {
			from_node: 'review',
			condition: 'review_changes_requested',
			generation: 3,
			visit: 2,
			outcome: 'agent_finished',
			finished_at: at(24)
		}),
		step(7, 'publish', 'publish', 24, {
			from_node: 'implement',
			condition: 'agent_finished',
			visit: 2,
			outcome: 'published',
			finished_at: at(25)
		}),
		step(8, 'review', 'agent', 25, {
			from_node: 'publish',
			condition: 'published',
			visit: 2,
			review_task_id: 'review-2',
			outcome: 'review_approved',
			finished_at: at(29)
		}),
		step(9, 'ci', 'check', 29, { from_node: 'review', condition: 'review_approved' })
	] as any[],
	runs: [
		run('run-plan', 2, 'claude-code', 'claude-opus-5-5', 'succeeded', { completed_at: at(4) }),
		run('run-impl-1', 3, 'opencode', 'kimi-k2', 'succeeded', { completed_at: at(12) }),
		run('run-impl-2', 6, 'opencode', 'kimi-k2', 'succeeded', { completed_at: at(24) })
	],
	pauses: [
		{ action: 'pause_requested', sequence: 6, mode: 'now', actor_id: 'user-1', at: at(19, 50) },
		{ action: 'paused', sequence: 6, mode: 'now', actor_id: 'user-1', at: at(20) },
		{ action: 'resumed', sequence: 6, actor_id: 'user-1', at: at(22) },
		{ action: 'version_switched', sequence: 9, from_version: 2, to_version: 3, actor_id: 'user-1', at: at(30) }
	] as any[],
	profiles: [
		profile('p-orch', 'Orchestrator', 'claude-code', 'claude-opus-5-5', 'plans'),
		profile('p-impl', 'Cheap Implementer', 'opencode', 'kimi-k2', 'changes_code'),
		profile('p-rev', 'Reviewer', 'codex', 'gpt-5', 'reviews')
	],
	can_decide: false,
	task_state: 'awaiting_review',
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
		task: baseTask({ workflow_version_id: 'version-3', workflow_node: 'ci', workflow_status: 'waiting' }) as any,
		workflow: workflow() as any,
		// A task without a workflow lists its attempts and its state changes.
		runs: [] as any[],
		transitions: [] as any[],
		providers: [] as any[],
		publication: { step: null } as any,
		sessions: [] as any[],
		// Every agent-task request, as "<method> <task>/<rest>".
		requests: [] as string[],
		sockets: [] as WebSocketRoute[],
		events: [] as any[]
	};
	// The task's times are today's, so the path shows hours, not dates.
	await page.clock.setFixedTime(new Date(at(40)));
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
		if (path.endsWith('/task-options'))
			return route.fulfill({ json: { providers: state.providers, model_servers: [] } });
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
			if (id !== state.task.id) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			state.requests.push(`${route.request().method()} ${id}${rest}`);
			if (rest === '/workflow') return route.fulfill({ json: state.workflow });
			if (rest === '/runs') return route.fulfill({ json: { runs: state.runs } });
			if (rest === '/history') {
				const after = Number(url.searchParams.get('after') ?? 0);
				return route.fulfill({ json: { transitions: state.transitions.filter((t) => t.sequence > after) } });
			}
			if (/^\/runs\/[^/]+\/logs$/.test(rest)) return route.fulfill({ json: { logs: [] } });
			if (rest.endsWith('/validation')) return route.fulfill({ json: null });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: state.sessions } });
			if (rest.endsWith('/publication')) return route.fulfill({ json: state.publication });
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
	return { state, emit };
}

const tasksRegion = (page: Page) => page.getByRole('region', { name: 'Coding tasks' });
const pathOf = (page: Page) => tasksRegion(page).getByRole('list', { name: 'Path taken' });
const count = (requests: string[], wanted: string) => requests.filter((request) => request === wanted).length;

async function showAgents(page: Page) {
	const tasks = tasksRegion(page);
	await expect(page.getByRole('button', { name: 'Agents', exact: true }).first()).toBeVisible();
	if (!(await tasks.isVisible())) await page.getByRole('button', { name: 'Agents', exact: true }).first().click();
	await expect(tasks).toBeVisible();
}

// The screenshots asked for with the change are also kept outside the test
// output when KUA133_SHOTS names a directory.
async function shot(page: Page, testInfo: TestInfo, name: string) {
	const file = testInfo.outputPath(name);
	await page.screenshot({ path: file });
	const keep = process.env.KUA133_SHOTS;
	if (keep) {
		mkdirSync(keep, { recursive: true });
		copyFileSync(file, join(keep, name));
	}
}

test('the sidebar shows the path a workflow task took, compact, and opens to its loops', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const path = pathOf(page);
	const rows = path.getByRole('listitem');

	// Compact: each step once, as it last went, the one the task is on last.
	await expect(rows).toHaveCount(5);
	await expect(rows.getByRole('link')).toHaveText([
		/^Plan · Orchestrator/,
		/^Implement · Cheap Implementer/,
		/^Publish/,
		/^Review · Reviewer/,
		/^CI/
	]);
	// A step is its name, its agent, its visit, when it started and how long
	// it took; how it went is an icon.
	const implement = rows.nth(1);
	await expect(implement.getByRole('link')).toContainText('×2');
	await expect(implement.getByRole('link')).toContainText('10:18 AM · 6m');
	await expect(implement.getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(rows.nth(0).getByRole('link')).toContainText('10:00 AM · 4m');
	await expect(rows.nth(3).getByRole('img', { name: 'Done' })).toBeVisible();
	// The step the task is on says what it waits for; nothing else explains itself.
	const current = rows.nth(4);
	await expect(current).toHaveAttribute('aria-current', 'step');
	await expect(current.getByRole('img', { name: 'Waiting' })).toBeVisible();
	await expect(current.getByRole('link')).toContainText('10:29 AM');
	await expect(path.getByTestId('task-path-note')).toHaveText(['Waiting for CI on the pull request']);
	await expect(path.getByTestId('task-path-detail')).toHaveCount(0);
	// The card does not say it a second time in the runner's words.
	await expect(tasksRegion(page).getByText('Workflow · Review → CI', { exact: false })).toHaveCount(0);
	// The chart stays above the tasks; the old verbose cards are gone.
	await expect(page.getByTestId('task-workflow')).toContainText('Plan and review · version 3');
	await expect(page.getByText('Step 2: Plan')).toHaveCount(0);
	await expect(page.getByText('Outcome:', { exact: false })).toHaveCount(0);

	// The time is absolute for a screen reader, and on hover or keyboard focus.
	const link = implement.getByRole('link');
	await expect(link).toHaveAccessibleName(/Oct 26, 2026/);
	// It is a plain link: nothing of a button about it.
	await expect(link).not.toHaveAttribute('type', /.*/);
	const tooltip = page
		.locator('[data-slot="tooltip-content"]:not([data-state="closed"])')
		.filter({ hasText: 'Oct 26, 2026' });
	await expect(tooltip).toHaveCount(0);
	await implement.getByRole('button', { name: 'Details of Implement' }).focus();
	await page.keyboard.press('Shift+Tab');
	await expect(link).toBeFocused();
	await expect(tooltip).toContainText('Oct 26, 2026, 10:18 AM');
	await link.blur();
	await expect(tooltip).toHaveCount(0);
	await expect(async () => {
		await page.mouse.move(0, 0);
		await rows.nth(0).getByRole('link').hover();
		await expect(tooltip).toContainText('Oct 26, 2026, 10:00 AM', { timeout: 1500 });
	}).toPass();
	await page.mouse.move(0, 0);
	await expect(tooltip).toHaveCount(0);
	await shot(page, testInfo, 'path-compact-desktop.png');

	// One control opens the whole path: the loop, the pause and the version move.
	const whole = tasksRegion(page).getByRole('button', { name: 'Full path' });
	await expect(whole).toHaveAttribute('aria-expanded', 'false');
	await whole.focus();
	await page.keyboard.press('Enter');
	await expect(whole).toHaveAttribute('aria-expanded', 'true');
	await expect(rows).toHaveCount(12);
	await expect(rows).toHaveText([
		/^\s*Start/,
		/^\s*Plan · Orchestrator/,
		/^\s*Implement · Cheap Implementer/,
		/^\s*Publish/,
		/^\s*Review · Reviewer/,
		/^\s*Implement · Cheap Implementer/,
		/^\s*Paused/,
		/^\s*Resumed/,
		/^\s*Publish/,
		/^\s*Review · Reviewer/,
		/^\s*CI/,
		/^\s*Moved to workflow version 3/
	]);
	await expect(rows.nth(4).getByRole('img', { name: 'Changes requested' })).toBeVisible();
	await expect(rows.nth(6)).toContainText('10:20 AM');
	await expect(rows.nth(7)).toContainText('10:22 AM');
	// A pause's time is reachable by keyboard too.
	await rows.nth(8).getByRole('link').focus();
	await page.keyboard.press('Shift+Tab');
	await expect(rows.nth(7).getByRole('button')).toBeFocused();
	await expect(tooltip).toContainText('Oct 26, 2026, 10:22 AM');
	await expect(rows.nth(7).getByRole('button')).toHaveAccessibleName(/Oct 26, 2026, 10:22 AM/);
	await rows.nth(7).getByRole('button').blur();

	// Where it went next, and why, shows when a step is opened.
	const review = rows.nth(4);
	const details = review.getByRole('button', { name: 'Details of Review' });
	await expect(details).toHaveAttribute('aria-expanded', 'false');
	await details.click();
	await expect(details).toHaveAttribute('aria-expanded', 'true');
	await expect(review.getByTestId('task-path-detail')).toContainText('Rename q to query');
	await expect(review.getByTestId('task-path-detail')).toContainText('Implement · Changes requested');
	// The step the task is on has nothing more to say.
	await expect(rows.nth(10).getByRole('button')).toHaveCount(0);
	await page.mouse.move(0, 0);
	await expect(page.locator('[data-slot="tooltip-content"]')).toHaveCount(0);
	// (The disclosure's short turn is over before the picture is taken.)
	await page.waitForTimeout(300);
	await shot(page, testInfo, 'path-expanded-desktop.png');
	for (const dark of [false, true]) {
		await page.evaluate((d) => {
			document.documentElement.classList.toggle('dark', d);
			document.documentElement.setAttribute('data-theme', d ? 'dark' : 'light');
		}, dark);
		await tasksRegion(page).screenshot({ path: testInfo.outputPath(`path-expanded-${dark ? 'dark' : 'light'}.png`) });
	}
	await whole.click();
	await expect(rows).toHaveCount(5);
});

test('a step links to its section in the agent view, and both read one load of the workflow', async ({ page }) => {
	const { state, emit } = await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const rows = pathOf(page).getByRole('listitem');
	await expect(rows).toHaveCount(5);
	// The chart and the card's path share the task's one load.
	expect(count(state.requests, 'GET task-1/workflow')).toBe(1);

	const link = rows.nth(1).getByRole('link');
	await expect(link).toHaveAttribute('href', /agent=task-1#step-6$/);
	await link.click();
	await expect(page).toHaveURL(/agent=task-1#step-6$/);
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(thread.getByRole('list', { name: 'Path taken' }).getByRole('listitem')).toHaveCount(9);
	const opened = (sequence: number) => page.locator(`#step-${sequence}`).getByRole('button').first();
	await expect(opened(6)).toHaveAttribute('aria-expanded', 'true');
	await expect(opened(3)).toHaveAttribute('aria-expanded', 'false');
	// The agent view opened on the load the sidebar already had.
	expect(count(state.requests, 'GET task-1/workflow')).toBe(1);

	// With the agent view open, another step of the path opens there too.
	await tasksRegion(page).getByRole('button', { name: 'Full path' }).click();
	await rows.nth(2).getByRole('link').click();
	await expect(page).toHaveURL(/agent=task-1#step-3$/);
	await expect(opened(3)).toHaveAttribute('aria-expanded', 'true');

	// One event reloads once for the timeline, the chart and the path.
	state.workflow = {
		...state.workflow,
		node: 'end',
		status: 'done',
		sequence: 10,
		steps: [
			...state.workflow.steps.slice(0, 8),
			{ ...state.workflow.steps[8], outcome: 'checks_passed', finished_at: at(35) },
			step(10, 'end', 'end', 35, { from_node: 'ci', condition: 'checks_passed', outcome: 'done', finished_at: at(35) })
		]
	};
	state.task = { ...state.task, state: 'completed', reason: 'Workflow finished', workflow_node: 'end', workflow_status: 'done', updated_at: at(35) };
	emit('task.state_changed', state.task);
	await expect(page.locator('#step-10')).toContainText('Done');
	await expect(rows).toHaveCount(13);
	expect(count(state.requests, 'GET task-1/workflow')).toBe(2);
});

test('the path follows the task as it advances', async ({ page }) => {
	const { state, emit } = await fixture(page);
	// The second implementation is under way.
	const view = workflow();
	view.steps = view.steps.slice(0, 6).map((item) => (item.sequence === 6 ? { ...item, outcome: undefined, finished_at: undefined } : item));
	view.pauses = [];
	view.runs = [...view.runs.slice(0, 2), run('run-impl-2', 6, 'opencode', 'kimi-k2', 'running')];
	Object.assign(view, { node: 'implement', status: 'running', sequence: 6, task_state: 'running' });
	state.workflow = view;
	state.task = baseTask({
		workflow_version_id: 'version-3',
		workflow_node: 'implement',
		workflow_status: 'running',
		state: 'running',
		reason: 'Native coding harness started',
		updated_at: at(18)
	});
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const rows = pathOf(page).getByRole('listitem');
	// The last outcome, then the step it led to, running and counting.
	await expect(rows.getByRole('link')).toHaveText([/^Plan/, /^Publish/, /^Review/, /^Implement · Cheap Implementer/]);
	await expect(rows.nth(2).getByRole('img', { name: 'Changes requested' })).toBeVisible();
	await expect(rows.nth(3).getByRole('img', { name: 'Running' })).toBeVisible();
	await expect(rows.nth(3).getByRole('link')).toContainText('×2');
	await expect(rows.nth(3).getByRole('link')).toContainText('10:18 AM · 22m');
	await expect(pathOf(page).getByTestId('task-path-note')).toHaveCount(0);

	// The implementer finishes and the work is published again.
	state.workflow = {
		...view,
		node: 'publish',
		sequence: 7,
		steps: [
			...view.steps.slice(0, 5),
			{ ...view.steps[5], outcome: 'agent_finished', finished_at: at(24) },
			step(7, 'publish', 'publish', 24, { from_node: 'implement', condition: 'agent_finished', visit: 2 })
		]
	};
	state.task = { ...state.task, state: 'publishing', workflow_node: 'publish', updated_at: at(24) };
	emit('task.state_changed', state.task);
	await expect(rows.getByRole('link')).toHaveText([/^Plan/, /^Review/, /^Implement/, /^Publish/]);
	await expect(rows.nth(2).getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(rows.nth(2).getByRole('link')).toContainText('10:18 AM · 6m');
	await expect(rows.nth(3).getByRole('img', { name: 'Running' })).toBeVisible();
	await expect(rows.nth(3)).toHaveAttribute('aria-current', 'step');

	// A step entered without a change of the task arrives as a workflow event.
	state.workflow = {
		...state.workflow,
		node: 'ci',
		status: 'waiting',
		sequence: 8,
		steps: [
			...state.workflow.steps.slice(0, 6),
			{ ...state.workflow.steps[6], outcome: 'published', finished_at: at(25) },
			step(8, 'ci', 'check', 25, { from_node: 'publish', condition: 'published' })
		]
	};
	state.task = { ...state.task, state: 'awaiting_review', workflow_node: 'ci', workflow_status: 'waiting' };
	emit('task.state_changed', state.task);
	await expect(rows.last().getByRole('link')).toHaveText(/^CI/);
	await expect(pathOf(page).getByTestId('task-path-note')).toHaveText(['Waiting for CI on the pull request']);
});

test('a failed task shows the step and the reason it stopped at', async ({ page }) => {
	const { state } = await fixture(page);
	const view = workflow();
	view.steps = view.steps.slice(0, 3).map((item) => (item.sequence === 3 ? { ...item, outcome: 'agent_failed', finished_at: at(9) } : item));
	view.pauses = [];
	view.runs = [view.runs[0], run('run-impl-1', 3, 'opencode', 'kimi-k2', 'failed', { completed_at: at(9) })];
	Object.assign(view, { node: 'implement', status: 'failed', sequence: 3, task_state: 'failed' });
	state.workflow = view;
	state.task = baseTask({
		workflow_version_id: 'version-3',
		workflow_node: 'implement',
		workflow_status: 'failed',
		state: 'failed',
		reason: 'Workflow · Implement failed: the agent exited with status 1',
		generation: 2,
		run_id: 'run-impl-1',
		updated_at: at(9)
	});
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const rows = pathOf(page).getByRole('listitem');
	await expect(rows.getByRole('link')).toHaveText([/^Plan · Orchestrator/, /^Implement · Cheap Implementer/]);
	const stopped = rows.nth(1);
	await expect(stopped.getByRole('img', { name: 'Failed' })).toBeVisible();
	await expect(stopped.getByRole('link')).toContainText('10:04 AM · 5m');
	// Why it stopped is under the step, without opening the history, and once.
	await expect(stopped.getByTestId('task-path-note')).toHaveText('Workflow · Implement failed: the agent exited with status 1');
	await expect(tasksRegion(page).getByRole('button', { name: 'Execution details', exact: true })).toHaveAttribute(
		'aria-expanded',
		'false'
	);
	await expect(tasksRegion(page).getByText('the agent exited with status 1', { exact: false })).toHaveCount(1);
});

test('a task without a workflow shows its attempts and state changes the same way', async ({ page }) => {
	const { state } = await fixture(page);
	// The Standard flow, as the workflow panel draws it for such a task.
	state.workflow = {
		...workflow(),
		workflow_id: 'standard',
		name: 'Standard',
		version: 1,
		builtin: true,
		graph: { nodes: [node('start', 'start', 'Start'), node('implement', 'agent', 'Implement'), node('end', 'end', 'Done')], edges: [], budget: {} },
		node: 'implement',
		status: 'awaiting_review',
		visits: {},
		sequence: 0,
		steps: [],
		runs: [],
		pauses: [],
		profiles: []
	};
	state.task = baseTask({ state: 'awaiting_review', reason: 'Ready for review', provider: 'codex', model: 'gpt-5', run_id: 'run-3', generation: 3 });
	state.runs = [
		{ id: 'run-3', provider_id: 'codex', status: 'succeeded', created_at: at(9), started_at: at(10), completed_at: at(13), changed_files: [] },
		{ id: 'run-2', provider_id: 'codex', status: 'failed', created_at: at(6), started_at: at(7), completed_at: at(8), changed_files: [] },
		{ id: 'run-1', provider_id: 'claude-code', status: 'failed', created_at: at(1), started_at: at(2), completed_at: at(5), changed_files: [] }
	];
	const change = (sequence: number, from_state: string, to_state: string, minute: number, reason = '') => ({
		sequence,
		task_id: 'task-1',
		from_state,
		to_state,
		reason,
		created_at: at(minute)
	});
	state.transitions = [
		change(1, '', 'queued', 1, 'Created'),
		change(2, 'queued', 'running', 2),
		change(3, 'running', 'failed', 5, 'The build is broken'),
		change(4, 'failed', 'queued', 6, 'Retried'),
		change(5, 'queued', 'failed', 8, 'No machine'),
		change(6, 'failed', 'queued', 9, 'Retried'),
		change(7, 'queued', 'running', 10),
		change(8, 'running', 'awaiting_review', 13, 'Ready for review')
	];
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const rows = pathOf(page).getByRole('listitem');
	// Compact: the last outcome and the attempt the task is on.
	await expect(rows.getByRole('link')).toHaveText([/^Codex/, /^Codex/]);
	await expect(rows.nth(0).getByRole('img', { name: 'Failed' })).toBeVisible();
	await expect(rows.nth(0).getByRole('link')).toContainText('×2');
	await expect(rows.nth(1).getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(rows.nth(1).getByRole('link')).toContainText('×3');
	await expect(rows.nth(1).getByRole('link')).toContainText('10:10 AM · 3m');
	await expect(rows.nth(1).getByRole('link')).toHaveAttribute('href', /agent=task-1#run-run-3$/);
	// The attempt is over and the task waits: the path says why, once.
	await expect(rows.nth(1).getByTestId('task-path-note')).toHaveText('Ready for review');
	await expect(tasksRegion(page).getByText('Ready for review')).toHaveCount(1);
	expect(state.requests).not.toContain('GET task-1/workflow');

	await tasksRegion(page).getByRole('button', { name: 'Full path' }).click();
	await expect(rows.getByRole('link')).toHaveText([/^Claude Code/, /^Codex/, /^Codex/]);
	// An attempt opens to its state changes, each with its time.
	await rows.nth(0).getByRole('button', { name: 'Details of Claude Code' }).click();
	const changes = rows.nth(0).getByTestId('task-path-detail').locator('p');
	await expect(changes).toHaveText([/^Queued · Created\s*10:01 AM$/, /^Running\s*10:02 AM$/, /^Failed · The build is broken\s*10:05 AM$/]);
	await expect(changes.nth(2).locator('time')).toHaveAttribute('title', /Oct 26, 2026, 10:05 AM/);

	// The workflow panel draws the Standard flow and shows the same path under it.
	await page.getByRole('button', { name: 'Workflow', exact: true }).first().click();
	const panel = page.getByTestId('workflow-panel');
	await expect(panel.getByTestId('task-workflow')).toContainText('Standard · version 1');
	await expect(panel.getByRole('list', { name: 'Path taken' }).getByRole('link')).toHaveText([/^Codex/, /^Codex/]);
	expect(count(state.requests, 'GET task-1/workflow')).toBe(1);
});

test('the path fits a phone, in the sheet, and its links leave the sheet', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const rows = pathOf(page).getByRole('listitem');
	await expect(rows).toHaveCount(5);
	await expect(pathOf(page).getByTestId('task-path-note')).toHaveText(['Waiting for CI on the pull request']);
	const sheet = page.getByRole('dialog');
	await expect(sheet).toBeVisible();
	const within = async () => {
		for (const row of await rows.all()) {
			const box = await row.boundingBox();
			expect(box!.x).toBeGreaterThanOrEqual(0);
			expect(box!.x + box!.width).toBeLessThanOrEqual(390);
		}
		// The name and the time are both readable on every step.
		for (const link of await rows.getByRole('link').all()) {
			await expect(link.locator('.font-medium')).toBeVisible();
			await expect(link.locator('time')).toBeVisible();
		}
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
	};
	await within();
	await shot(page, testInfo, 'path-compact-phone.png');
	await tasksRegion(page).getByRole('button', { name: 'Full path' }).click();
	await expect(rows).toHaveCount(12);
	await within();
	await page.screenshot({ path: testInfo.outputPath('path-expanded-phone.png') });

	// A step opens in the agent view, which the sheet would cover.
	await rows.nth(5).getByRole('link').click();
	await expect(page).toHaveURL(/agent=task-1#step-6$/);
	await expect(sheet).toBeHidden();
	await expect(page.locator('#step-6').getByRole('button').first()).toHaveAttribute('aria-expanded', 'true');
});

test('the sidebar has no composer and no review action; both are in the agent view', async ({ page }) => {
	const { state, emit } = await fixture(page);
	state.providers = [
		{ id: 'codex', display_name: 'Codex', supported_modes: ['autonomous'], custom: false },
		{ id: 'opencode', display_name: 'OpenCode', supported_modes: ['autonomous'], custom: false },
		{ id: 'claude-code', display_name: 'Claude Code', supported_modes: ['autonomous'], custom: false }
	];
	state.publication = {
		step: { id: 'publication-1', agent_run_id: 'run-impl-2', status: 'succeeded', command_argv: [], completed_at: at(25) },
		branch: 'kuayle/eng-1',
		pull_request_url: 'https://github.com/acme/app/pull/12'
	};
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1');
	await showAgents(page);
	const tasks = tasksRegion(page);
	await expect(pathOf(page).getByRole('listitem')).toHaveCount(5);
	await expect(tasks.getByRole('link', { name: 'Open pull request' })).toBeVisible();

	// The card neither writes to the agent nor asks for a review.
	await expect(tasks.getByRole('textbox')).toHaveCount(0);
	await expect(tasks.getByRole('region', { name: 'Live messages' })).toHaveCount(0);
	await expect(tasks.getByRole('button', { name: 'Send', exact: true })).toHaveCount(0);
	await tasks.getByRole('button', { name: 'More actions', exact: true }).click();
	await expect(page.getByRole('menuitem', { name: 'Open environment' })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: /review/i })).toHaveCount(0);
	await page.keyboard.press('Escape');

	// The agent view writes to the agent…
	await pathOf(page).getByRole('listitem').last().getByRole('link').click();
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	await expect(thread).toBeVisible();
	await expect(page.getByRole('textbox', { name: 'Write to the agent' })).toBeVisible();

	// …and shows the reviews asked of the task's pull request.
	state.sessions = [
		{
			id: 'session-2',
			task_id: 'review-9',
			provider: 'claude-code',
			purpose: 'review',
			state: 'completed',
			created_at: at(31),
			updated_at: at(33),
			task_state: 'completed',
			review: {
				task_id: 'review-9',
				reviewed_task_id: 'task-1',
				requested_by: 'user-1',
				provider: 'claude-code',
				commit_sha: '0123456789abcdef0123456789abcdef01234567',
				repository: 'acme/app',
				pull_request_number: 12,
				pull_request_url: 'https://github.com/acme/app/pull/12',
				base_branch: 'main',
				focus: '',
				attempt: 1,
				post_to_pull_request: false,
				verdict: 'request_changes',
				findings: '- handle the **empty** query',
				comment_state: 'not_requested',
				created_at: at(31),
				updated_at: at(33)
			}
		}
	];
	emit('task.review_updated', { task_id: 'task-1', review_task_id: 'review-9', state: 'completed' });
	const reviews = thread.getByRole('region', { name: 'Agent reviews' });
	await expect(reviews.getByText('Review by Claude Code', { exact: true })).toBeVisible();
	await expect(reviews.getByText('handle the empty query')).toBeVisible();
	await expect(tasks.getByRole('region', { name: 'Agent reviews' })).toHaveCount(0);

	// A review is asked for from its header: nothing is started, the request
	// is written for the assistant, which asks for approval.
	const sent: string[] = [];
	page.on('request', (request) => {
		if (request.method() !== 'GET') sent.push(request.url());
	});
	await thread.getByRole('button', { name: 'Request review', exact: true }).click();
	const items = page.getByRole('menu').getByRole('menuitem');
	await expect(items).toHaveText([/OpenCode\s*Same agent$/, /Claude Code$/, /Codex$/]);
	await page.getByRole('menuitem', { name: 'Claude Code' }).click();
	await expect(page).not.toHaveURL(/agent=/);
	const composer = page.getByRole('textbox', { name: 'Message', exact: true });
	await expect(composer).toHaveValue('Review ENG-1 with Claude Code');
	await expect(composer).toBeFocused();
	expect(sent).toEqual([]);
});
