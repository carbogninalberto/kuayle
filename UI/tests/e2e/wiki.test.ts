import { expect, test, type Page } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const projectId = '00000000-0000-0000-0000-000000000020';
const statusId = '00000000-0000-0000-0000-000000000030';
const teamWikiPath = `/test/teams/${teamId}/wiki`;
const projectWikiPath = `/test/projects/${projectId}/wiki`;
const now = '2026-01-01T00:00:00Z';

type StoredPage = {
	id: string;
	team_id: string | null;
	project_id: string | null;
	title: string;
	content: string;
	version: number;
	updated_by_name: string | null;
	updated_at: string;
};

// A workspace with one team and one project whose wiki API keeps pages in
// memory, so a reload reads back what was saved.
async function mockWorkspace(page: Page, options: { role?: string; wikiStatus?: number; conflict?: boolean } = {}) {
	const { role = 'member', wikiStatus, conflict = false } = options;
	await page.routeWebSocket('**/ws', () => {});
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	const writes: { method: string; path: string; body: any }[] = [];
	const pages: StoredPage[] = [];
	let nextId = 1;
	const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', created_at: now };
	const project = { id: projectId, name: 'Launch', team_id: teamId, status: 'planned', created_at: now };
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user', name: 'Ada', email: 'ada@example.com' } });
		if (path === '/api/preferences') return route.fulfill({ json: { issues_group_by: 'status', theme_mode: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'workspace', slug: 'test', name: 'Test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: 'workspace', slug: 'test', name: 'Test', current_user_role: role } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === `/api/workspaces/test/teams/${teamId}`) return route.fulfill({ json: team });
		if (path === '/api/workspaces/test/projects' || path === `/api/workspaces/test/teams/${teamId}/projects`)
			return route.fulfill({ json: [project] });
		if (path === `/api/workspaces/test/projects/${projectId}`)
			return route.fulfill({ json: { ...project, progress: { total: 0, completed: 0, cancelled: 0 } } });

		const wiki = path.match(/^\/api\/workspaces\/test\/(teams|projects)\/([^/]+)\/wiki(?:\/([^/]+))?$/);
		if (wiki) {
			if (wikiStatus) return route.fulfill({ status: wikiStatus, json: { error: { code: 'NOT_FOUND', message: 'Wiki page not found' } } });
			const [, kind, scopeId, pageId] = wiki;
			const inScope = (p: StoredPage) => (kind === 'teams' ? p.team_id : p.project_id) === scopeId;
			const body = request.postData() ? request.postDataJSON() : null;
			if (method !== 'GET') writes.push({ method, path, body });
			if (!pageId && method === 'GET')
				return route.fulfill({ json: pages.filter(inScope).map(({ content: _content, ...summary }) => summary) });
			if (!pageId && method === 'POST') {
				const created: StoredPage = {
					id: `00000000-0000-0000-0000-${String(900 + nextId++).padStart(12, '0')}`,
					team_id: kind === 'teams' ? scopeId : null,
					project_id: kind === 'projects' ? scopeId : null,
					title: body.title.trim(),
					content: body.content,
					version: 1,
					updated_by_name: 'Ada',
					updated_at: now
				};
				pages.unshift(created);
				return route.fulfill({ status: 201, json: created });
			}
			const stored = pages.find((p) => p.id === pageId && inScope(p));
			if (!stored) return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Wiki page not found' } } });
			if (method === 'GET') return route.fulfill({ json: stored });
			if (method === 'PATCH') {
				if (conflict || body.version !== stored.version)
					return route.fulfill({ status: 409, json: { error: { code: 'WIKI_VERSION_CONFLICT', message: 'changed' } } });
				Object.assign(stored, { title: body.title ?? stored.title, content: body.content ?? stored.content });
				stored.version++;
				return route.fulfill({ json: stored });
			}
		}

		if (path.endsWith('/statuses'))
			return route.fulfill({
				json: [{ id: statusId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', position: 0, is_default: true, project_ids: [] }]
			});
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.endsWith('/github/status')) return route.fulfill({ json: { configured: false, repos: [] } });
		if (path.endsWith('/dev-machine-scope-setting')) return route.fulfill({ json: {} });
		if (
			['labels', 'members', 'views', 'cycles', 'dev-machine-environments', 'issue-templates', 'templates'].some((p) =>
				path.endsWith('/' + p)
			)
		)
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return { errors, writes, pages };
}

async function typeInEditor(page: Page, text: string) {
	const editor = page.locator('form .tiptap[contenteditable="true"]');
	await editor.click();
	await page.keyboard.type(text);
}

test('a team wiki starts empty, creates a page and reopens it with content preserved', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`/test/teams/${teamId}`);
	await page.getByTestId('wiki-entry').click();
	await expect(page).toHaveURL(teamWikiPath);

	await expect(page.getByText('No pages yet')).toBeVisible();
	await page.getByRole('button', { name: 'Create the first page' }).click();
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByText('Give the page a title.')).toBeVisible();
	expect(state.writes).toEqual([]);

	await page.getByLabel('Page title').fill('Onboarding');
	await typeInEditor(page, 'Run make dev to start.');
	await page.getByRole('button', { name: 'Save', exact: true }).click();

	await expect(page.getByRole('heading', { name: 'Onboarding', level: 1 })).toBeVisible();
	await expect(page).toHaveURL(new RegExp(`${teamWikiPath}\\?page=`));
	expect(state.writes).toHaveLength(1);
	expect(state.writes[0].body.title).toBe('Onboarding');
	expect(state.writes[0].body.content).toContain('Run make dev to start.');

	await page.reload();
	await expect(page.getByRole('heading', { name: 'Onboarding', level: 1 })).toBeVisible();
	await expect(page.getByText('Run make dev to start.')).toBeVisible();
	await expect(page.getByRole('navigation', { name: 'Pages' }).getByRole('link', { name: 'Onboarding' })).toHaveAttribute(
		'aria-current',
		'page'
	);
	expect(state.errors).toEqual([]);
});

test('unsaved edits ask before navigating away and survive "keep editing"', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(teamWikiPath);
	await page.getByRole('button', { name: 'Create the first page' }).click();
	await page.getByLabel('Page title').fill('Runbook');
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByRole('heading', { name: 'Runbook', level: 1 })).toBeVisible();
	const pageUrl = page.url();

	await page.getByRole('button', { name: 'Edit', exact: true }).click();
	await typeInEditor(page, 'Draft that is not saved');
	await expect(page.getByText('Unsaved changes')).toBeVisible();

	// The sidebar link to the team's views would leave the wiki.
	await page.getByRole('link', { name: 'Views', exact: true }).click();
	await expect(page.getByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeVisible();
	await page.getByRole('button', { name: 'Keep editing' }).click();
	await expect(page).toHaveURL(pageUrl);
	await expect(page.getByText('Draft that is not saved')).toBeVisible();

	await page.getByRole('button', { name: 'Cancel', exact: true }).click();
	await page.getByTestId('wiki-discard').click();
	await expect(page.getByRole('alertdialog')).toHaveCount(0);
	await expect(page.getByRole('heading', { name: 'Runbook', level: 1 })).toBeVisible();
	await expect(page.getByText('Draft that is not saved')).toHaveCount(0);

	// Without changes, leaving needs no confirmation.
	await page.getByRole('button', { name: 'Edit', exact: true }).click();
	await page.getByRole('link', { name: 'Views', exact: true }).click();
	await expect(page).toHaveURL(`/test/teams/${teamId}/views`);
	expect(state.writes.filter((w) => w.method === 'PATCH')).toEqual([]);
	expect(state.errors).toEqual([]);
});

