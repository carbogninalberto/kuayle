import { test, expect, type Page, type TestInfo } from '@playwright/test';

// The task page layout (KUA-281): in the agent view a task column (phases,
// members, budget) sits left of the room and the side panel right of it, on
// one resizable pane group whose side panes close and keep their sizes. The
// conversations fold to a rail while a task is open. Below the wide
// breakpoint there is one column and the panes open as sheets. Served from
// mocked API routes.
test.use({ timezoneId: 'UTC', locale: 'en-US' });

const at = (minute: number) => `2026-11-25T10:${String(minute).padStart(2, '0')}:00Z`;
const conversation = {
	id: 'chat-1',
	workspace_id: 'workspace-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	title: 'Implement search',
	created_at: at(0),
	updated_at: at(0)
};
const issue = { id: 'issue-1', identifier: 'ENG-1', title: 'Implement search', status: 'todo', description: '' };

const baseTask = (over: Record<string, unknown> = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	prompt: 'Add search to the issue list.',
	provider: 'claude-code',
	model: 'claude-opus-5-5',
	thinking: '',
	state: 'awaiting_review',
	reason: 'Waiting for the reviewer’s verdict on abc1234',
	generation: 1,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl-1',
	kind: 'implement',
	workflow_version_id: 'version-1',
	workflow_node: 'review',
	workflow_status: 'waiting',
	created_at: at(0),
	updated_at: at(8),
	...over
});
const profile = (id: string, name: string, harness: string, model: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: '',
	behaviour: 'changes_code',
	harness,
	model,
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: at(0),
	updated_at: at(0)
});
const run = (id: string, taskId: string, role: string | null, provider: string, status: string, minute: number) => ({
	id,
	task_id: taskId,
	role,
	provider,
	model: provider === 'codex' ? 'gpt-5' : 'claude-opus-5-5',
	status,
	created_at: at(minute),
	...(status === 'succeeded' ? { completed_at: at(minute + 2) } : {})
});

// A team pipeline at its review phase: the implementer's turn ended and the
// reviewer works on its second turn.
const pipelineTask = baseTask({ pipeline: true });
const pipelineView = {
	workflow_id: 'workflow-team',
	name: 'Team',
	version: 1,
	builtin: false,
	graph: {
		kind: 'pipeline',
		phases: [
			{ id: 'implement', label: 'Implement', owner: 'implementer' },
			{ id: 'review', label: 'Review', owner: 'implementer', checks: [{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true }] },
			{ id: 'integrate', label: 'CI checks', owner: 'implementer' },
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		],
		roster: [
			{ role: 'implementer', profile_id: 'p-impl', writer: true },
			{ role: 'reviewer', profile_id: 'p-rev' }
		],
		budget: { max_turns: 40 }
	},
	node: 'review',
	status: 'waiting',
	visits: {},
	iterations: 7,
	sequence: 4,
	steps: [],
	runs: [
		run('run-impl-1', 'task-1', 'implementer', 'claude-code', 'succeeded', 1),
		run('run-rev-1', 'member-rev', 'reviewer', 'codex', 'succeeded', 4),
		run('run-rev-2', 'member-rev', 'reviewer', 'codex', 'running', 7)
	],
	usage: { input_tokens: 120_000, output_tokens: 8_000, cost_usd: 1.25 },
	profiles: [profile('p-impl', 'Implementer', 'claude-code', 'claude-opus-5-5'), profile('p-rev', 'Reviewer', 'codex', 'gpt-5')],
	can_decide: false,
	task_state: 'awaiting_review',
	task_reason: '',
	task_generation: 1
};
const post = (seq: number, author_kind: string, kind: string, body: string, extra: Record<string, unknown> = {}) => ({
	id: `message-${seq}`,
	task_id: 'task-1',
	seq,
	author_kind,
	author_role: null,
	author_user_id: author_kind === 'human' ? 'user-1' : null,
	author_member_id: null,
	run_id: null,
	to_roles: [],
	kind,
	body,
	data: {},
	reply_to: null,
	reject_reason: null,
	created_at: at(seq),
	...extra
});
const room = [
	post(1, 'human', 'say', 'Search should cover titles and identifiers.'),
	post(2, 'kuayle', 'phase', '', { data: { phase: 'implement' } }),
	post(3, 'kuayle', 'phase', '', { data: { phase: 'review' } })
];

