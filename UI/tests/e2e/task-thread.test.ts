import { test, expect, type Page, type TestInfo, type WebSocketRoute } from '@playwright/test';

// A quieter task thread (KUA-286): one line per event, Kuayle's facts in a
// row folded into one line, filters by role and to the decisions, the same
// lines for a graph workflow's steps, and the Checks tab where the owner
// approves or waives. Served from mocked API routes.
const at = (minute: number) => `2026-11-25T10:${String(minute).padStart(2, '0')}:00Z`;
const HEAD = 'abc1234def5678abc1234def5678abc1234def56';
const text = (value: string) => ({ type: 'assistant', message: { content: [{ type: 'text', text: value }] } });

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
	reason: 'Integrate: 1 check(s) failed on abc1234: lint.',
	generation: 2,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl-2',
	kind: 'implement',
	workflow_version_id: 'version-1',
	workflow_node: 'integrate',
	workflow_status: 'waiting',
	pipeline: true,
	created_at: at(0),
	updated_at: at(12),
	...over
});

const run = (id: string, taskId: string, provider: string, model: string, status: string, minute: number, over = {}) => ({
	id,
	task_id: taskId,
	provider,
	model,
	status,
	created_at: at(minute),
	...(status === 'succeeded' ? { completed_at: at(minute + 3) } : {}),
	...over
});

const pipelineView = (over: Record<string, unknown> = {}) => ({
	workflow_id: 'workflow-team',
	name: 'Team',
	version: 1,
	builtin: false,
	graph: {
		kind: 'pipeline',
		phases: [
			{ id: 'implement', label: 'Implement', owner: 'implementer', checks: [{ id: 'pr', fact: 'pr_exists', required: true }] },
			{
				id: 'integrate',
				label: 'Integrate',
				owner: 'kuayle',
				checks: [
					{ id: 'ci', fact: 'ci_green', required: true },
					{ id: 'migrations', fact: 'migration_collision' }
				]
			},
			{ id: 'review', label: 'Review', owner: 'implementer', checks: [{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true }] },
			{ id: 'approval', label: 'Approval', owner: 'human', checks: [{ id: 'owner', fact: 'approved', required: true }] },
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		],
		roster: [
			{ role: 'implementer', profile_id: 'p-impl', writer: true },
			{ role: 'reviewer', profile_id: 'p-rev' }
		]
	},
	node: 'integrate',
	status: 'waiting',
	visits: {},
	iterations: 3,
	sequence: 9,
	steps: [],
	runs: [
		run('run-impl-1', 'task-1', 'claude-code', 'claude-opus-5-5', 'succeeded', 1, {
			role: 'implementer',
			summary: 'Search is added.\n\nThe endpoint and its tests.',
			input_tokens: 1000,
			output_tokens: 200,
			cache_read_tokens: 50000
		}),
		run('run-rev-1', 'member-rev', 'codex', 'gpt-5', 'succeeded', 6, { role: 'reviewer' }),
		run('run-impl-2', 'task-1', 'claude-code', 'claude-opus-5-5', 'running', 11, { role: 'implementer' })
	],
	profiles: [],
	can_decide: true,
	task_state: 'awaiting_review',
	task_reason: '',
	task_generation: 2,
	...over
});

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
const member = (seq: number, role: string, run_id: string, kind: string, body: string, extra: Record<string, unknown> = {}) =>
	post(seq, 'member', kind, body, { author_role: role, author_member_id: `member-${role}`, run_id, ...extra });
const fact = (seq: number, name: string, value: unknown, body = '') => post(seq, 'kuayle', 'fact', body, { data: { fact: name, value } });

const room = () => [
	post(1, 'human', 'say', 'Search should cover titles and identifiers.'),
	post(2, 'kuayle', 'phase', '', { data: { phase: 'implement' } }),
	member(3, 'implementer', 'run-impl-1', 'say', 'Pushed the first version.', { to_roles: ['reviewer'] }),
	fact(4, 'pr_exists', true),
	fact(5, 'head_sha', HEAD),
	fact(6, 'ci_green', { state: 'pending', head: HEAD, failing: [] }, '1 of 3 check(s) are still running on abc1234. Kuayle waits for them.'),
	fact(7, 'ci_green', { state: 'failing', head: HEAD, failing: ['lint'] }, '1 check(s) failed on abc1234: lint. Next step: read their output, fix the cause and push.'),
	fact(8, 'migration_collision', false),
	post(9, 'kuayle', 'phase', '', { data: { phase: 'integrate' } }),
	member(10, 'reviewer', 'run-rev-1', 'verdict', 'Rename q to query.', {
		data: { verdict: 'request_changes', commit: HEAD.slice(0, 7), role: 'reviewer' }
	}),
	member(11, 'implementer', 'run-impl-2', 'say', 'Fixing lint.')
];

