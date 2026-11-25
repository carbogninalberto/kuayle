import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

// The agent view of a task on a pipeline (KUA-268): its room in order, the
// owner writing to it from the composer and posts arriving live, served from
// mocked API routes.
const at = (minute: number) => `2026-11-24T10:${String(minute).padStart(2, '0')}:00Z`;
const text = (value: string) => ({ type: 'assistant', message: { content: [{ type: 'text', text: value }] } });

const task = {
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
	state: 'running',
	reason: '',
	generation: 1,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl-1',
	kind: 'implement',
	workflow_version_id: 'version-1',
	workflow_status: 'running',
	pipeline: true,
	created_at: at(0),
	updated_at: at(1)
};

const run = (id: string, taskId: string, provider: string, model: string, status: string, minute: number) => ({
	id,
	task_id: taskId,
	provider,
	model,
	status,
	created_at: at(minute),
	...(status === 'succeeded' ? { completed_at: at(minute + 2) } : {})
});

// The built-in team pipeline: phases with checks, an implementer and a reviewer.
const view = {
	workflow_id: 'workflow-team',
	name: 'Team',
	version: 1,
	builtin: false,
	graph: {
		kind: 'pipeline',
		phases: [
			{ id: 'implement', label: 'Implement', owner: 'implementer' },
			{ id: 'review', label: 'Review', owner: 'implementer', checks: [{ id: 'reviewer', fact: 'verdict', role: 'reviewer', required: true }] },
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		],
		roster: [
			{ role: 'implementer', profile_id: 'p-impl', writer: true },
			{ role: 'reviewer', profile_id: 'p-rev' }
		]
	},
	node: '',
	status: 'running',
	visits: {},
	iterations: 0,
	sequence: 0,
	steps: [],
	runs: [
		run('run-impl-1', 'task-1', 'claude-code', 'claude-opus-5-5', 'succeeded', 1),
		run('run-rev-1', 'member-rev', 'codex', 'gpt-5', 'succeeded', 4),
		run('run-rev-2', 'member-rev', 'codex', 'gpt-5', 'running', 7)
	],
	profiles: [],
	can_decide: false,
	task_state: 'running',
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
const member = (seq: number, role: string, run_id: string, kind: string, body: string, extra: Record<string, unknown> = {}) =>
	post(seq, 'member', kind, body, { author_role: role, author_member_id: `member-${role}`, run_id, ...extra });

const room = () => [
	post(1, 'human', 'say', 'Search should cover titles and identifiers.'),
	post(2, 'kuayle', 'phase', '', { data: { phase: 'implement' } }),
	member(3, 'implementer', 'run-impl-1', 'ask', 'Should search ignore case?', { to_roles: ['reviewer'] }),
	member(4, 'implementer', 'run-impl-1', 'verdict', 'Approving my own work.', {
		reject_reason: 'the implementer may not post verdict'
	}),
	member(5, 'reviewer', 'run-rev-1', 'say', 'Yes, ignore case.', { to_roles: ['implementer'], reply_to: 3 }),
	post(6, 'kuayle', 'fact', 'CI passed on abc1234.', { data: { fact: 'ci_green', value: true } }),
	member(7, 'reviewer', 'run-rev-2', 'verdict', 'Looks good to me.', {
		data: { verdict: 'approve', commit: 'abc1234def5678', role: 'reviewer' }
	})
];

async function fixture(page: Page, workflow: Record<string, unknown> = view) {
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
		room: room(),
		posts: [] as any[],
		decisions: [] as any[],
		// Room reads, as their ?after= values.
		reads: [] as number[],
		sockets: [] as WebSocketRoute[],
		events: [] as any[]
	};
	const logs: Record<string, any[]> = {
		'run-impl-1': [text('Starting with the endpoint.'), text('Search is added and covered.')].map((value, i) => ({
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
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [task] } });
		if (path.endsWith('/events'))
			return route.fulfill({ json: state.events.filter((e) => e.cursor > Number(url.searchParams.get('after') ?? 0)) });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false } });
		const match = path.match(/\/agent-tasks\/([^/]+)(\/.*)?$/);
		if (match) {
			const [, id, rest = ''] = match;
			if (rest === '/workflow') return route.fulfill({ json: workflow });
			if (rest === '/workflow/decision') {
				state.decisions.push(route.request().postDataJSON());
				return route.fulfill({ json: { task_id: id, state: 'awaiting_review', workflow_sequence: 10 } });
			}
			if (rest === '/room' && route.request().method() === 'POST') {
				const input = route.request().postDataJSON();
				state.posts.push(input);
				const message = post(state.room.length + 1, 'human', input.kind, input.body);
				state.room.push(message);
				return route.fulfill({ status: 201, json: message });
			}
			if (rest === '/room') {
				const after = Number(url.searchParams.get('after') ?? 0);
				state.reads.push(after);
				return route.fulfill({ json: { messages: state.room.filter((message) => message.seq > after) } });
			}
			const logRoute = rest.match(/^\/runs\/([^/]+)\/logs$/);
			if (logRoute) return route.fulfill({ json: { logs: logs[logRoute[1]] ?? [] } });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/notebook') return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (id === task.id && rest === '') return route.fulfill({ json: task });
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ json: [] });
	});
	const emit = (type: string, payload: any) => {
		const event = { id: crypto.randomUUID(), conversation_id: 'chat-1', cursor: state.events.length + 1, type, payload };
		state.events.push(event);
		state.sockets.at(-1)!.send(JSON.stringify(event));
	};
	return { state, emit };
}