// A graph workflow at its review step, with an iteration budget.
const graphTask = baseTask({ id: 'task-2', run_id: 'run-g-2', state: 'running', workflow_status: 'running', reason: '' });
const step = (sequence: number, node_id: string, node_type: string, minute: number, over: Record<string, unknown> = {}) => ({
	task_id: 'task-2',
	sequence,
	node_id,
	node_type,
	visit: 1,
	reason: '',
	detail: '',
	entered_at: at(minute),
	...over
});
const graphView = {
	workflow_id: 'workflow-review',
	name: 'Review loop',
	version: 2,
	builtin: false,
	graph: {
		nodes: [
			{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
			{ id: 'implement', type: 'agent', label: 'Implement', profile_id: 'p-impl', position: { x: 300, y: 0 } },
			{ id: 'review', type: 'agent', label: 'Review', profile_id: 'p-rev', position: { x: 600, y: 0 } },
			{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: { x: 900, y: 0 } }
		],
		edges: [],
		budget: { max_iterations: 5 }
	},
	node: 'review',
	status: 'running',
	visits: { start: 1, implement: 1, review: 1 },
	iterations: 2,
	sequence: 3,
	steps: [
		step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
		step(2, 'implement', 'agent', 0, { generation: 1, outcome: 'agent_finished', finished_at: at(5) }),
		step(3, 'review', 'agent', 5, { generation: 2 })
	],
	runs: [
		{ ...run('run-g-1', 'task-2', null, 'claude-code', 'succeeded', 1), step: 2 },
		{ ...run('run-g-2', 'task-2', null, 'codex', 'running', 5), step: 3 }
	],
	profiles: pipelineView.profiles,
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 2
};

async function fixture(page: Page) {
	const tasks = [pipelineTask, graphTask];
	const views: Record<string, unknown> = { 'task-1': pipelineView, 'task-2': graphView };
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url()),
			path = url.pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status')) return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path.endsWith('/task-options')) return route.fulfill({ json: { providers: [], model_servers: [] } });
		if (path.endsWith('/conversations/chat-1/tasks')) return route.fulfill({ json: { tasks } });
		if (path.endsWith('/events')) return route.fulfill({ json: [] });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false } });
		const match = path.match(/\/agent-tasks\/([^/]+)(\/.*)?$/);
		if (match) {
			const [, id, rest = ''] = match;
			if (rest === '/workflow') return route.fulfill({ json: views[id] });
			if (rest === '/room')
				return route.fulfill({ json: { messages: room.filter((message) => message.seq > Number(url.searchParams.get('after') ?? 0)) } });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/notebook') return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (rest.endsWith('/logs')) return route.fulfill({ json: { logs: [] } });
			const found = tasks.find((task) => task.id === id);
			if (found && rest === '') return route.fulfill({ json: found });
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ json: [] });
	});
}

const agentView = (id = 'task-1') => `/test/assistant?conversation=chat-1&agent=${id}`;
const column = (page: Page) => page.getByTestId('task-column');
const toggle = (page: Page) => page.getByRole('button', { name: 'Task overview', exact: true });
const panels = (page: Page) => page.getByRole('group', { name: 'Side panels' });
const width = async (page: Page, testId: string) => (await page.getByTestId(testId).boundingBox())?.width ?? 0;

async function dark(page: Page) {
	await page.evaluate(() => {
		document.documentElement.classList.add('dark');
		document.documentElement.setAttribute('data-theme', 'dark');
	});
}
async function shot(page: Page, testInfo: TestInfo, name: string) {
	await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`${name}.png`) });
}

