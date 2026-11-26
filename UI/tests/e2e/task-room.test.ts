import { audit, verifyAudit } from './room-post-audit';
import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';
import { openPanel } from './side-panel';

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
	post(6, 'kuayle', 'fact', '', { data: { fact: 'ci_green', value: { state: 'passing', head: 'abc1234def5678', failing: [] } } }),
	member(7, 'reviewer', 'run-rev-2', 'verdict', 'Looks good to me.', {
		data: { verdict: 'approve', commit: 'abc1234def5678', role: 'reviewer' }
	})
];

async function fixture(page: Page, workflow: Record<string, unknown> = view) {
	const currentTask = {
		...task,
		state: workflow.task_state,
		reason: workflow.task_reason,
		workflow_status: workflow.status,
		workflow_node: workflow.node,
		pending_decisions: [] as any[]
	};
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
		'run-rev-1': [
			{ type: 'turn.failed', error: { message: 'Temporary connection loss' } },
			{ type: 'item.completed', item: {
				type: 'command_execution', command: 'rg SearchBox', status: 'failed', exit_code: 1
			} }
		].map((value, i) => ({
			id: i + 1, agent_run_id: 'run-rev-1', stream: 'stdout', sequence: i + 1, truncated: false,
			content: JSON.stringify(value) + '\n', created_at: at(4)
		})),
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
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [currentTask] } });
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
				const message = post(state.room.length + 1, 'human', input.kind, input.body, {
					reply_to: input.reply_to ?? null,
					data: input.data ?? {}
				});
				if (input.data?.answer)
					currentTask.pending_decisions = currentTask.pending_decisions.filter((d) => d.seq !== input.reply_to);
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
			if (id === task.id && rest === '') return route.fulfill({ json: currentTask });
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ json: [] });
	});
	const emit = (type: string, payload: any) => {
		const event = { id: crypto.randomUUID(), conversation_id: 'chat-1', cursor: state.events.length + 1, type, payload };
		state.events.push(event);
		state.sockets.at(-1)!.send(JSON.stringify(event));
	};
	return { state, emit, currentTask };
}

test('a room post just over the preview height stays fully readable', async ({ page }) => {
	const { state } = await fixture(page);
	state.room = [member(1, 'reviewer', 'run-rev-2', 'say', 'Review complete.')];
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const post = page.getByTestId('task-room').locator('[data-seq="1"]');
	const paragraph = post.getByText('Review complete.', { exact: true });
	const more = post.getByRole('button', { name: 'Show more' });

	// Exercise both sides of the measured-height boundary independently of font metrics.
	await paragraph.evaluate((el) => {
		el.style.marginBlock = '0';
		el.style.height = '305px';
	});
	await expect(more).toBeVisible();
	await paragraph.evaluate((el) => { el.style.height = '304px'; });
	await expect(more).toHaveCount(0);
	const bounds = (await paragraph.boundingBox())!;
	const bubble = (await post.locator('[data-slot="bubble-content"]').boundingBox())!;
	expect(bounds.y + bounds.height).toBeLessThanOrEqual(bubble.y + bubble.height);
});