test('a pipeline task shows its room: the owner, Kuayle and each role, and the owner writes to it', async ({ page }, testInfo) => {
	const { state, emit } = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const room = thread.getByRole('list', { name: 'Task room' });
	const rows = room.locator(':scope > li');
	await expect(rows).toHaveCount(6);
	// No timeline of workflow steps for a pipeline, and no graph to chart
	// beside it.
	await expect(thread.getByRole('list', { name: 'Path taken' })).toHaveCount(0);
	await expect(page.getByTestId('task-workflow')).toContainText('Team');
	await expect(page.getByTestId('task-workflow-chart')).toHaveCount(0);

	// The owner's post, then Kuayle's phase move, named by the pipeline.
	await expect(rows.nth(0).getByRole('article', { name: 'You' })).toContainText('Search should cover titles and identifiers.');
	await expect(rows.nth(1).getByRole('article', { name: 'Kuayle' })).toContainText('Moved to Implement');

	// The implementer's turn is one row: its question to the reviewer and
	// its refused post, muted with the reason.
	const implementer = rows.nth(2).getByRole('article', { name: 'implementer' });
	await expect(implementer).toContainText('Should search ignore case?');
	await expect(implementer).toContainText('To reviewer');
	await expect(implementer.getByRole('img', { name: 'Question' })).toBeVisible();
	await expect(implementer).toContainText('claude-opus-5-5');
	const refused = implementer.locator('[data-refused]');
	await expect(refused).toContainText('Approving my own work.');
	await expect(refused).toContainText('Kuayle refused this message: the implementer may not post verdict');
	await expect(refused.locator('[data-slot="bubble-content"]')).toHaveClass(/text-muted-foreground/);

	// The reviewer answers in a turn of its own; Kuayle posts a fact; the
	// reviewer's next turn gives its verdict on a commit.
	await expect(rows.nth(3).getByRole('article', { name: 'reviewer' })).toContainText('Yes, ignore case.');
	await expect(rows.nth(4).getByRole('article', { name: 'Kuayle' })).toContainText('CI passed on abc1234.');
	const verdict = rows.nth(5).getByRole('article', { name: 'reviewer' });
	await expect(verdict).toContainText('Approves');
	await expect(verdict).toContainText('Commit abc1234');
	await expect(verdict).toContainText('Looks good to me.');
	await expect(verdict.getByRole('img', { name: 'Running' })).toBeVisible();

	// A turn opens the run's transcript.
	const session = implementer.getByRole('button', { name: 'Agent session · implementer' });
	await expect(session).toHaveAttribute('aria-expanded', 'false');
	await expect(implementer.getByText('Starting with the endpoint.')).toHaveCount(0);
	await session.press('Enter');
	await expect(session).toHaveAttribute('aria-expanded', 'true');
	await expect(implementer.getByText('Starting with the endpoint.')).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath('task-room-desktop.png'), fullPage: true });

	// The composer writes to the room: a say, sent once.
	const composer = page.getByRole('textbox', { name: 'Write to the room' });
	await expect(composer).toHaveAttribute('placeholder', 'Write to everyone working on ENG-1…');
	// Text only, and no "now": the room is read by everyone working on it.
	await expect(page.getByTestId('agent-composer').getByRole('button', { name: 'Interrupt and send' })).toHaveCount(0);
	await expect(page.getByTestId('agent-composer').getByRole('button', { name: 'Attach images' })).toBeDisabled();
	await composer.fill('Please also update the docs.');
	await composer.press('Enter');
	await expect(rows).toHaveCount(7);
	await expect(rows.nth(6).getByRole('article', { name: 'You' })).toContainText('Please also update the docs.');
	await expect(page.getByTestId('agent-composer').getByRole('status')).toHaveText('Posted to the room of ENG-1');
	await expect(composer).toHaveValue('');
	expect(state.posts).toHaveLength(1);
	expect(state.posts[0]).toMatchObject({ kind: 'say', body: 'Please also update the docs.' });
	expect(state.posts[0].request_id).toMatch(/^[0-9a-f-]{36}$/);

	// Posts arrive live, once each: the event of the owner's own post adds
	// nothing, Kuayle's next phase move is added.
	const own = state.room.at(-1)!;
	expect(own.seq).toBe(8);
	emit('task.room_message', own);
	const next = post(9, 'kuayle', 'phase', '', { data: { phase: 'review' } });
	state.room.push(next);
	emit('task.room_message', next);
	await expect(rows).toHaveCount(8);
	await expect(rows.nth(7)).toContainText('Moved to Review');
	await expect(room.getByText('Please also update the docs.')).toHaveCount(1);

	// A post that arrives after a gap reads what was missed.
	const missed = member(10, 'implementer', 'run-impl-2', 'say', 'Docs updated.');
	const after = post(11, 'kuayle', 'fact', 'CI running.', { data: { fact: 'ci_green', value: null } });
	state.room.push(missed, after);
	emit('task.room_message', after);
	await expect(rows).toHaveCount(10);
	await expect(rows.nth(8)).toContainText('Docs updated.');
	expect(state.reads).toContain(9);

	// At phone width the room fits without scrolling sideways.
	await page.setViewportSize({ width: 320, height: 720 });
	await expect.poll(() => room.evaluate((list) => list.scrollWidth <= list.clientWidth)).toBe(true);
	await page.screenshot({ path: testInfo.outputPath('task-room-phone.png') });
});