test('the task column shows the phases, who works and the budget beside the room', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView());
	await expect(page.getByTestId('agent-thread')).toBeVisible();

	const phases = column(page).getByRole('list', { name: 'Phases' });
	await expect(phases.getByRole('listitem')).toHaveText([/Implement/, /Review/, /CI checks/, /Merge/]);
	const current = phases.locator('[aria-current="step"]');
	await expect(current).toHaveCount(1);
	await expect(current).toContainText('Review');
	// What it waits for, under the current phase; the phase before it passed.
	await expect(current).toContainText('Waiting for the reviewer’s verdict on abc1234');
	await expect(phases.getByRole('listitem').first().getByRole('img', { name: 'Done' })).toBeVisible();

	const members = column(page).getByRole('list', { name: 'Members' });
	await expect(members.getByRole('listitem')).toHaveCount(2);
	await expect(members.getByRole('listitem').filter({ hasText: 'reviewer' })).toContainText('Working');
	await expect(members.getByRole('listitem').filter({ hasText: 'reviewer' })).toContainText('Turns: 2');
	await expect(members.getByRole('listitem').filter({ hasText: 'implementer' })).toContainText('Idle');

	const budget = column(page).getByTestId('task-column-budget');
	await expect(budget).toContainText('7 of 40 turns');
	await expect(budget.getByRole('progressbar')).toHaveAttribute('aria-valuenow', String((7 / 40) * 100));
	await expect(budget).toContainText('$1.25');

	// The conversations fold to their rail while the task is open, and open again on request.
	await expect(page.getByTestId('assistant-sidebar')).toHaveCount(0);
	const rail = page.getByTestId('assistant-sidebar-rail');
	await expect(rail).toBeVisible();
	expect(await width(page, 'assistant-sidebar-rail')).toBeLessThan(64);
	await expect(rail.getByRole('link', { name: 'New conversation' })).toBeVisible();
	await shot(page, testInfo, 'task-layout-desktop-light');
	await dark(page);
	await shot(page, testInfo, 'task-layout-desktop-dark');

	await rail.getByRole('button', { name: 'Show conversations' }).click();
	await expect(page.getByTestId('assistant-sidebar')).toBeVisible();
	await expect(rail).toHaveCount(0);
	await page.getByRole('button', { name: 'Hide conversations' }).click();
	await expect(page.getByTestId('assistant-sidebar-rail')).toBeVisible();
	// Back in the chat the conversations are a column again.
	await page.getByTestId('agent-thread-header').getByRole('link', { name: 'Assistant' }).click();
	await expect(page).not.toHaveURL(/agent=/);
	await expect(page.getByTestId('assistant-sidebar')).toBeVisible();
	await expect(page.getByTestId('task-column')).toHaveCount(0);
});

test('the room takes the width the closed side panel leaves', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 1600, height: 900 });
	await page.goto(agentView());
	await expect(page.getByTestId('agent-thread')).toBeVisible();
	// A wide screen opens the agents panel by itself: the room keeps its reading width.
	await expect(page.getByTestId('side-panel')).toBeVisible();
	expect(await width(page, 'agent-thread')).toBeLessThanOrEqual(768);
	await panels(page).getByRole('button', { pressed: true }).click();
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
	await expect.poll(() => width(page, 'agent-thread')).toBeGreaterThan(800);
	await expect.poll(() => width(page, 'agent-composer')).toBeGreaterThan(800);
	await shot(page, testInfo, 'task-layout-wide-room-light');
});

test('the side panes close, open and keep their sizes across reloads', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView());
	await expect(column(page)).toBeVisible();
	await expect(toggle(page)).toHaveAttribute('aria-pressed', 'true');

	// Wider by keyboard on its handle, and as wide after a reload.
	const before = await width(page, 'task-column');
	await page.getByRole('separator', { name: 'Resize task overview' }).focus();
	await page.keyboard.press('ArrowRight');
	await expect.poll(() => width(page, 'task-column')).toBeGreaterThan(before + 40);
	const resized = await width(page, 'task-column');
	await page.waitForTimeout(250);
	await page.reload();
	await expect(column(page)).toBeVisible();
	await expect.poll(() => width(page, 'task-column')).toBeGreaterThan(resized - 3);
	expect(await width(page, 'task-column')).toBeLessThan(resized + 3);

	// Closed from its own header, still closed after a reload, opened from the top bar.
	await column(page).getByRole('button', { name: 'Hide task overview' }).click();
	await expect(column(page)).toHaveCount(0);
	await expect(toggle(page)).toHaveAttribute('aria-pressed', 'false');
	await expect(toggle(page)).toBeFocused();
	await page.reload();
	await expect(page.getByTestId('agent-thread')).toBeVisible();
	await expect(column(page)).toHaveCount(0);
	await toggle(page).click();
	await expect(column(page)).toBeVisible();
	await expect.poll(() => width(page, 'task-column')).toBeGreaterThan(resized - 3);

	// The side panel closes and stays closed; the task column is not touched by it.
	await expect(page.getByTestId('side-panel')).toBeVisible();
	await panels(page).getByRole('button', { pressed: true }).click();
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
	await page.reload();
	await expect(column(page)).toBeVisible();
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
	// The room stays mounted while a pane opens: what is typed is kept.
	const composer = page.getByRole('textbox', { name: 'Write to the room' });
	await composer.fill('Keep this draft');
	await panels(page).getByRole('button', { name: 'Workflow' }).click();
	await expect(page.getByTestId('side-panel')).toBeVisible();
	await column(page).getByRole('button', { name: 'Hide task overview' }).click();
	await expect(composer).toHaveValue('Keep this draft');
});

test('a graph-workflow task shows its steps and its iteration budget', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView('task-2'));
	const path = column(page).getByTestId('task-path');
	await expect(path).toBeVisible();
	await expect(path.locator('[aria-current="step"]')).toContainText('Review');
	await expect(column(page).getByRole('list', { name: 'Members' })).toHaveCount(0);
	await expect(column(page).getByTestId('task-column-budget')).toContainText('2 of 5 iterations');
});