// A graph workflow task: an implementer, publish and CI in a row, a reviewer
// asking for changes, and the implementer's second visit.
const node = (id: string, type: string, label: string, profile_id?: string) => ({ id, type, label, profile_id, position: { x: 0, y: 0 } });
const step = (sequence: number, node_id: string, node_type: string, entered: number, over: Record<string, unknown> = {}) => ({
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
const profile = (id: string, name: string, harness: string, model: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: '',
	behaviour: '',
	harness,
	model,
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: at(0),
	updated_at: at(0)
});
const graphView = () => ({
	workflow_id: 'workflow-1',
	name: 'Review loop',
	version: 1,
	builtin: false,
	graph: {
		nodes: [
			node('start', 'start', 'Start'),
			node('implement', 'agent', 'Implement', 'p-impl'),
			node('publish', 'publish', 'Publish'),
			node('ci', 'check', 'CI'),
			node('review', 'agent', 'Review', 'p-rev')
		],
		edges: [],
		budget: {}
	},
	node: 'implement',
	status: 'running',
	visits: {},
	iterations: 2,
	sequence: 6,
	steps: [
		step(1, 'start', 'start', 0, { outcome: 'else', finished_at: at(0) }),
		step(2, 'implement', 'agent', 0, { generation: 1, outcome: 'agent_finished', finished_at: at(4) }),
		step(3, 'publish', 'publish', 4, { outcome: 'published', finished_at: at(5) }),
		step(4, 'ci', 'check', 5, { outcome: 'checks_passed', finished_at: at(7) }),
		step(5, 'review', 'agent', 7, { review_task_id: 'review-1', outcome: 'review_changes_requested', finished_at: at(9) }),
		step(6, 'implement', 'agent', 9, { generation: 2, visit: 2 })
	],
	runs: [
		run('run-impl-1', 'task-1', 'claude-code', 'claude-opus-5-5', 'succeeded', 0, { step: 2, summary: 'Search is added.' }),
		run('run-review', 'review-1', 'codex', 'gpt-5', 'succeeded', 7, { step: 5 }),
		run('run-impl-2', 'task-1', 'claude-code', 'claude-opus-5-5', 'running', 9, { step: 6 })
	],
	profiles: [profile('p-impl', 'Implementer', 'claude-code', 'claude-opus-5-5'), profile('p-rev', 'Reviewer', 'codex', 'gpt-5')],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 2
});

async function fixture(page: Page, options: { task?: Record<string, unknown>; workflow?: Record<string, unknown>; room?: any[] } = {}) {
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
	const state = {
		task: baseTask(options.task),
		workflow: options.workflow ?? pipelineView(),
		room: options.room ?? room(),
		posts: [] as any[],
		decisions: [] as any[],
		sockets: [] as WebSocketRoute[],
		events: [] as any[]
	};
	const logs: Record<string, any[]> = {
		'run-impl-1': [text('Starting with the endpoint.'), text('Search is added.')].map((value, i) => ({
			id: i + 1,
			agent_run_id: 'run-impl-1',
			stream: 'stdout',
			sequence: i + 1,
			content: JSON.stringify(value) + '\n',
			truncated: false,
			created_at: at(1)
		}))
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', (socket) => state.sockets.push(socket));
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
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [state.task] } });
		if (path.endsWith('/events'))
			return route.fulfill({ json: state.events.filter((e) => e.cursor > Number(url.searchParams.get('after') ?? 0)) });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false } });
		const match = path.match(/\/agent-tasks\/([^/]+)(\/.*)?$/);
		if (match) {
			const [, id, rest = ''] = match;
			if (rest === '/workflow') return route.fulfill({ json: state.workflow });
			if (rest === '/workflow/decision') {
				state.decisions.push(route.request().postDataJSON());
				return route.fulfill({ json: { task_id: id, state: 'awaiting_review', workflow_sequence: 10 } });
			}
			if (rest === '/room' && route.request().method() === 'POST') {
				const input = route.request().postDataJSON();
				state.posts.push(input);
				const message = post(state.room.length + 1, 'human', input.kind, input.body ?? '', { data: input.data ?? {} });
				state.room.push(message);
				return route.fulfill({ status: 201, json: message });
			}
			if (rest === '/room') {
				const after = Number(url.searchParams.get('after') ?? 0);
				return route.fulfill({ json: { messages: state.room.filter((message) => message.seq > after) } });
			}
			const logRoute = rest.match(/^\/runs\/([^/]+)\/logs$/);
			if (logRoute) return route.fulfill({ json: { logs: logs[logRoute[1]] ?? [] } });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/runs') return route.fulfill({ json: { runs: [], usage: {} } });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/notebook') return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (rest === '/publication') return route.fulfill({ json: { step: null } });
			if (id === state.task.id && rest === '') return route.fulfill({ json: state.task });
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		}
		return route.fulfill({ json: [] });
	});
	return { state };
}

