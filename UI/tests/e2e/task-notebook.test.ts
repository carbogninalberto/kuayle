import { test, expect, type Page, type WebSocketRoute } from '@playwright/test';

// The task notebook, the prompt a step received and the flag on a run that
// did not update its notebook (KUA-134), in the agent view of a task like
// KUA-122: an orchestrator plans, an implementer codes. Served from mocked
// API routes.
const time = '2026-11-10T10:00:00Z';
const at = (minute: number) => `2026-11-10T10:${String(minute).padStart(2, '0')}:00Z`;
const shots = process.env.KUA134_SHOTS;

const request = 'Add search to the issue list.';
const task = (over: any = {}) => ({
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	prompt: request,
	provider: 'opencode',
	model: 'kimi-k2',
	thinking: '',
	state: 'running',
	reason: '',
	generation: 2,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-impl',
	workflow_version_id: 'version-1',
	workflow_node: 'implement',
	workflow_status: 'running',
	created_at: time,
	updated_at: at(6),
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
const workflow = () => ({
	workflow_id: 'workflow-1',
	name: 'Cheap Workflow',
	version: 4,
	builtin: false,
	graph: {
		nodes: [
			node('start', 'start', 'Start'),
			node('plan', 'agent', 'Implementer', 'p-orch'),
			node('implement', 'agent', 'Implement', 'p-impl')
		],
		edges: [],
		budget: {}
	},
	node: 'implement',
	status: 'running',
	visits: { start: 1, plan: 1, implement: 1 },
	iterations: 2,
	sequence: 3,
	steps: [
		{ task_id: 'task-1', sequence: 1, node_id: 'start', node_type: 'start', visit: 1, reason: '', detail: '', entered_at: at(0), outcome: 'else', finished_at: at(0) },
		{ task_id: 'task-1', sequence: 2, node_id: 'plan', node_type: 'agent', visit: 1, reason: '', detail: '', entered_at: at(0), generation: 1, outcome: 'agent_finished', finished_at: at(4), plan: 'Endpoint, search box, docs.' },
		{ task_id: 'task-1', sequence: 3, node_id: 'implement', node_type: 'agent', visit: 1, reason: '', detail: '', entered_at: at(4), generation: 2 }
	],
	runs: [
		{ id: 'run-plan', task_id: 'task-1', step: 2, provider: 'claude-code', model: 'claude-opus-5-5', status: 'succeeded', created_at: at(0), started_at: at(0), completed_at: at(4) },
		{ id: 'run-impl', task_id: 'task-1', step: 3, provider: 'opencode', model: 'kimi-k2', status: 'running', created_at: at(4), started_at: at(4) }
	],
	profiles: [
		profile('p-orch', 'Orchestrator', 'claude-code', 'claude-opus-5-5', 'plans'),
		profile('p-impl', 'Cheap Implementer', 'opencode', 'kimi-k2', 'changes_code')
	],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 2
});

const notebook = (over: any = {}) => ({
	task_id: 'task-1',
	revision: 5,
	updated_at: at(6),
	notebook: {
		plan: {
			text: 'Endpoint, search box, docs.',
			steps: [
				{ title: 'Add the search endpoint', files: ['api/search.go'], commands: ['go test ./api/...'], acceptance: 'The endpoint answers with matching issues', done: true },
				{ title: 'Add the search box to the list' },
				{ title: 'Write the docs' }
			]
		},
		decisions: [{ decision: 'Search by title only', why: 'The description index is not ready' }],
		done: ['The endpoint is added and covered by tests'],
		remaining: ['The search box', 'The docs'],
		verify: ['go test ./api/...', 'npm run test:e2e -- search'],
		findings: [
			{ id: 1, source: 'review', text: 'Rename q to query', resolved: true, at: at(3) },
			{ id: 2, source: 'ci', text: 'lint: unused import in list.ts', at: at(5) }
		],
		instructions: [
			{ source: 'request', author_id: 'user-1', text: request, at: at(0) },
			{ source: 'live_message', author_id: 'user-1', text: 'Keep the old route working.', at: at(5) }
		],
		events: [{ text: 'Paused by the provider\'s usage limit; the attempt did not fail and the worktree is unchanged', at: at(2) }],
		next_finding: 3
	},
	history: [
		{ revision: 5, author: 'agent', run_id: 'run-impl', generation: 2, step_sequence: 3, step_label: 'Implement', sections: ['done', 'plan', 'remaining'], summary: 'update from notebook.json', created_at: at(6) },
		{ revision: 4, author: 'person', user_id: 'user-1', run_id: 'run-impl', generation: 2, step_sequence: 3, step_label: 'Implement', sections: ['instructions'], summary: 'instruction (live_message)', created_at: at(5) },
		{ revision: 2, author: 'agent', run_id: 'run-plan', generation: 1, step_sequence: 2, step_label: 'Implementer', sections: ['plan'], summary: "the planning step's plan", created_at: at(4) }
	],
	...over
});

// What the planning step of KUA-122 was given: its step tells it to
// implement, its profile not to.
const planPrompt = () => ({
	run_id: 'run-plan',
	recorded: true,
	worktree_read_only: true,
	sources: [
		{ source: 'workflow_step', channel: 'prompt', text: 'List the changes needed, then implement them. Run the relevant tests.' },
		{ source: 'profile', channel: 'instructions_file', text: 'You are the orchestrator. Do not implement the steps yourself.' },
		{ source: 'contract', channel: 'prompt', text: 'You write a plan; the worktree is read-only.' },
		{ source: 'task', channel: 'prompt', text: Array.from({ length: 30 }, (_, i) => `Request line ${i + 1}`).join('\n') },
		{ source: 'plan', channel: 'prompt', text: '' },
		{ source: 'notebook', channel: 'instructions_file', text: '# Task notebook\n\nNothing yet.' },
		{ source: 'memory', channel: 'instructions_file', text: 'Lessons for this repository (approved by the workspace\'s admins):\n- Run make check before pushing' }
	]
});

async function fixture(page: Page) {
	const conversation = { id: 'chat-1', workspace_id: 'workspace-1', creator_id: 'user-1', issue_id: 'issue-1', team_id: 'team-1', title: 'Search', created_at: time, updated_at: time };
	const issue = { id: 'issue-1', identifier: 'ENG-1', title: 'Search', status: 'todo', description: '' };
	const state = {
		task: task() as any,
		workflow: workflow() as any,
		// null: the reader is not the task's creator and gets "not found".
		notebook: notebook() as any,
		// The running attempt started before prompts were recorded.
		prompts: {
			'run-plan': planPrompt(),
			'run-impl': { run_id: 'run-impl', recorded: false, worktree_read_only: false, sources: null }
		} as Record<string, any>,
		requests: [] as string[],
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
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', display_name: 'Olga Owner', email: 'owner@example.test' } });
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
		const match = path.match(/\/agent-tasks\/task-1(\/.*)?$/);
		if (match) {
			const rest = match[1] ?? '';
			state.requests.push(rest);
			const missing = () => route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (rest === '/workflow') return route.fulfill({ json: state.workflow });
			if (rest === '/notebook') return state.notebook ? route.fulfill({ json: state.notebook }) : missing();
			const prompt = rest.match(/^\/runs\/([^/]+)\/prompt$/);
			if (prompt) return state.prompts[prompt[1]] ? route.fulfill({ json: state.prompts[prompt[1]] }) : missing();
			if (/^\/runs\/[^/]+\/logs$/.test(rest)) return route.fulfill({ json: { logs: [] } });
			if (rest === '/runs') return route.fulfill({ json: { runs: [] } });
			if (rest.endsWith('/validation')) return route.fulfill({ json: null });
			if (rest.endsWith('/checks')) return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/history') return route.fulfill({ json: { transitions: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/publication') return route.fulfill({ json: { step: null } });
			if (rest === '/signals') return route.fulfill({ json: [] });
			return route.fulfill({ json: state.task });
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

const sizes = [
	{ name: 'desktop', width: 1280, height: 900 },
	{ name: 'phone', width: 390, height: 844 }
];
const fits = (page: Page) =>
	page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

for (const size of sizes) {
	test.describe(size.name, () => {
		test.beforeEach(async ({ page }) => {
			await page.setViewportSize({ width: size.width, height: size.height });
		});

		test('the notebook is one line above the timeline that opens to a working document', async ({ page }) => {
			await fixture(page);
			await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
			const thread = page.getByRole('region', { name: 'Agent thread of ENG-1' });
			const book = thread.getByRole('region', { name: 'Notebook', exact: true });
			const toggle = book.getByRole('button', { name: /^Notebook/ });
			// Closed, it is a line with the plan's progress; the timeline stays the view.
			await expect(toggle).toHaveAttribute('aria-expanded', 'false');
			await expect(toggle).toContainText('1 of 3');
			await expect(thread.getByRole('list', { name: 'Path taken' })).toBeVisible();
			await expect(book.getByRole('heading')).toHaveCount(0);

			await toggle.click();
			// The plan first, as a checklist that shows what is done; opening it
			// does not send the reader to the end of the transcript.
			await expect(book.getByRole('heading').first()).toHaveText('Plan');
			await expect(book.getByRole('region', { name: 'Remaining' })).toBeVisible();
			await expect(toggle).toBeInViewport();
			if (shots && size.name === 'desktop') {
				await page.setViewportSize({ width: 1280, height: 1100 });
				await toggle.scrollIntoViewIfNeeded();
				await page.screenshot({ path: `${shots}/1-notebook-desktop.png` });
				await page.setViewportSize({ width: size.width, height: size.height });
			}
			const plan = book.getByRole('region', { name: 'Plan' });
			const steps = plan.getByRole('listitem');
			await expect(steps).toHaveCount(3);
			await expect(steps.nth(0).getByRole('img', { name: 'Done' })).toBeVisible();
			await expect(steps.nth(1).getByRole('img', { name: 'To do' })).toBeVisible();
			// A step's files, commands and acceptance check are one click away.
			await expect(plan.getByText('go test ./api/...')).toBeHidden();
			await steps.nth(0).getByRole('button', { name: 'Add the search endpoint' }).click();
			await expect(plan.getByText('The endpoint answers with matching issues')).toBeVisible();
			await expect(plan.getByText('api/search.go')).toBeVisible();

			// Then what is done and remains, how to verify, decisions, findings.
			await expect(book.getByRole('heading')).toHaveText([
				'Plan',
				'Done',
				'Remaining',
				'How to verify',
				'Decisions',
				'Findings',
				'From people'
			]);
			await expect(book.getByRole('region', { name: 'Remaining' }).getByRole('listitem')).toHaveText([
				'The search box',
				'The docs'
			]);
			await expect(book.getByRole('region', { name: 'Decisions' })).toContainText('The description index is not ready');
			// Open findings come first; a resolved one says so.
			const findings = book.getByRole('region', { name: 'Findings' }).getByRole('listitem');
			await expect(findings.nth(0)).toContainText('CI');
			await expect(findings.nth(0)).toContainText('lint: unused import');
			await expect(findings.nth(1).getByRole('img', { name: 'Resolved' })).toBeVisible();

			// What a person asked is apart from what agents wrote; the request
			// the header already shows is not repeated.
			const people = book.getByRole('region', { name: 'From people' });
			await expect(people.getByRole('listitem')).toHaveCount(1);
			await expect(people).toContainText('Keep the old route working.');
			await expect(people).toContainText('Olga Owner');
			await expect(plan).not.toContainText('Keep the old route working.');
			await expect(people).not.toContainText(request);

			// Who wrote what, at which step, is asked for, not on every line.
			await expect(book.getByRole('list', { name: 'History' })).toBeHidden();
			await book.getByRole('button', { name: 'History' }).click();
			const history = book.getByRole('list', { name: 'History' }).getByRole('listitem');
			await expect(history).toHaveCount(4);
			await expect(history.nth(0)).toContainText('Agent · Implement — Plan, Done, Remaining');
			await expect(history.nth(1)).toContainText('Olga Owner · Implement — From people');
			await expect(history.nth(3)).toContainText('Paused by the provider');
			expect(await fits(page)).toBe(true);
		});

		test('the notebook follows the steps as they write to it', async ({ page }) => {
			const { state, emit } = await fixture(page);
			await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
			const book = page.getByRole('region', { name: 'Notebook', exact: true });
			const toggle = book.getByRole('button', { name: /^Notebook/ });
			await expect(toggle).toContainText('1 of 3');
			await toggle.click();
			await expect(book.getByRole('region', { name: 'Remaining' }).getByRole('listitem')).toHaveCount(2);

			// The implementer records its second step; the task's event reloads it.
			const next = notebook({ revision: 6 });
			next.notebook.plan.steps[1].done = true;
			next.notebook.remaining = ['The docs'];
			next.notebook.done.push('The search box filters the list');
			state.notebook = next;
			emit('task.workflow_updated', { task_id: 'task-1', sequence: 3, node: 'implement', status: 'running' });
			await expect(toggle).toContainText('2 of 3');
			await expect(book.getByRole('region', { name: 'Remaining' }).getByRole('listitem')).toHaveText(['The docs']);
			await expect(book.getByRole('region', { name: 'Done' })).toContainText('The search box filters the list');
			// It stays open while it changes.
			await expect(toggle).toHaveAttribute('aria-expanded', 'true');
		});

		test('a step opens the prompt it received, one section per source', async ({ page }) => {
			const { state } = await fixture(page);
			await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
			const step = page.locator('#step-2');
			await expect(step).toBeVisible();
			// Nothing of it is read or shown until it is asked for.
			await expect(page.getByRole('dialog')).toHaveCount(0);
			expect(state.requests.filter((rest) => rest.endsWith('/prompt'))).toEqual([]);

			// It is asked for from the opened step; a closed line is one control.
			await expect(step.getByRole('button')).toHaveCount(1);
			await step.getByRole('button').first().click();
			await step.getByRole('button', { name: 'Prompt · Implementer' }).click();
			const sheet = page.getByRole('dialog', { name: 'Prompt · Implementer' });
			await expect(sheet).toBeVisible();
			await expect(sheet.getByText('Read-only worktree')).toBeVisible();
			// The source names are the headings, in the order the prompt was
			// assembled; an empty source is left out.
			await expect(sheet.getByRole('heading', { level: 3 })).toHaveText([
				/^\s*Workflow step/,
				/^\s*Profile/,
				/^\s*Kuayle.s rules/,
				/^\s*Task/,
				/^\s*Notebook/,
				/^\s*Lessons/
			]);
			await expect(sheet.getByRole('heading', { name: /^Workflow step/ })).toContainText('69 B');
			await expect(sheet.getByRole('heading', { name: /^Profile/ })).toContainText('Instructions file');
			// The conflicting pair is on screen together.
			await expect(sheet.getByText('then implement them')).toBeVisible();
			await expect(sheet.getByText('Do not implement the steps yourself.')).toBeVisible();

			if (shots && size.name === 'desktop') {
				// The sheet slides in.
				await page.waitForTimeout(500);
				await page.screenshot({ path: `${shots}/2-prompt-by-source.png` });
			}

			// A long section shows its first lines and expands in place.
			const taskSection = sheet.locator('[data-source="task"]');
			const body = taskSection.getByText('Request line 30');
			await expect(body).toHaveClass(/line-clamp-6/);
			await taskSection.getByRole('button', { name: 'Expand' }).click();
			await expect(body).not.toHaveClass(/line-clamp-6/);
			await expect(taskSection.getByRole('button', { name: 'Show less' })).toBeVisible();
			// Each section closes.
			const profile = sheet.getByRole('button', { name: /^Profile/ });
			await profile.click();
			await expect(profile).toHaveAttribute('aria-expanded', 'false');
			await expect(sheet.getByText('Do not implement the steps yourself.')).toBeHidden();
			await profile.click();
			expect(await fits(page)).toBe(true);

			await page.keyboard.press('Escape');
			await expect(page.getByRole('dialog')).toHaveCount(0);
			// A run whose prompt was not recorded says so.
			await page.locator('#step-3').getByRole('button', { name: 'Prompt · Implement' }).click();
			await expect(page.getByRole('dialog')).toContainText('The prompt of this run was not recorded');
		});

		test('a run that did not update its notebook carries a quiet flag', async ({ page }) => {
			const { state } = await fixture(page);
			const view = notebook();
			view.notebook.done.push(
				"[Recorded by Kuayle from the agent's final message; the agent did not update the notebook] (Implementer) The plan is ready."
			);
			view.history.unshift({
				revision: 6,
				author: 'kuayle',
				run_id: 'run-plan',
				generation: 1,
				step_sequence: 2,
				step_label: 'Implementer',
				sections: ['done'],
				summary: 'The agent did not update the task notebook, so Kuayle recorded its final message in it instead',
				created_at: at(4)
			});
			view.revision = 6;
			state.notebook = view;
			await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
			const flag = page.getByRole('img', { name: 'Did not update the notebook' });
			await expect(flag).toHaveCount(1);
			// On the line of the step whose run it was, also while it is closed.
			const line = page.locator('#step-2').getByRole('button').first();
			await expect(line).toHaveAttribute('aria-expanded', 'false');
			await expect(line.getByRole('img', { name: 'Did not update the notebook' })).toBeVisible();
			// The notebook says who recorded that entry, without the marker.
			const book = page.getByRole('region', { name: 'Notebook', exact: true });
			await book.getByRole('button', { name: /^Notebook/ }).click();
			const done = book.getByRole('region', { name: 'Done' });
			await expect(done).toContainText('(Implementer) The plan is ready.');
			await expect(done).toContainText('Recorded by Kuayle');
			await expect(done).not.toContainText('[Recorded by Kuayle');
			expect(await fits(page)).toBe(true);
		});
	});
}

test("another member's task shows no notebook and no prompt", async ({ page }) => {
	const { state } = await fixture(page);
	state.task = task({ creator_id: 'user-2' });
	state.notebook = null;
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto('/test/assistant?conversation=chat-1&agent=task-1');
	await expect(page.locator('#step-2')).toBeVisible();
	await expect(page.getByRole('region', { name: 'Notebook', exact: true })).toHaveCount(0);
	await page.locator('#step-2').getByRole('button').first().click();
	await expect(page.locator('#step-2').getByRole('article', { name: 'Plan' })).toBeVisible();
	await expect(page.getByRole('button', { name: /^Prompt ·/ })).toHaveCount(0);
	expect(state.requests).not.toContain('/notebook');
});