test('the room shows who may approve a merge and which commits a waiver covers', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	const commit = 'abc1234def567890123456789012345678901234';
	state.room = [
		post(1, 'human', 'grant', '', { data: { action: 'approve', role: 'lead' } }),
		post(2, 'human', 'waiver', 'The retired check has no runner.', { data: { check: 'legacy-ci', standing: true } }),
		post(3, 'human', 'waiver', 'Verified this build locally.', { data: { check: 'ci', commit } }),
		post(4, 'member', 'request', '', { author_role: 'lead', data: { action: 'approve', commit } })
	];
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const room = page.getByRole('list', { name: 'Task room' });
	await expect(room.locator('[data-seq="1"]')).toContainText('Lets the lead approve merges');
	await expect(room.locator('[data-seq="2"]')).toContainText('Waived the check legacy-ci for all commits');
	await expect(room.locator('[data-seq="3"]')).toContainText('Waived the check ci');
	await expect(room.locator('[data-seq="3"]')).toContainText('Commit abc1234');
	await expect(room.locator('[data-seq="4"]')).toContainText('Approved merging abc1234');

	for (const [device, width] of [
		['desktop', 1280],
		['phone', 390]
	] as const) {
		await page.setViewportSize({ width, height: 900 });
		await expect.poll(() => room.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
		for (const mode of ['light', 'dark']) {
			await page.evaluate((mode) => {
				document.documentElement.classList.toggle('dark', mode === 'dark');
				document.documentElement.classList.toggle('light', mode === 'light');
			}, mode);
			await page.screenshot({ path: testInfo.outputPath(`grants-${device}-${mode}.png`) });
		}
	}
});

// The owner approves the head where the pipeline waits for it (its tick then
// merges through the gate); a pipeline has nothing to deny.
test('the owner approves a pipeline task waiting for approval', async ({ page }) => {
	const approval = {
		...view,
		graph: {
			...view.graph,
			phases: [
				...view.graph.phases.slice(0, 2),
				{ id: 'approval', label: 'Approval', owner: 'human', checks: [{ id: 'owner', fact: 'approved', required: true }] },
				view.graph.phases[2]
			]
		},
		node: 'approval',
		status: 'waiting',
		sequence: 9,
		can_decide: true,
		task_state: 'awaiting_review'
	};
	const { state } = await fixture(page, approval);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const decision = page.getByTestId('task-workflow-decision');
	await expect(decision).toContainText('Waiting for an approval: Approval');
	await expect(decision.getByRole('button', { name: 'Deny' })).toHaveCount(0);
	await decision.getByRole('button', { name: 'Approve' }).click();
	await expect.poll(() => state.decisions).toEqual([{ sequence: 9, decision: 'approve' }]);
});