const agentView = '/test/assistant?conversation=chat-1&agent=task-1';
const thread = (page: Page) => page.getByRole('region', { name: 'Agent thread of ENG-1' });
const filters = (page: Page) => thread(page).getByRole('group', { name: 'Filter the thread' });
async function chooseRole(page: Page, role: string) {
	await filters(page).getByRole('combobox', { name: 'Role to show' }).click();
	await page.getByRole('option', { name: role, exact: true }).click();
}
async function dark(page: Page, on: boolean) {
	await page.evaluate((value) => {
		document.documentElement.classList.toggle('dark', value);
		document.documentElement.setAttribute('data-theme', value ? 'dark' : 'light');
	}, on);
}
async function shots(page: Page, testInfo: TestInfo, name: string) {
	for (const on of [false, true]) {
		await dark(page, on);
		await page.screenshot({ animations: 'disabled', path: testInfo.outputPath(`${name}-${on ? 'dark' : 'light'}.png`) });
	}
	await dark(page, false);
}

test('the room is one line per event, folds Kuayle’s facts and filters by role or to the decisions', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView);
	const rows = thread(page).getByRole('list', { name: 'Task room' }).locator(':scope > li');
	await expect(rows).toHaveCount(7);

	// The header: the issue, the title, the phase with the task's state, who
	// works now, stop and a menu with the trace.
	const header = thread(page).getByTestId('agent-thread-header');
	await expect(header.getByRole('status')).toHaveText('Integrate · Awaiting review');
	await expect(header.getByTestId('thread-working')).toHaveAttribute('title', '1 working');
	await header.getByRole('button', { name: 'More actions' }).click();
	await expect(page.getByRole('menuitem', { name: 'Download trace' })).toBeVisible();
	await page.keyboard.press('Escape');

	// Five facts in a row are one line: what they say, CI's run from where
	// it started to where it ended; opened, each with Kuayle's reason.
	const facts = rows.nth(3).getByRole('article', { name: 'Kuayle' });
	const factsLine = facts.getByRole('button');
	await expect(factsLine).toContainText('Pull request opened · Head abc1234 · CI running, then CI failed · No migration collision');
	await factsLine.click();
	const read = facts.getByRole('list', { name: 'Facts Kuayle read' }).getByRole('listitem');
	await expect(read).toHaveCount(5);
	await expect(read.nth(3)).toContainText('CI failed');
	await expect(read.nth(3)).toContainText('1 check(s) failed on abc1234: lint.');
	await factsLine.click();

	// A turn is one line: the role, its final message's first line, its time,
	// the tokens it worked through and how it went.
	const first = rows.nth(2).getByRole('article', { name: 'implementer' }).getByRole('button');
	await expect(first).toContainText('implementer — Search is added.');
	await expect(first).toContainText('3m 0s');
	await expect(first).toContainText('1.2K tokens');
	await expect(first.getByRole('img', { name: 'Done' })).toBeVisible();
	const verdict = rows.nth(5).getByRole('article', { name: 'reviewer' }).getByRole('button');
	await expect(verdict).toContainText('reviewer — Rename q to query.');
	await expect(verdict).toContainText('Changes requested');
	await shots(page, testInfo, 'thread-desktop');

	// One role's lines.
	await chooseRole(page, 'reviewer');
	await expect(rows).toHaveCount(1);
	await expect(rows.first()).toContainText('Rename q to query.');
	// The decisions: phase moves and verdicts.
	await chooseRole(page, 'Everyone');
	const decisions = filters(page).getByRole('button', { name: 'Decisions only' });
	await decisions.press('Space');
	await expect(decisions).toHaveAttribute('aria-pressed', 'true');
	await expect(rows).toHaveCount(3);
	await expect(rows.nth(0)).toContainText('Moved to Implement');
	await expect(rows.nth(1)).toContainText('Moved to Integrate');
	await expect(rows.nth(2)).toContainText('Changes requested');
	// Nothing left: the reader is told, and shows everything again.
	await chooseRole(page, 'implementer');
	await expect(rows).toHaveCount(0);
	await expect(thread(page).getByText('No line matches this filter.')).toBeVisible();
	await thread(page).getByRole('button', { name: 'Show everything' }).click();
	await expect(rows).toHaveCount(7);
	await expect(decisions).toHaveAttribute('aria-pressed', 'false');
});

