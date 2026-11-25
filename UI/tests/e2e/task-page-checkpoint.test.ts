import { test, expect, type Locator, type Page, type TestInfo } from '@playwright/test';

// The Phase 3 checkpoint (KUA-296): on a desktop and at phone width, with the
// keyboard only, the owner answers from the task page who is working, which
// phase the task is in, what blocks it and what changed, and opens a changed
// file with its diff. Every control is reached with Tab and used with Enter or
// the arrow keys; nothing is clicked. Served from mocked API routes.
test.use({ timezoneId: 'UTC', locale: 'en-US' });

const at = (minute: number) => `2026-11-25T10:${String(minute).padStart(2, '0')}:00Z`;
const HEAD = 'abc1234def5678abc1234def5678abc1234def56';
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

// A team pipeline in its Integrate phase: CI failed on the head, the
// reviewer asked for changes, and the implementer is fixing lint.
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
	updated_at: at(12)
};
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
const run = (id: string, taskId: string, role: string, provider: string, status: string, minute: number, over = {}) => ({
	id,
	task_id: taskId,
	role,
	provider,
	model: provider === 'codex' ? 'gpt-5' : 'claude-opus-5-5',
	status,
	created_at: at(minute),
	...(status === 'succeeded' ? { completed_at: at(minute + 3) } : {}),
	...over
});
const view = {
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
			{ id: 'merge', label: 'Merge', owner: 'kuayle', guarded: true }
		],
		roster: [
			{ role: 'implementer', profile_id: 'p-impl', writer: true },
			{ role: 'reviewer', profile_id: 'p-rev' }
		],
		budget: { max_turns: 40 }
	},
	node: 'integrate',
	status: 'waiting',
	visits: {},
	iterations: 3,
	sequence: 9,
	steps: [],
	runs: [
		run('run-impl-1', 'task-1', 'implementer', 'claude-code', 'succeeded', 1, {
			summary: 'Search is added.',
			input_tokens: 1000,
			output_tokens: 200
		}),
		run('run-rev-1', 'member-rev', 'reviewer', 'codex', 'succeeded', 6),
		run('run-impl-2', 'task-1', 'implementer', 'claude-code', 'running', 11)
	],
	usage: { input_tokens: 120_000, output_tokens: 8_000, cost_usd: 1.25 },
	profiles: [profile('p-impl', 'Implementer', 'claude-code', 'claude-opus-5-5'), profile('p-rev', 'Reviewer', 'codex', 'gpt-5')],
	can_decide: false,
	task_state: 'running',
	task_reason: '',
	task_generation: 2
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
const fact = (seq: number, name: string, value: unknown, body = '') => post(seq, 'kuayle', 'fact', body, { data: { fact: name, value } });
const room = [
	post(1, 'human', 'say', 'Search should cover titles and identifiers.'),
	post(2, 'kuayle', 'phase', '', { data: { phase: 'implement' } }),
	member(3, 'implementer', 'run-impl-1', 'say', 'Pushed the first version.', { to_roles: ['reviewer'] }),
	fact(4, 'pr_exists', true),
	fact(5, 'head_sha', HEAD),
	fact(6, 'ci_green', { state: 'failing', head: HEAD, failing: ['lint'] }, '1 check(s) failed on abc1234: lint. Next step: read their output, fix the cause and push.'),
	fact(7, 'migration_collision', false),
	post(8, 'kuayle', 'phase', '', { data: { phase: 'integrate' } }),
	member(9, 'reviewer', 'run-rev-1', 'verdict', 'Rename q to query.', {
		data: { verdict: 'request_changes', commit: HEAD.slice(0, 7), role: 'reviewer' }
	}),
	member(10, 'implementer', 'run-impl-2', 'say', 'Fixing lint.')
];

// What the task changed: the files at its head, two of them changed by the
// newest attempt that changed files, and that attempt's diff.
const search = ['export function search(query: string) {', '\treturn issues.filter((issue) => issue.title.includes(query));', '}', ''].join('\n');
const diff = [
	'diff --git a/docs/notes.md b/docs/notes.md',
	'@@ -1 +1 @@',
	'-Old notes',
	'+Search covers titles.',
	'diff --git a/src/lib/search.ts b/src/lib/search.ts',
	'new file mode 100644',
	'--- /dev/null',
	'+++ b/src/lib/search.ts',
	'@@ -0,0 +1,3 @@',
	...search.trimEnd().split('\n').map((line) => `+${line}`)
].join('\n');
const listing = {
	commit: HEAD,
	files: [
		{ path: 'README.md', changed: false },
		{ path: 'docs/notes.md', changed: true },
		{ path: 'src/lib/search.ts', changed: true },
		{ path: 'src/lib/util/strings.ts', changed: false },
		{ path: 'src/main.go', changed: false }
	],
	truncated: false
};