test('at phone width the page is one column and the panes open as sheets', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(agentView());
	await expect(page.getByTestId('agent-thread')).toBeVisible();
	await expect(column(page)).toHaveCount(0);
	await expect(page.getByTestId('assistant-sidebar-rail')).toHaveCount(0);
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
	await shot(page, testInfo, 'task-layout-phone-light');

	await toggle(page).click();
	const sheet = page.getByRole('dialog', { name: 'Task overview' });
	await expect(sheet).toBeVisible();
	await expect(sheet.getByRole('list', { name: 'Phases' }).locator('[aria-current="step"]')).toContainText('Review');
	await expect(sheet.getByRole('list', { name: 'Members' })).toBeVisible();
	await dark(page);
	await shot(page, testInfo, 'task-layout-phone-sheet-dark');
	await sheet.getByRole('button', { name: 'Hide task overview' }).click();
	await expect(sheet).toHaveCount(0);
	await expect(toggle(page)).toBeFocused();

	await morePanels(page).click();
	await page.getByRole('menuitemcheckbox', { name: 'Workflow' }).click();
	await expect(page.getByRole('dialog', { name: 'Workflow' })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(page.getByRole('dialog')).toHaveCount(0);
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await shot(page, testInfo, 'task-layout-phone-dark');

	// The narrowest phone keeps the top bar's controls in the window.
	await page.setViewportSize({ width: 320, height: 700 });
	await expect(toggle(page)).toBeInViewport();
	await expect(morePanels(page)).toBeInViewport();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	// Widening the window shows the column without popping a sheet open.
	await page.setViewportSize({ width: 1440, height: 900 });
	await expect(column(page)).toBeVisible();
	await expect(page.getByRole('dialog')).toHaveCount(0);
});

// Six panels (Details, Agents, Workflow, Files, Notebook, Checks) as icons
// would leave a narrow top bar's title a few letters wide: below its width
// threshold the bar shows the open panel and one "More panels" menu.
const morePanels = (page: Page) => panels(page).getByRole('button', { name: 'More panels', exact: true });
const titleMenu = (page: Page) => page.getByRole('button', { name: 'Implement search: rename or switch conversation' });
const allPanels = ['Details', 'Agents', 'Workflow', 'Files', 'Notebook', 'Checks'];

test('a narrow top bar keeps its title and lists the panels in one menu', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(agentView());
	await expect(page.getByTestId('agent-thread')).toBeVisible();
	await expect(morePanels(page)).toBeVisible();
	await expect(panels(page).getByRole('button')).toHaveCount(1);
	expect((await titleMenu(page).boundingBox())!.width).toBeGreaterThan(100);

	// Keyboard only: the menu names every panel, says which is open and opens one as a sheet.
	await morePanels(page).focus();
	await page.keyboard.press('Enter');
	const menu = page.getByRole('menu');
	await expect(menu.getByRole('menuitemcheckbox')).toHaveText(allPanels);
	await expect(menu.getByRole('menuitemcheckbox', { checked: true })).toHaveCount(0);
	await expect(menu.getByRole('menuitemcheckbox', { name: 'Details' })).toBeFocused();
	await page.keyboard.press('ArrowDown');
	await page.keyboard.press('ArrowDown');
	await expect(menu.getByRole('menuitemcheckbox', { name: 'Workflow' })).toBeFocused();
	await page.keyboard.press('Enter');
	const sheet = page.getByRole('dialog', { name: 'Workflow' });
	await expect(sheet).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(sheet).toHaveCount(0);
	await expect(morePanels(page)).toBeFocused();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	// A wide bar shows every panel and no menu.
	await page.setViewportSize({ width: 1280, height: 900 });
	await expect(column(page)).toBeVisible();
	await column(page).getByRole('button', { name: 'Hide task overview' }).click();
	for (const name of allPanels) await expect(panels(page).getByRole('button', { name, exact: true })).toBeVisible();
	await expect(morePanels(page)).toHaveCount(0);
	await panels(page).getByRole('button', { name: 'Workflow', exact: true }).click();
	await expect(page.getByTestId('side-panel')).toBeVisible();
	for (const name of allPanels) await expect(panels(page).getByRole('button', { name, exact: true })).toBeVisible();

	// Beside the task column and the side panel the bar is narrow again: the open
	// panel stays one press away, the others are in the menu, where it is checked.
	await toggle(page).click();
	await expect(column(page)).toBeVisible();
	await expect(morePanels(page)).toBeVisible();
	await expect(panels(page).getByRole('button')).toHaveCount(2);
	await expect(panels(page).getByRole('button', { pressed: true })).toHaveAccessibleName('Workflow');
	await morePanels(page).click();
	await expect(menu.getByRole('menuitemcheckbox', { checked: true })).toHaveText('Workflow');
	await menu.getByRole('menuitemcheckbox', { name: 'Checks' }).click();
	await expect(page.getByTestId('side-panel').getByRole('region', { name: 'Checks', exact: true })).toBeVisible();
	await expect(panels(page).getByRole('button', { pressed: true })).toHaveAccessibleName('Checks');
	// Choosing the open panel again closes it.
	await morePanels(page).click();
	await menu.getByRole('menuitemcheckbox', { name: 'Checks' }).click();
	await expect(page.getByTestId('side-panel')).toHaveCount(0);
});