test('a graph workflow’s steps are the same lines: Kuayle’s steps in a row fold, and the filters apply', async ({ page }) => {
	await fixture(page, {
		task: { pipeline: false, state: 'running', workflow_node: 'implement', workflow_status: 'running' },
		workflow: graphView()
	});
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto(agentView);
	const timeline = thread(page).getByRole('list', { name: 'Path taken' });
	const rows = timeline.locator(':scope > li');
	await expect(rows).toHaveCount(5);
	await expect(thread(page).getByTestId('agent-thread-header').getByRole('status')).toHaveText('Implement · Running');
	await expect(page.locator('#step-2').getByRole('button').first()).toContainText('Implement · Implementer — Search is added.');

	// Publish and CI are one line that opens to each.
	const fold = rows.nth(2).locator('[data-step="fold"]');
	await expect(fold.getByRole('button').first()).toHaveText('Publish · CI');
	await expect(page.locator('#step-3')).toBeHidden();
	await fold.getByRole('button').first().click();
	await expect(page.locator('#step-3')).toBeVisible();
	await expect(page.locator('#step-3')).toContainText('Publish');
	await expect(page.locator('#step-4')).toContainText('CI');

	await chooseRole(page, 'Reviewer');
	await expect(rows).toHaveCount(1);
	await expect(rows.first().locator('#step-5')).toBeVisible();
	await chooseRole(page, 'Everyone');
	await filters(page).getByRole('button', { name: 'Decisions only' }).click();
	await expect(rows).toHaveCount(1);
	await expect(rows.first().getByRole('img', { name: 'Changes requested' })).toBeVisible();
	await filters(page).getByRole('button', { name: 'Decisions only' }).click();

	// A link to a folded step opens its line.
	await page.goto(`${agentView}#step-4`);
	await expect(page.locator('#step-4')).toBeVisible();
	await expect(page.locator('[data-step="fold"]').getByRole('button').first()).toHaveAttribute('aria-expanded', 'true');
});

const checks = (page: Page) => page.getByRole('region', { name: 'Checks', exact: true });
const check = (page: Page, id: string) => checks(page).getByTestId(`check-${id}`);
async function openChecks(page: Page) {
	await page.getByRole('group', { name: 'Side panels' }).getByRole('button', { name: 'Checks', exact: true }).click();
	await expect(checks(page)).toBeVisible();
}