async function fixture(page: Page) {
	const requests: string[] = [];
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
		if (path.endsWith('/conversations/chat-1/tasks')) return route.fulfill({ json: { tasks: [task] } });
		if (path.endsWith('/events')) return route.fulfill({ json: [] });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false } });
		const match = path.match(/\/agent-tasks\/([^/]+)(\/.*)?$/);
		if (match) {
			const [, id, rest = ''] = match;
			requests.push(`${id}${rest}`);
			if (rest === '/workflow') return route.fulfill({ json: view });
			if (rest === '/room')
				return route.fulfill({ json: { messages: room.filter((message) => message.seq > Number(url.searchParams.get('after') ?? 0)) } });
			if (rest === '/files') return route.fulfill({ json: listing });
			if (rest === '/files/content') {
				const file = url.searchParams.get('path') ?? '';
				const content = file === 'src/lib/search.ts' ? search : `# ${file}\n`;
				const total = content.trimEnd().split('\n').length;
				return route.fulfill({
					json: { path: file, commit: HEAD, content, start: 1, end: total, total_lines: total, truncated: false, binary: false, too_large: false }
				});
			}
			if (rest === '/runs')
				return route.fulfill({
					json: {
						runs: [
							{ id: 'run-impl-2', status: 'running', changed_files: [], commits: [], created_at: at(11) },
							{ id: 'run-impl-1', status: 'succeeded', changed_files: ['docs/notes.md', 'src/lib/search.ts'], commits: [], created_at: at(1) }
						],
						usage: {}
					}
				});
			if (rest === '/runs/run-impl-1/diff') return route.fulfill({ json: { diff, truncated: false } });
			if (/^\/runs\/[^/]+\/logs$/.test(rest)) return route.fulfill({ json: { logs: [] } });
			if (rest.endsWith('/checks') || rest === '/signals') return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/notebook') return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			if (rest === '/publication') return route.fulfill({ json: { step: null } });
			if (id === task.id && rest === '') return route.fulfill({ json: task });
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		}
		return route.fulfill({ json: [] });
	});
	return { requests };
}

const agentView = '/test/assistant?conversation=chat-1&agent=task-1';
const roomRows = (page: Page) => page.getByRole('list', { name: 'Task room' }).locator(':scope > li');
const morePanels = (page: Page) =>
	page.getByRole('group', { name: 'Side panels' }).getByRole('button', { name: 'More panels', exact: true });
const tree = (page: Page) => page.getByRole('region', { name: 'File tree' });
const fits = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