// One rule for the side panel's own switcher, by its width: every tab shows
// its icon and the open one its name too; narrower (the phone's sheet too),
// icons only; narrower still, the open tab and a "More panels" menu. The tabs
// always fit.
test('the side panel names its open tab and its tabs fit at any width', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 1600, height: 900 });
	await page.goto(agentView());
	const side = page.getByTestId('side-panel');
	await expect(side).toBeVisible();
	const tabs = side.getByRole('toolbar', { name: 'Side panels' });
	const tab = tabs.getByRole('button', { pressed: true });
	const more = tabs.getByRole('button', { name: 'More panels', exact: true });
	const named = () => tabs.getByRole('button').evaluateAll((buttons) => buttons.map((button) => button.getBoundingClientRect().width > 40));
	const fits = () =>
		tabs.evaluate((toolbar) => {
			const box = toolbar.getBoundingClientRect();
			return [...toolbar.querySelectorAll('button')].every((button) => button.getBoundingClientRect().right <= box.right + 0.5);
		});
	// Wide: the open tab (Agents) shows its name; the others their icon.
	await expect(tabs.getByRole('button')).toHaveCount(allPanels.length);
	await expect(tab).toHaveAccessibleName('Agents');
	await expect.poll(named).toEqual(allPanels.map((name) => name === 'Agents'));
	expect(await fits()).toBe(true);

	// Narrower: icons only, every panel still one press away.
	await page.setViewportSize({ width: 1280, height: 900 });
	await expect.poll(named).toEqual(allPanels.map(() => false));
	await expect(tabs.getByRole('button')).toHaveCount(allPanels.length);
	await expect(more).toHaveCount(0);
	expect(await fits()).toBe(true);

	// At its narrowest the column keeps the open tab and lists the panels in a menu.
	await page.getByRole('separator', { name: 'Resize side panel' }).focus();
	for (let i = 0; i < 12; i++) await page.keyboard.press('ArrowRight');
	await expect.poll(() => width(page, 'side-panel')).toBeLessThan(240);
	await expect(more).toBeVisible();
	await expect(tabs.getByRole('button')).toHaveCount(2);
	await expect(tab).toHaveAccessibleName('Agents');
	expect(await fits()).toBe(true);
	await more.focus();
	await page.keyboard.press('Enter');
	const menu = page.getByRole('menu');
	await expect(menu.getByRole('menuitemradio')).toHaveText(allPanels);
	await expect(menu.getByRole('menuitemradio', { checked: true })).toHaveText('Agents');
	await menu.getByRole('menuitemradio', { name: 'Workflow' }).press('Enter');
	await expect(side.getByTestId('workflow-panel')).toBeVisible();
	await expect(tab).toHaveAccessibleName('Workflow');

	// The phone's sheet has room for every icon, not for a name.
	await page.setViewportSize({ width: 390, height: 844 });
	await morePanels(page).click();
	await page.getByRole('menuitemcheckbox', { name: 'Checks' }).click();
	const sheet = page.getByRole('dialog', { name: 'Checks' });
	await expect(sheet).toBeVisible();
	const sheetTabs = sheet.getByRole('toolbar', { name: 'Side panels' });
	await expect(sheetTabs.getByRole('button', { pressed: true })).toHaveAccessibleName('Checks');
	await expect(sheetTabs.getByRole('button')).toHaveCount(allPanels.length);
	await expect
		.poll(() => sheetTabs.getByRole('button').evaluateAll((buttons) => buttons.map((button) => button.getBoundingClientRect().width > 40)))
		.toEqual(allPanels.map(() => false));
	expect(await sheetTabs.evaluate((toolbar) => toolbar.scrollWidth <= toolbar.clientWidth)).toBe(true);
});