test('the Checks tab says how each check stands, why and since when, and the owner waives one', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView);
	await expect(thread(page).getByRole('list', { name: 'Task room' }).locator(':scope > li')).toHaveCount(7);
	await openChecks(page);
	await expect(checks(page)).toContainText('For the head abc1234');

	// The phase before the current one passed; the current one says what blocks it.
	await expect(check(page, 'pr')).toHaveAttribute('data-state', 'met');
	await expect(checks(page).getByTestId('checks-phase-integrate')).toContainText('Current');
	const ci = check(page, 'ci');
	await expect(ci).toHaveAttribute('data-state', 'unmet');
	await expect(ci.getByRole('img', { name: 'Not met' })).toBeVisible();
	await expect(ci).toContainText('required');
	await expect(ci).toContainText('since');
	await expect(ci).toContainText('1 check(s) failed on abc1234: lint.');
	await expect(check(page, 'migrations')).toHaveAttribute('data-state', 'met');
	await expect(check(page, 'migrations')).toContainText('advisory');
	await expect(check(page, 'reviewer')).toHaveAttribute('data-state', 'unmet');
	await expect(check(page, 'reviewer')).toContainText('reviewer: Changes requested on this head.');
	await expect(check(page, 'owner')).toHaveAttribute('data-state', 'unknown');
	// Only a required check that blocks is waived; nothing waits for an approval yet.
	await expect(check(page, 'pr').getByRole('button')).toHaveCount(0);
	await expect(check(page, 'migrations').getByRole('button')).toHaveCount(0);
	await expect(checks(page).getByRole('button', { name: 'Approve' })).toHaveCount(0);
	await shots(page, testInfo, 'checks-desktop');

	// A waiver is asked once, then posted to the room through the owner's session.
	await ci.getByRole('button', { name: 'Waive ci' }).click();
	const dialog = page.getByRole('alertdialog', { name: 'Waive ci?' });
	await expect(dialog).toContainText('Integrate');
	await expect(dialog).toContainText('abc1234');
	await dialog.getByRole('button', { name: 'Waive', exact: true }).click();
	await expect.poll(() => state.posts).toHaveLength(1);
	expect(state.posts[0]).toMatchObject({ kind: 'waiver', data: { check: 'ci' } });
	expect(state.posts[0].request_id).toMatch(/^[0-9a-f-]{36}$/);
	await expect(ci).toHaveAttribute('data-state', 'waived');
	await expect(ci).toContainText('You waived it for abc1234.');
	await expect(ci.getByRole('button')).toHaveCount(0);

	// The waiver is a decision of the thread.
	await filters(page).getByRole('button', { name: 'Decisions only' }).click();
	await expect(thread(page).getByRole('list', { name: 'Task room' }).locator(':scope > li').last()).toContainText('Waived the check ci');
});

test('the owner approves where the pipeline waits for it, and a finished task offers nothing', async ({ page }) => {
	const waiting = [...room(), post(12, 'kuayle', 'phase', '', { data: { phase: 'approval' } }), fact(13, 'approved', false, 'Nobody has approved merging abc1234 yet.')];
	const { state } = await fixture(page, {
		task: { workflow_node: 'approval' },
		workflow: pipelineView({ node: 'approval' }),
		room: waiting
	});
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto(agentView);
	await openChecks(page);
	const owner = check(page, 'owner');
	await expect(owner).toHaveAttribute('data-state', 'unmet');
	await expect(owner).toContainText('Nobody has approved merging abc1234 yet.');
	await owner.getByRole('button', { name: 'Approve' }).click();
	await expect.poll(() => state.decisions).toEqual([{ sequence: 9, decision: 'approve' }]);

	state.task = baseTask({ state: 'completed', workflow_node: 'merge', workflow_status: 'done' });
	state.workflow = pipelineView({ node: 'merge', status: 'done', can_decide: false });
	await page.reload();
	// The tab chosen is kept across the reload.
	await expect(checks(page)).toBeVisible();
	await expect(check(page, 'owner')).toHaveAttribute('data-state', 'met');
	await expect(checks(page).getByRole('button', { name: /^(Approve|Waive)/ })).toHaveCount(0);
});

test('at phone width the thread and its Checks tab fit without scrolling sideways', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(agentView);
	await expect(thread(page).getByRole('list', { name: 'Task room' }).locator(':scope > li')).toHaveCount(7);
	await expect(filters(page)).toBeVisible();
	const fits = () => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
	expect(await fits()).toBe(true);
	await shots(page, testInfo, 'thread-phone');
	await openChecks(page);
	await expect(check(page, 'ci')).toHaveAttribute('data-state', 'unmet');
	expect(await fits()).toBe(true);
	await shots(page, testInfo, 'checks-phone');
});