// Presses Tab until the target has the focus: the target is reachable with the
// keyboard, in the page's own order.
async function tabTo(page: Page, target: Locator, max = 150) {
	await expect(target).toBeVisible();
	for (let i = 0; i < max; i++) {
		if (await target.evaluate((element) => element === document.activeElement)) return;
		await page.keyboard.press('Tab');
	}
	throw new Error(`Tab never reached ${target}`);
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

// Who works and where the task is: the phases with the current one and why it
// waits, and the members with who works now.
async function expectWhoAndWhere(scope: Locator) {
	const current = scope.getByRole('list', { name: 'Phases' }).locator('[aria-current="step"]');
	await expect(current).toContainText('Integrate');
	await expect(current).toContainText('1 check(s) failed on abc1234: lint.');
	const members = scope.getByRole('list', { name: 'Members' });
	await expect(members.getByTestId('task-member-implementer')).toContainText('Working');
	await expect(members.getByTestId('task-member-reviewer')).toContainText('Idle');
}

// What blocks it, check by check, with Kuayle's reason.
async function expectBlocks(page: Page) {
	const checks = page.getByRole('region', { name: 'Checks', exact: true });
	await expect(checks.getByTestId('checks-phase-integrate')).toContainText('Current');
	const ci = checks.getByTestId('check-ci');
	await expect(ci).toHaveAttribute('data-state', 'unmet');
	await expect(ci).toContainText('required');
	await expect(ci).toContainText('1 check(s) failed on abc1234: lint.');
	await expect(checks.getByTestId('check-reviewer')).toContainText('reviewer: Changes requested on this head.');
}

// What changed: the changed files marked in the tree; one opened by keyboard
// and toggled to its diff and back.
async function expectChanges(page: Page) {
	await expect(page.getByTestId('files-summary')).toHaveText('5 files · 2 changed · at abc1234');
	await expect(tree(page).getByRole('button', { name: 'docs Contains changed files' })).toBeVisible();
	const lib = tree(page).getByRole('button', { name: 'lib Contains changed files' });
	await expect(lib).toHaveAttribute('aria-expanded', 'false');
	await tabTo(page, lib);
	await page.keyboard.press('Enter');
	await expect(lib).toHaveAttribute('aria-expanded', 'true');
	const file = tree(page).getByRole('button', { name: 'search.ts Changed' });
	await tabTo(page, file, 5);
	await page.keyboard.press('Enter');
	await expect(file).toHaveAttribute('aria-current', 'true');

	const preview = page.getByRole('region', { name: 'src/lib/search.ts' });
	await expect(preview.locator('[data-slot=code-block-line]')).toHaveCount(3);
	const tabs = preview.getByRole('tablist', { name: 'Show the file or its diff' });
	await tabTo(page, tabs.getByRole('tab', { name: 'File' }));
	await page.keyboard.press('ArrowRight');
	await expect(tabs.getByRole('tab', { name: 'Diff' })).toHaveAttribute('aria-selected', 'true');
	await expect(tabs.getByRole('tab', { name: 'Diff' })).toBeFocused();
	const changes = preview.getByTestId('file-diff');
	await expect(changes).toContainText('+export function search(query: string) {');
	await expect(changes).not.toContainText('Search covers titles.');
}

test('desktop, keyboard only: who works, which phase, what blocks it and what changed', async ({ page }, testInfo) => {
	const { requests } = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.goto(agentView);
	await expect(roomRows(page)).toHaveCount(7);

	// The header says it in one line: the phase, the task's state and how many work.
	const header = page.getByTestId('agent-thread-header');
	await expect(header.getByRole('status')).toHaveText('Integrate · Running');
	await expect(header.getByTestId('thread-working')).toHaveAttribute('title', '1 working');
	// The task column beside the room says who and where.
	await expectWhoAndWhere(page.getByTestId('task-column'));

	// The checks, from the top bar's panel menu (the bar is narrow beside two columns).
	await tabTo(page, morePanels(page));
	await page.keyboard.press('Enter');
	await expect(page.getByRole('menuitemcheckbox', { name: 'Details' })).toBeFocused();
	await page.keyboard.press('End');
	await expect(page.getByRole('menuitemcheckbox', { name: 'Checks' })).toBeFocused();
	await page.keyboard.press('Enter');
	await expect(page.getByTestId('side-panel')).toBeVisible();
	await expectBlocks(page);

	// The files, from the side panel's own switcher.
	const switcher = page.getByTestId('side-panel').getByRole('toolbar', { name: 'Side panels' });
	await tabTo(page, switcher.getByRole('button', { name: 'Files', exact: true }));
	await page.keyboard.press('Enter');
	await expect(switcher.getByRole('button', { pressed: true })).toHaveAccessibleName('Files');
	await expectChanges(page);
	expect(requests).toContain('task-1/runs/run-impl-1/diff');
	expect(await fits(page)).toBe(true);
	await shots(page, testInfo, 'checkpoint-desktop');

	// And back to the file.
	await page.keyboard.press('ArrowLeft');
	await expect(page.getByRole('tab', { name: 'File' })).toHaveAttribute('aria-selected', 'true');
	await expect(page.getByRole('region', { name: 'src/lib/search.ts' }).locator('[data-slot=code-block-line]')).toHaveCount(3);
});

test('phone, keyboard only: the same answers from the top bar and its sheets', async ({ page }, testInfo) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(agentView);
	await expect(roomRows(page)).toHaveCount(7);
	await expect(page.getByTestId('agent-thread-header').getByRole('status')).toHaveText('Integrate · Running');
	expect(await fits(page)).toBe(true);
	await shots(page, testInfo, 'checkpoint-phone');

	// Who and where: the task overview opens as a sheet and gives the focus back.
	const overview = page.getByRole('button', { name: 'Task overview', exact: true });
	await tabTo(page, overview);
	await page.keyboard.press('Enter');
	const sheet = page.getByRole('dialog', { name: 'Task overview' });
	await expect(sheet).toBeVisible();
	await expectWhoAndWhere(sheet);
	await page.keyboard.press('Escape');
	await expect(sheet).toHaveCount(0);
	await expect(overview).toBeFocused();

	// What blocks it: the Checks sheet, from the top bar's one panel menu.
	await tabTo(page, morePanels(page));
	await page.keyboard.press('Enter');
	await page.keyboard.press('End');
	await expect(page.getByRole('menuitemcheckbox', { name: 'Checks' })).toBeFocused();
	await page.keyboard.press('Enter');
	await expect(page.getByRole('dialog', { name: 'Checks' })).toBeVisible();
	await expectBlocks(page);
	await page.keyboard.press('Escape');
	await expect(page.getByRole('dialog')).toHaveCount(0);
	await expect(morePanels(page)).toBeFocused();

	// What changed: the Files sheet, a file and its diff.
	await page.keyboard.press('Enter');
	await expect(page.getByRole('menuitemcheckbox', { name: 'Details' })).toBeFocused();
	for (const name of ['Agents', 'Workflow', 'Files']) {
		await page.keyboard.press('ArrowDown');
		await expect(page.getByRole('menuitemcheckbox', { name })).toBeFocused();
	}
	await page.keyboard.press('Enter');
	const files = page.getByRole('dialog', { name: 'Files' });
	await expect(files.getByTestId('files-panel')).toBeVisible();
	await expectChanges(page);
	expect(await fits(page)).toBe(true);
	await shots(page, testInfo, 'checkpoint-phone-files');
});