test('the room post body renders semantic Markdown and neutralizes untrusted markup', async ({ page }) => {
	const { state } = await fixture(page);
	const source = [
		audit,
		'<script>void 0</script>',
		'[unsafe](javascript:void(0)) [mixed](JaVaScRiPt:void(0)) [data](data:text/html,<b>x</b>)'
	].join('\n\n');
	state.room = [member(1, 'reviewer', 'run-rev-2', 'say', source)];
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const body = page.getByTestId('task-room').locator('[data-seq="1"]');
	await expect(body.locator('table')).toHaveCount(1);
	await body.getByRole('button', { name: 'Show more' }).click();
	await expect(body.locator('ul li')).toHaveCount(2);
	await expect(body.locator('pre code')).toContainText('npm run test:e2e');
	await expect(body.locator('strong')).toHaveText('focus');
	await expect(body.getByRole('link', { name: 'audit notes' })).toHaveAttribute('href', 'https://example.com/audit');
	await expect(body).toContainText('<script>void 0</script>');
	await expect(body.locator('script, a[href^="javascript:" i], a[href^="data:" i]')).toHaveCount(0);

	// Keyboard focus on a link below the preview reveals its destination.
	await body.getByRole('button', { name: 'Show less' }).click();
	await body.getByRole('link', { name: 'audit notes' }).focus();
	await expect(body.getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true');
});

for (const width of [1600, 390]) {
	test(`a room audit stays inside the agent post at ${width}px`, async ({ page }) => {
		const { state } = await fixture(page);
		state.room = [member(1, 'implementer', 'run-impl-1', 'ask', audit)];
		await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
		await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
		const turn = page.getByTestId('task-room').getByRole('article', { name: 'implementer' });
		await turn.getByRole('button', { name: /implementer/ }).click();
		await verifyAudit(page, turn.locator('[data-seq="1"]'), 'room');
	});
}

test('a pipeline task shows its room: the owner, Kuayle and each role, and the owner writes to it', async ({ page }, testInfo) => {
	const { state, emit } = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
	const room = thread.getByRole('list', { name: 'Task room' });
	const rows = room.locator(':scope > li');
	await expect(rows).toHaveCount(6);
	// The room stays open beside the Agents panel.
	await expect(thread.getByRole('list', { name: 'Path taken' })).toHaveCount(0);
	await expect(page.getByTestId('task-workflow')).toContainText('Team');
	await expect(page.getByTestId('task-workflow-chart')).toHaveCount(0);

	// The owner's post, then Kuayle's phase move, named by the pipeline.
	await expect(rows.nth(0).getByRole('article', { name: 'You' })).toContainText('Search should cover titles and identifiers.');
	await expect(rows.nth(1).getByRole('article', { name: 'Kuayle' })).toContainText('Moved to Implement');

	// The implementer's turn is one line (KUA-286): its role, what it said
	// last, how long it took and how it went, with its question flagged.
	// The model, what it posted and its transcript are inside.
	const implementer = rows.nth(2).getByRole('article', { name: 'implementer' });
	const line = implementer.getByRole('button').first();
	await expect(implementer.getByRole('button')).toHaveCount(1);
	await expect(line).toHaveAttribute('aria-expanded', 'false');
	await expect(line).toContainText('implementer — Should search ignore case?');
	await expect(line).toContainText('2m 0s');
	await expect(line.getByRole('img', { name: 'Question' })).toBeVisible();
	await expect(line.getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(implementer.getByText('claude-opus-5-5')).toHaveCount(0);
	await expect(implementer.getByText('Starting with the endpoint.')).toHaveCount(0);
	await line.press('Enter');
	await expect(line).toHaveAttribute('aria-expanded', 'true');
	await expect(implementer.getByTestId('turn-details')).toContainText('Claude Code');
	await expect(implementer.getByTestId('turn-details')).toContainText('claude-opus-5-5');
	await expect(implementer).toContainText('To reviewer');
	await expect(implementer.getByText('Starting with the endpoint.')).toBeVisible();
	// Its refused post is muted, with the reason.
	const refused = implementer.locator('[data-refused]');
	await expect(refused).toContainText('Approving my own work.');
	await expect(refused).toContainText('Kuayle refused this message: the implementer may not post verdict');
	await expect(refused.locator('[data-slot="bubble-content"]')).toHaveClass(/text-muted-foreground/);

	// The reviewer answers in a turn of its own; Kuayle's fact is a line; the
	// reviewer's next turn gives its verdict on a commit.
	await expect(rows.nth(3).getByRole('article', { name: 'reviewer' })).toContainText('Yes, ignore case.');
	await expect(rows.nth(4).getByRole('article', { name: 'Kuayle' })).toContainText('CI passed');
	const verdict = rows.nth(5).getByRole('article', { name: 'reviewer' });
	const verdictLine = verdict.getByRole('button').first();
	await expect(verdictLine).toContainText('Approves');
	await expect(verdictLine).toContainText('Looks good to me.');
	await expect(verdictLine.getByRole('img', { name: 'Running' })).toBeVisible();
	// Its run has not ended, so the turn starts open (KUA-321).
	await expect(verdictLine).toHaveAttribute('aria-expanded', 'true');
	await expect(verdict).toContainText('Commit abc1234');
	await page.screenshot({ path: testInfo.outputPath('task-room-desktop.png'), fullPage: true });

	// A completed reviewer's turn uses the same neutral shell-exit presentation.
	const reviewer = rows.nth(3).getByRole('article', { name: 'reviewer' });
	await reviewer.getByRole('button').first().press('Enter');
	await expect(reviewer.getByRole('button', { name: 'rg SearchBox exit 1' })).toBeVisible();
	await expect(reviewer.locator('[data-status="error"]')).toHaveCount(0);

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
	const after = post(11, 'kuayle', 'fact', '1 of 3 check(s) are still running.', {
		data: { fact: 'ci_green', value: { state: 'pending', head: 'abc1234def5678', failing: [] } }
	});
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

// KUA-321: a turn that is running but has not posted yet has a row, open on
// its live stream, and the row folds when the turn ends.
test('a running turn that has not posted shows a working row with its live output, which folds when the turn ends', async ({ page }) => {
	const started = new Date(Date.now() - 5 * 60_000).toISOString();
	const workflow = {
		...view,
		runs: [{ ...run('run-impl-1', 'task-1', 'claude-code', 'claude-opus-5-5', 'running', 1), role: 'implementer', started_at: started }]
	};
	const { state, emit } = await fixture(page, workflow);
	state.room = [];
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const room = page.getByRole('list', { name: 'Task room' });
	await expect(room.locator(':scope > li')).toHaveCount(1);
	const turn = room.getByRole('article', { name: 'implementer' });
	const line = turn.getByRole('button').first();
	await expect(line).toContainText('implementer');
	await expect(line).toContainText('Working');
	await expect(line).toContainText('5m');
	await expect(line.getByRole('img', { name: 'Running' })).toBeVisible();
	// It starts open on the run's live output, though it posted nothing.
	await expect(line).toHaveAttribute('aria-expanded', 'true');
	await expect(turn.getByText('Starting with the endpoint.')).toBeVisible();
	await expect(page.getByTestId('task-workflow')).toContainText('Working');
	await expect(room.getByText('Nothing here yet')).toHaveCount(0);

	// Its first post joins the same row.
	const first = member(1, 'implementer', 'run-impl-1', 'say', 'Starting on the endpoint.');
	state.room.push(first);
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	emit('task.room_message', first);
	await expect(room.locator(':scope > li')).toHaveCount(1);
	await expect(turn.getByText('Starting on the endpoint.').first()).toBeVisible();
	await expect(line).toHaveAttribute('aria-expanded', 'true');

	// When the turn ends the row folds, like the others.
	workflow.runs = [{ ...workflow.runs[0], status: 'succeeded', completed_at: new Date().toISOString() }];
	emit('task.workflow_updated', { task_id: 'task-1', sequence: 2, node: 'implement', status: 'running' });
	await expect(line).toHaveAttribute('aria-expanded', 'false');
	await expect(line.getByRole('img', { name: 'Done' })).toBeVisible();
	await expect(turn.getByText('Starting with the endpoint.')).toHaveCount(0);
	await expect(page.getByTestId('run-member-implementer')).toContainText('Idle');
	await expect(room.locator(':scope > li')).toHaveCount(1);
});

test('the room shows who may approve a merge and which commits a waiver covers', async ({ page }, testInfo) => {
	const { state } = await fixture(page);
	const commit = 'abc1234def567890123456789012345678901234';
	state.room = [
		post(1, 'human', 'grant', '', { data: { action: 'approve', role: 'lead' } }),
		post(2, 'human', 'waiver', 'The retired check has no runner.', { data: { check: 'legacy-ci', standing: true } }),
		post(3, 'human', 'waiver', 'Verified this build locally.', { data: { check: 'ci', commit } }),
		post(4, 'member', 'request', '', { author_role: 'lead', data: { action: 'approve', commit } }),
		post(5, 'human', 'grant', '', { data: { action: 'split', role: 'lead' } })
	];
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const room = page.getByRole('list', { name: 'Task room' });
	await expect(room.locator('[data-seq="1"]')).toContainText('Lets the lead approve merges');
	await expect(room.locator('[data-seq="2"]')).toContainText('Waived the check legacy-ci for all commits');
	await expect(room.locator('[data-seq="3"]')).toContainText('Waived the check ci');
	await expect(room.locator('[data-seq="3"]')).toContainText('Commit abc1234');
	// The lead's post is one line of its turn (KUA-286); it opens to the post.
	await room.getByRole('article', { name: 'lead' }).getByRole('button').first().click();
	await expect(room.locator('[data-seq="4"]')).toContainText('Approved merging abc1234');
	await expect(room.locator('[data-seq="5"]')).toContainText('Lets the lead create sub-issues without asking');

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
	const agents = page.getByRole('region', { name: 'Agents', exact: true });
	await expect(agents.getByRole('button', { name: /Team v1 · Waiting · Approval/ })).toBeVisible();
	await expect(agents.getByTestId('task-workflow-chart')).toHaveCount(0);
	const decision = agents.getByTestId('task-workflow-decision');
	await expect(decision).toContainText('Waiting for an approval: Approval');
	await expect(decision.getByRole('button', { name: 'Deny' })).toHaveCount(0);
	await decision.getByRole('button', { name: 'Approve' }).click();
	await expect.poll(() => state.decisions).toEqual([{ sequence: 9, decision: 'approve' }]);
});

for (const [status, paused_at] of [['Paused', at(3)], ['Blocked', null]] as const) {
	test(`the Agents status line shows a ${status.toLowerCase()} pipeline`, async ({ page }) => {
		await fixture(page, { ...view, task_state: 'blocked', paused_at, node: 'implement' });
		await page.setViewportSize({ width: 1280, height: 900 });
		await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
		const agents = page.getByRole('region', { name: 'Agents', exact: true });
		await expect(agents.getByRole('button', { name: new RegExp(`Team v1 · ${status} · Implement`) })).toBeVisible();
		await expect(agents.getByTestId('task-workflow-chart')).toHaveCount(0);
	});
}

test('the roster and pipeline status stay available when room history fails to load', async ({ page }) => {
	const name = 'Release verification for the full workspace before publishing';
	await fixture(page, { ...view, name, node: 'implement', runs: [] });
	let unavailable = true;
	await page.route('**/agent-tasks/task-1/room?*', (route) =>
		unavailable ? route.fulfill({ status: 503, json: {} }) : route.fallback()
	);
	await page.setViewportSize({ width: 320, height: 720 });
	await page.goto('/test/assistant?conversation=chat-1');
	await openPanel(page, 'Agents');
	const agents = page.getByRole('region', { name: 'Agents', exact: true });
	const summary = agents.getByRole('button', { name: new RegExp(name) });
	await expect(summary).toContainText('Running');
	await expect(agents.getByTestId('run-member-reviewer')).toContainText('Idle');
	expect(await agents.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
	const alert = agents.getByRole('alert');
	await expect(alert).toBeVisible();
	unavailable = false;
	await alert.getByRole('button', { name: 'Refresh' }).click();
	await expect(alert).toHaveCount(0);
	await expect(agents.getByTestId('run-member-reviewer')).toContainText('Looks good to me.');
});

test('the owner can answer an ask with a choice and find other waiting tasks from the sidebar', async ({ page }) => {
	const { state, currentTask } = await fixture(page);
	const body = 'Which search should ship?\nThe identifier index is ready.\nA) Titles\nB) Identifiers\nC) Both';
	state.room = [member(1, 'lead', '', 'ask', body, { to_roles: ['human'] })];
	currentTask.pending_decisions = [{ kind: 'ask', seq: 1, asker: 'lead', body, data: {} }];
	await page.route('**/agent-tasks?needs_me=true', (route) =>
		route.fulfill({
			json: [
				{
					id: 'task-2',
					conversation_id: 'chat-2',
					pending_decision_count: 2,
					state: 'blocked',
					issue_identifier: 'ENG-2',
					issue_title: 'Release notes',
					updated_at: at(1),
					provider: 'codex'
				}
			]
		})
	);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const card = page.getByTestId('owner-decision');
	await expect(card).toContainText('The identifier index is ready.');
	await page.route(
		'**/agent-tasks/task-1/room',
		(route) => route.fulfill({ status: 409, json: { error: { code: 'ROOM_REFUSED' } } }),
		{ times: 1 }
	);
	await card.getByRole('button', { name: 'B) Identifiers' }).click();
	await expect(card.getByRole('button', { name: 'B) Identifiers' })).toBeEnabled();
	await card.getByRole('button', { name: 'B) Identifiers' }).click();
	await expect(card).toHaveCount(0);
	expect(state.posts).toMatchObject([{ reply_to: 1, to: ['lead'], data: { answer: 'reply' }, body: 'B) Identifiers' }]);
	await expect(page.getByTestId('rail-decision-count')).toHaveText('2');
	await page
		.getByTestId('assistant-sidebar-rail')
		.getByRole('button', { name: /Show conversations/i })
		.click();
	const sidebar = page.getByTestId('assistant-sidebar');
	await sidebar.getByRole('button', { name: /Needs me/ }).click();
	await expect(sidebar.getByRole('link', { name: /ENG-2.*Release notes/ })).toHaveAttribute(
		'href',
		'/test/assistant?conversation=chat-2&agent=task-2'
	);
});

test('the creator reads the proposed sub-issues and approves the exact split from the room', async ({
	page
}, testInfo) => {
	const { state, currentTask } = await fixture(page, {
		...view,
		task_state: 'blocked',
		status: 'paused',
		can_decide: true,
		task_reason:
			'Workflow · Nobody made progress on this head in 3 rounds; Review waits on search. Continue or stop the workflow'
	});
	const issues = [
		{ title: 'Search issue titles', description: 'Match words in the issue title.' },
		{ title: 'Search identifiers', description: 'Open an issue by its exact identifier.' }
	];
	state.room = [
		member(1, 'lead', '', 'request', 'Implement the two kinds of search separately.', {
			data: { action: 'split', issues }
		})
	];
	currentTask.pending_decisions = [
		{ kind: 'request', blocks_pause: true, seq: 1, asker: 'lead', body: state.room[0].body, data: state.room[0].data }
	];
	const approvals: string[] = [];
	await page.route('**/agent-tasks/task-1/room/1/approve-split', (route) => {
		approvals.push(route.request().method());
		currentTask.pending_decisions = [];
		state.room.push(
			post(2, 'kuayle', 'fact', 'Created the sub-issues.', { reply_to: 1, data: { fact: 'split_approved' } })
		);
		return route.fulfill({ status: 204 });
	});
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	const notice = page.getByTestId('agent-thread-header').getByRole('link', { name: /Waiting on your decision/ });
	await expect(notice).toBeVisible();
	await notice.click();
	const badge = page.getByTestId('thread-decision-count');
	await expect(badge).toContainText('1');
	const railBadge = page.getByTestId('rail-decision-count');
	await expect(railBadge).toHaveText('1');
	const proposal = page.getByTestId('owner-decision');
	await expect(proposal).toBeVisible({ timeout: 20_000 });
	const approve = proposal.getByRole('button', { name: 'Approve', exact: true });
	for (const issue of issues) {
		await expect(proposal).toContainText(issue.title);
		await expect(proposal).toContainText(issue.description);
	}
	await expect(approve).toBeVisible();
	for (const [device, width, height] of [['desktop', 1280, 900], ['phone', 375, 780]] as const) {
		await page.setViewportSize({ width, height });
		for (const theme of ['light', 'dark']) {
			await page.evaluate((theme) => {
				document.documentElement.classList.toggle('dark', theme === 'dark');
				document.documentElement.setAttribute('data-theme', theme);
			}, theme);
			await approve.scrollIntoViewIfNeeded();
			expect(await proposal.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
			await page.screenshot({ path: testInfo.outputPath(`split-${device}-${theme}.png`) });
		}
	}
	expect(approvals).toEqual([]);
	await approve.click();
	await expect(page.getByText('Created the sub-issues.')).toBeVisible();
	await expect(approve).toHaveCount(0);
	await expect(badge).toHaveCount(0);
	await expect(notice).toHaveCount(0);
	await page.setViewportSize({ width: 1280, height: 900 });
	await expect(railBadge).toHaveCount(0);
	expect(approvals).toEqual(['POST']);
});

for (const viewport of [
	{ name: 'desktop', width: 1600, height: 900 },
	{ name: 'phone', width: 390, height: 844 }
]) {
	test(`Agents shows member activity and opens the full pipeline in Workflow on ${viewport.name}`, async ({ page }) => {
		const phases = [
			{ id: 'implement', label: 'Implement', owner: 'implementer', checks: [{ id: 'pull-request', fact: 'pr_exists', required: true }] },
			{ id: 'review', label: 'Review', owner: 'reviewer', checks: [{ id: 'ci', fact: 'ci_green', required: true }] },
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		];
		const { state, emit } = await fixture(page, {
			...view,
			name: 'Quality Pipeline',
			version: 2,
			iterations: 1,
			node: 'review',
			graph: { ...view.graph, phases, lead: { profile_id: 'p-lead' } },
			runs: [
				...view.runs.map((run) => run.task_id === task.id
					? { ...run, role: 'implementer', summary: 'Added search for titles and identifiers.' }
					: { ...run, role: 'reviewer' }),
				{ ...run('run-lead', 'member-lead', 'codex', 'gpt-5', 'waiting_input', 2), role: 'lead' }
			]
		});
		const head = 'ab34567fedc';
		const reason = 'The browser tests are still running.';
		state.room = [
			post(1, 'kuayle', 'fact', '', { data: { fact: 'pr_exists', value: true } }),
			post(2, 'kuayle', 'fact', '', { data: { fact: 'head_sha', value: head } }),
			member(3, 'lead', 'run-lead', 'phase', 'Review the search changes.', { data: { phase: 'review' } }),
			member(4, 'reviewer', 'run-rev-2', 'say', 'Checking search by issue identifier.'),
			post(5, 'kuayle', 'fact', reason, { data: { fact: 'ci_green', value: { state: 'pending', head } } })
		];

		await page.setViewportSize({ width: viewport.width, height: viewport.height });
		// The issue chat also loads the checks when the task room has never been opened.
		await page.goto(viewport.name === 'phone' ? '/test/assistant?conversation=chat-1' : '/test/assistant?conversation=chat-1&agent=task-1');
		const details = page.getByTestId('task-workflow');
		if (viewport.name === 'phone') {
			// At phone width the panel is a sheet that the toggle opens (KUA-331).
			await openPanel(page, 'Agents');
		}
		const agents = page.getByRole('region', { name: 'Agents', exact: true });
		const summary = agents.getByRole('button', { name: /Quality Pipeline v2 · Running · Review · Iteration 1/ });
		await expect(agents.getByTestId('run-member-reviewer')).toContainText('Working');
		await expect(agents.getByTestId('task-workflow-chart')).toHaveCount(0);
		await expect(agents.getByRole('toolbar', { name: 'Chart tools' })).toHaveCount(0);
		await expect(agents.getByRole('region', { name: 'Checks', exact: true })).toHaveCount(0);
		await expect(agents.getByTestId('run-member-implementer')).toContainText('Idle');
		await expect(agents.getByTestId('run-member-implementer')).toContainText('Added search for titles and identifiers.');
		await expect(agents.getByTestId('run-member-lead')).toContainText('Waiting');
		await expect(agents.getByTestId('run-member-reviewer')).toContainText('Checking search by issue identifier.');
		await expect(summary).toBeVisible();
		expect(await summary.locator('span').evaluateAll((parts) => parts.every((part) => part.scrollWidth <= part.clientWidth))).toBe(true);
		for (const theme of ['light', 'dark']) {
			await page.evaluate((theme) => {
				document.documentElement.classList.toggle('dark', theme === 'dark');
				document.documentElement.setAttribute('data-theme', theme);
			}, theme);
			expect(await agents.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
			if (process.env.KUAYLE_SCREENSHOT_DIR)
				await page.screenshot({ path: `${process.env.KUAYLE_SCREENSHOT_DIR}/agents-${viewport.name}-${theme}.png`, animations: 'disabled' });
		}
		if (viewport.name === 'desktop') await summary.press('Enter');
		else await summary.click();
		await expect(page.getByTestId('workflow-panel')).toBeVisible();
		const chart = details.getByTestId('task-workflow-chart');
		await expect(chart.getByTestId('workflow-node-review')).toHaveAttribute('data-current', 'true');
		await expect(chart.locator('[data-current="true"]')).toHaveCount(1);
		await expect(chart.locator('.svelte-flow__edge')).toHaveCount(phases.length - 1);
		let right = -Infinity;
		for (const phase of phases) {
			const card = chart.getByTestId(`workflow-node-${phase.id}`);
			await expect(card).toContainText(phase.label);
			const box = (await card.boundingBox())!;
			expect(box.x).toBeGreaterThan(right);
			right = box.x + box.width;
		}
		const ci = details.getByTestId('run-check-ci');
		await expect(ci).toContainText('Waiting');
		await expect(ci).toContainText(reason);
		await expect(ci.locator('time')).toHaveAttribute('datetime', at(5));
		await expect(details.getByTestId('run-check-pull-request')).toContainText('Met');
		await expect(details.getByTestId('run-member-reviewer')).toContainText('Working');
		const writer = details.getByTestId('run-member-implementer');
		await expect(writer).toContainText('implementer');
		await expect(writer).not.toContainText('Working');

		// Show the whole path in a widened desktop panel. On a phone keep the
		// readable, centred view of the current phase that opens by default.
		if (viewport.name === 'desktop') {
			const handle = (await page.getByLabel('Resize side panel').boundingBox())!;
			await page.mouse.move(handle.x + handle.width / 2, handle.y + 20);
			await page.mouse.down();
			await page.mouse.move(handle.x - 400, handle.y + 20, { steps: 8 });
			await page.mouse.up();
			await chart.getByRole('button', { name: 'Fit to view' }).click();
			await expect.poll(async () => {
				const area = (await chart.boundingBox())!;
				const first = (await chart.getByTestId('workflow-node-implement').boundingBox())!;
				const last = (await chart.getByTestId('workflow-node-merge').boundingBox())!;
				return first.x >= area.x && last.x + last.width <= area.x + area.width;
			}).toBe(true);
		}
		await page.mouse.move(0, 0);
		for (const theme of ['light', 'dark']) {
			await page.evaluate((theme) => {
				document.documentElement.classList.toggle('dark', theme === 'dark');
				document.documentElement.setAttribute('data-theme', theme);
			}, theme);
			expect(await details.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
			expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
			if (process.env.KUAYLE_SCREENSHOT_DIR)
				await page.screenshot({ path: `${process.env.KUAYLE_SCREENSHOT_DIR}/pipeline-run-${viewport.name}-${theme}.png`, animations: 'disabled' });
		}

		await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
		const passed = post(6, 'kuayle', 'fact', '', { data: { fact: 'ci_green', value: { state: 'passing', head } } });
		state.room.push(passed);
		emit('task.room_message', passed);
		await expect(ci).toContainText('Met');
		await expect(ci).not.toContainText(reason);
		await expect(ci.locator('time')).toHaveAttribute('datetime', at(6));
	});
}
