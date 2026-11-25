import { test, expect, type Page } from '@playwright/test';

// A task's files panel (KUA-285): the tree of the files the task's head
// commit tracks, the files the task changed marked, a path filter, a preview
// with line numbers and highlighting that pages through a long file, and a
// changed file's part of the stored diff. Served from mocked API routes.
const time = '2026-11-25T10:00:00Z';
const commit = 'abc1234def5678abc1234def5678abc1234def56';
const shots = process.env.KUA285_SHOTS;

const task = {
	id: 'task-1',
	workspace_id: 'workspace-1',
	conversation_id: 'chat-1',
	creator_id: 'user-1',
	issue_id: 'issue-1',
	team_id: 'team-1',
	kind: 'implement',
	prompt: 'Add search to the issue list.',
	provider: 'claude-code',
	model: '',
	thinking: '',
	state: 'awaiting_review',
	reason: '',
	generation: 2,
	failure_count: 0,
	machine_id: 'machine-1',
	checkout_id: 'checkout-1',
	run_id: 'run-2',
	created_at: time,
	updated_at: time
};

const search = [
	'export function search(query: string) {',
	'\t// Match titles only.',
	'\treturn issues.filter((issue) => issue.title.includes(query));',
	'}',
	''
].join('\n');
// The newest attempt that changed files (run-1); the newest attempt (run-2) changed none.
const diff = [
	'diff --git a/docs/notes.md b/docs/notes.md',
	'@@ -1 +1 @@',
	'-Old notes',
	'+New notes',
	'diff --git a/src/lib/search.ts b/src/lib/search.ts',
	'new file mode 100644',
	'--- /dev/null',
	'+++ b/src/lib/search.ts',
	'@@ -0,0 +1,4 @@',
	...search.trimEnd().split('\n').map((line) => `+${line}`)
].join('\n');

// A long log: 12,000 lines served 5,000 at a time, each page over the highlighting cap.
const logLine = (n: number) => `2026-11-25 10:00:00 line ${String(n).padStart(5, '0')} ${'x'.repeat(10)}`;
const logPage = (start: number) => {
	const end = Math.min(start + 4999, 12000);
	const lines = Array.from({ length: end - start + 1 }, (_, i) => logLine(start + i));
	return { content: `${lines.join('\n')}\n`, start, end, total_lines: 12000, truncated: end < 12000 };
};

const listing = (over: Record<string, unknown> = {}) => ({
	commit,
	files: [
		{ path: '.github/workflows/ci.yml', changed: false },
		{ path: 'README.md', changed: false },
		{ path: 'assets/logo.png', changed: false },
		{ path: 'docs/build.log', changed: false },
		{ path: 'src/lib/search.ts', changed: true },
		{ path: 'src/lib/util/strings.ts', changed: false },
		{ path: 'src/main.go', changed: false }
	],
	truncated: false,
	...over
});