test('a save that conflicts with someone else keeps the draft and offers the latest version', async ({ page }) => {
	const state = await mockWorkspace(page, { conflict: true });
	await page.goto(teamWikiPath);
	await page.getByRole('button', { name: 'Create the first page' }).click();
	await page.getByLabel('Page title').fill('Shared notes');
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await page.getByRole('button', { name: 'Edit', exact: true }).click();
	await typeInEditor(page, 'My edit');
	await page.getByRole('button', { name: 'Save', exact: true }).click();

	await expect(page.getByTestId('wiki-conflict')).toBeVisible();
	await expect(page.getByText('My edit')).toBeVisible();
	await page.getByRole('button', { name: 'Load latest version' }).click();
	await expect(page.getByRole('heading', { name: 'Shared notes', level: 1 })).toBeVisible();
	expect(state.errors).toEqual([]);
});

test('the project wiki is reachable from the project and is read-only for guests', async ({ page }) => {
	const state = await mockWorkspace(page, { role: 'guest' });
	await page.goto(`/test/projects/${projectId}`);
	await page.getByTestId('wiki-entry').click();
	await expect(page).toHaveURL(projectWikiPath);
	await expect(page.getByText('Nobody has written a page here yet.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Create the first page' })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'New page' })).toHaveCount(0);
	expect(state.writes).toEqual([]);
	expect(state.errors).toEqual([]);
});

test('a wiki the API denies shows that it is unavailable', async ({ page }) => {
	const state = await mockWorkspace(page, { wikiStatus: 404 });
	await page.goto(teamWikiPath);
	await expect(page.getByText('This wiki is not available.', { exact: false })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Create the first page' })).toHaveCount(0);
	expect(state.errors).toEqual([]);
});