type Failure = { status: number; code: string };
async function fixture(page: Page, theme: 'light' | 'dark' = 'light') {
	const conversation = { id: 'chat-1', workspace_id: 'workspace-1', creator_id: 'user-1', issue_id: 'issue-1', team_id: 'team-1', title: 'Search', created_at: time, updated_at: time };
	const issue = { id: 'issue-1', identifier: 'ENG-1', title: 'Search', status: 'todo', description: '' };
	const state = {
		listing: listing() as Record<string, unknown>,
		listingFailure: null as Failure | null,
		contentFailures: {} as Record<string, Failure>,
		requests: [] as string[]
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.routeWebSocket('**/conversations/chat-1/stream*', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url()),
			path = url.pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/preferences')
			return route.fulfill({ json: { theme_mode: theme, light_theme: 'light', dark_theme: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/assistant/status')) return route.fulfill({ json: { configured: true, model: 'assistant-model' } });
		if (path.endsWith('/conversations')) return route.fulfill({ json: [conversation] });
		if (path.endsWith('/task-options')) return route.fulfill({ json: { providers: [], model_servers: [] } });
		if (path.endsWith('/tasks')) return route.fulfill({ json: { tasks: [task] } });
		if (path.endsWith('/events')) return route.fulfill({ json: [] });
		if (path.endsWith('/conversations/chat-1'))
			return route.fulfill({ json: { conversation, issue, messages: [], agent_messages: [], cursor: 0, has_more: false } });
		const match = path.match(/\/agent-tasks\/task-1(\/.*)?$/);
		if (match) {
			const rest = match[1] ?? '';
			const fail = (failure: Failure) =>
				route.fulfill({ status: failure.status, json: { error: { code: failure.code, message: failure.code } } });
			if (rest === '/files') {
				state.requests.push(`files${url.search}`);
				return state.listingFailure ? fail(state.listingFailure) : route.fulfill({ json: state.listing });
			}
			if (rest === '/files/content') {
				const file = url.searchParams.get('path') ?? '';
				const start = Number(url.searchParams.get('start') ?? 1);
				state.requests.push(`content ${file} ref=${url.searchParams.get('ref')} start=${start}`);
				if (state.contentFailures[file]) return fail(state.contentFailures[file]);
				const base = { path: file, commit, binary: false, too_large: false };
				if (file === 'assets/logo.png')
					return route.fulfill({ json: { ...base, content: '', start: 1, end: 0, total_lines: 0, truncated: false, binary: true } });
				if (file === 'docs/build.log') return route.fulfill({ json: { ...base, ...logPage(start) } });
				const content = file === 'src/lib/search.ts' ? search : '# Search\n\nSearch issues by title.\n';
				const total = content.trimEnd().split('\n').length;
				return route.fulfill({ json: { ...base, content, start: 1, end: total, total_lines: total, truncated: false } });
			}
			if (rest === '/runs') {
				state.requests.push('runs');
				return route.fulfill({
					json: {
						runs: [
							{ id: 'run-2', status: 'succeeded', changed_files: [], commits: [], created_at: time },
							{ id: 'run-1', status: 'succeeded', changed_files: ['src/lib/search.ts'], commits: [], created_at: time }
						]
					}
				});
			}
			const runDiff = rest.match(/^\/runs\/([^/]+)\/diff$/);
			if (runDiff) {
				state.requests.push(`diff ${runDiff[1]}`);
				return route.fulfill({ json: { diff, truncated: false } });
			}
			if (/^\/runs\/[^/]+\/logs$/.test(rest)) return route.fulfill({ json: { logs: [] } });
			if (rest.endsWith('/checks') || rest === '/signals') return route.fulfill({ json: [] });
			if (rest === '/live-messages') return route.fulfill({ json: { messages: [] } });
			if (rest === '/history') return route.fulfill({ json: { transitions: [] } });
			if (rest === '/sessions') return route.fulfill({ json: { sessions: [] } });
			if (rest === '/workflow') return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
			return route.fulfill({ json: task });
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

async function openFiles(page: Page) {
	await page.goto('/test/assistant?conversation=chat-1');
	const button = page.getByRole('group', { name: 'Side panels' }).getByRole('button', { name: 'Files', exact: true });
	await expect(button).toBeVisible();
	await button.click();
	const panel = page.getByTestId('files-panel');
	await expect(panel).toBeVisible();
	return panel;
}
const tree = (page: Page) => page.getByRole('region', { name: 'File tree' });
const fits = (page: Page) =>
	page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

test('desktop: the tree marks changes, filters by path and opens a file with its diff', async ({ page }) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1280, height: 900 });
	const panel = await openFiles(page);
	await expect(panel.getByTestId('files-summary')).toHaveText('7 files · 1 changed · at abc1234');
	// The head by default: no ref.
	expect(state.requests.filter((request) => request.startsWith('files'))).toEqual(['files']);

	// First-level folders are open; deeper ones closed. The changed file's folders say so.
	const src = tree(page).getByRole('button', { name: 'src Contains changed files' });
	const lib = tree(page).getByRole('button', { name: 'lib Contains changed files' });
	await expect(src).toHaveAttribute('aria-expanded', 'true');
	await expect(lib).toHaveAttribute('aria-expanded', 'false');
	await expect(tree(page).getByRole('button', { name: 'workflows' })).toHaveAttribute('aria-expanded', 'false');
	await expect(tree(page).getByRole('button', { name: 'main.go' })).toBeVisible();
	await expect(tree(page).getByRole('button', { name: /^search\.ts/ })).toHaveCount(0);
	await expect(panel.getByText('Choose a file to preview it.')).toBeVisible();

	// A filter opens the folders holding its matches; no match offers to clear it.
	const filter = panel.getByRole('searchbox', { name: 'Filter files' });
	await filter.fill('SEARCH');
	await expect(tree(page).getByRole('button', { name: 'search.ts Changed' })).toBeVisible();
	await expect(tree(page).getByRole('button', { name: 'main.go' })).toHaveCount(0);
	await filter.fill('nothing-like-this');
	await expect(tree(page).getByText('No files match “nothing-like-this”.')).toBeVisible();
	await tree(page).getByRole('button', { name: 'Clear filter' }).click();
	await expect(filter).toHaveValue('');
	// The reader's folders come back as they were.
	await expect(lib).toHaveAttribute('aria-expanded', 'false');

	// Keyboard only: open the folder and the file with the primitives' own keys.
	await lib.focus();
	await page.keyboard.press('Enter');
	await expect(lib).toHaveAttribute('aria-expanded', 'true');
	await page.keyboard.press('Tab');
	await expect(tree(page).getByRole('button', { name: 'util' })).toBeFocused();
	await page.keyboard.press('Tab');
	const file = tree(page).getByRole('button', { name: 'search.ts Changed' });
	await expect(file).toBeFocused();
	await page.keyboard.press('Enter');
	await expect(file).toHaveAttribute('aria-current', 'true');

	// The preview numbers and highlights the file read at the listed commit.
	const preview = page.getByRole('region', { name: 'src/lib/search.ts' });
	const code = preview.locator('[data-slot=code-block-content]');
	await expect(code).toHaveAttribute('data-numbered', 'true');
	await expect(code.locator('[data-slot=code-block-line]')).toHaveCount(4);
	await expect(code.locator('.hljs-keyword').first()).toHaveText('export');
	await expect(code.locator('.hljs-comment')).toHaveText('// Match titles only.');
	expect(state.requests).toContain(`content src/lib/search.ts ref=${commit} start=1`);

	// A changed file shows its own part of the newest attempt that changed files.
	const tabs = preview.getByRole('tablist', { name: 'Show the file or its diff' });
	await expect(tabs.getByRole('tab', { name: 'File' })).toHaveAttribute('aria-selected', 'true');
	await tabs.getByRole('tab', { name: 'Diff' }).click();
	const changes = preview.getByTestId('file-diff');
	await expect(changes).toContainText('+export function search(query: string) {');
	await expect(changes).not.toContainText('New notes');
	expect(state.requests).toContain('diff run-1');
	expect(state.requests).not.toContain('diff run-2');
	if (shots) await page.screenshot({ path: `${shots}/files-desktop-light.png`, animations: 'disabled' });

	// An unchanged file has no diff to show.
	await tree(page).getByRole('button', { name: 'README.md' }).click();
	const readme = page.getByRole('region', { name: 'README.md' });
	await expect(readme.locator('[data-slot=code-block-line]')).toHaveCount(3);
	await expect(readme.getByRole('tablist')).toHaveCount(0);
	expect(await fits(page)).toBe(true);
});

test('a long file pages by line range, plain above the highlighting cap; a truncated listing says so', async ({ page }) => {
	const state = await fixture(page);
	state.listing = listing({ truncated: true });
	await page.setViewportSize({ width: 1280, height: 900 });
	const panel = await openFiles(page);
	await expect(tree(page).getByRole('note')).toHaveText('Only the first 7 files are listed; this commit tracks more.');

	await tree(page).getByRole('button', { name: 'docs' }).click();
	await expect(tree(page).getByRole('button', { name: 'docs' })).toHaveAttribute('aria-expanded', 'false');
	await tree(page).getByRole('button', { name: 'docs' }).click();
	await tree(page).getByRole('button', { name: 'build.log' }).click();
	const preview = page.getByRole('region', { name: 'docs/build.log' });
	const pages = preview.getByTestId('file-pages');
	await expect(pages).toContainText('Lines 1–5,000 of 12,000');
	await expect(preview.getByText('Shown without highlighting because this part of the file is large.')).toBeVisible();
	const lines = preview.locator('[data-slot=code-block-line]');
	await expect(lines.first()).toHaveText(logLine(1));
	await expect(pages.getByRole('button', { name: 'Previous lines' })).toBeDisabled();

	await pages.getByRole('button', { name: 'Next lines' }).click();
	await expect(pages).toContainText('Lines 5,001–10,000 of 12,000');
	await expect(lines.first()).toHaveText(logLine(5001));
	// The numbering continues from the page's first line.
	await expect(preview.locator('[data-slot=code-block-content]')).toHaveAttribute('style', /counter-reset: line 5000/);
	expect(state.requests).toContain(`content docs/build.log ref=${commit} start=5001`);
	await pages.getByRole('button', { name: 'Next lines' }).click();
	await expect(pages).toContainText('Lines 10,001–12,000 of 12,000');
	await expect(pages.getByRole('button', { name: 'Next lines' })).toBeDisabled();
	await pages.getByRole('button', { name: 'Previous lines' }).click();
	await expect(pages).toContainText('Lines 5,001–10,000 of 12,000');
	await pages.getByRole('button', { name: 'Previous lines' }).click();
	await expect(pages).toContainText('Lines 1–5,000 of 12,000');

	// A binary file is listed, never shown.
	await tree(page).getByRole('button', { name: 'logo.png' }).click();
	await expect(page.getByRole('region', { name: 'assets/logo.png' })).toContainText(
		'This is a binary file; its content is not shown.'
	);
	await expect(panel.getByRole('region', { name: 'assets/logo.png' }).locator('[data-slot=code-block-line]')).toHaveCount(0);
});

test('another member is told the files are the creator’s; failures offer the way out', async ({ page }) => {
	const state = await fixture(page);
	state.listingFailure = { status: 404, code: 'NOT_FOUND' };
	await page.setViewportSize({ width: 1280, height: 900 });
	const panel = await openFiles(page);
	const error = panel.getByTestId('files-error');
	await expect(error).toHaveText('Only the person who started this task can browse its files.');
	await expect(error.getByRole('button')).toHaveCount(0);
	await expect(panel.getByRole('searchbox')).toHaveCount(0);
	await expect(tree(page)).toHaveCount(0);

	// The machine is not running and nothing is published: start it, then try again.
	state.listingFailure = { status: 409, code: 'FILES_UNAVAILABLE' };
	await panel.getByRole('button', { name: 'Reload files' }).click();
	await expect(error).toContainText('Start the machine to browse its files.');
	state.listingFailure = null;
	await error.getByRole('button', { name: 'Try again' }).click();
	await expect(tree(page).getByRole('button', { name: 'main.go' })).toBeVisible();

	// The head moved on: the file is no longer at the listed commit.
	state.contentFailures['src/main.go'] = { status: 404, code: 'FILE_NOT_FOUND' };
	await tree(page).getByRole('button', { name: 'main.go' }).click();
	const preview = page.getByRole('region', { name: 'src/main.go' });
	await expect(preview.getByRole('alert')).toContainText("The task's files changed since this list was loaded.");
	const listings = state.requests.filter((request) => request === 'files').length;
	await preview.getByRole('button', { name: 'Reload files' }).click();
	await expect.poll(() => state.requests.filter((request) => request === 'files').length).toBe(listings + 1);
});

test('phone: the panel is a sheet; the tree and the preview fit its width', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await openFiles(page);
	const sheet = page.getByRole('dialog');
	await expect(sheet.getByTestId('files-panel')).toBeVisible();
	await tree(page).getByRole('button', { name: 'lib Contains changed files' }).click();
	await tree(page).getByRole('button', { name: 'search.ts Changed' }).click();
	const preview = page.getByRole('region', { name: 'src/lib/search.ts' });
	await expect(preview.locator('[data-slot=code-block-line]')).toHaveCount(4);
	await preview.getByRole('tab', { name: 'Diff' }).click();
	await expect(preview.getByTestId('file-diff')).toContainText('+export function search');
	const box = (await preview.boundingBox())!;
	expect(box.x).toBeGreaterThanOrEqual(0);
	expect(box.x + box.width).toBeLessThanOrEqual(390);
	expect(await fits(page)).toBe(true);
	if (shots) await page.screenshot({ path: `${shots}/files-phone-light.png`, animations: 'disabled' });
});

// Screenshots for the pull request in the dark theme (KUA285_SHOTS=<dir>).
test('dark theme screenshots', async ({ page }) => {
	test.skip(!shots, 'only when taking screenshots');
	await fixture(page, 'dark');
	for (const size of [
		{ name: 'desktop', width: 1280, height: 900 },
		{ name: 'phone', width: 390, height: 844 }
	]) {
		await page.setViewportSize({ width: size.width, height: size.height });
		await openFiles(page);
		await tree(page).getByRole('button', { name: 'lib Contains changed files' }).click();
		await tree(page).getByRole('button', { name: 'search.ts Changed' }).click();
		await expect(page.locator('.hljs-keyword').first()).toBeVisible();
		await page.screenshot({ path: `${shots}/files-${size.name}-dark.png`, animations: 'disabled' });
	}
});
