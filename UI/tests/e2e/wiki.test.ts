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
	created_at: string;
	created_by: string | null;
	registry_date: string | null;
	parent_id?: string | null;
	position?: number;
	kind?: 'page' | 'folder';
};

// A workspace with one team and one project whose wiki API keeps pages in
// memory, so a reload reads back what was saved.
async function mockWorkspace(
	page: Page,
	options: {
		role?: string;
		wikiStatus?: number;
		conflict?: boolean;
		seed?: Partial<StoredPage>[];
		versions?: unknown[];
	} = {}
) {
	const { role = 'member', wikiStatus, conflict = false, seed = [], versions = [] } = options;
	await page.routeWebSocket('**/ws', () => {});
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	const writes: { method: string; path: string; body: any }[] = [];
	const blank = {
		team_id: teamId,
		project_id: null,
		content: '',
		version: 1,
		updated_by_name: 'Ada',
		updated_at: now,
		created_at: now,
		created_by: 'user',
		registry_date: null
	};
	const pages: StoredPage[] = seed.map((p, i) => ({
		...blank,
		id: `00000000-0000-0000-0000-${String(800 + i).padStart(12, '0')}`,
		title: `Page ${i}`,
		...p
	}));
	const settings = { registry_enabled: false };
	let publication: { mode: string; children: boolean; url: string | null } = {
		mode: 'off',
		children: false,
		url: null
	};
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

		const wiki = path.match(
			/^\/api\/workspaces\/test\/(teams|projects)\/([^/]+)\/wiki(?:\/([^/]+))?(?:\/(versions|rework|overviews|move|publish))?$/
		);
		if (wiki) {
			if (wikiStatus)
				return route.fulfill({
					status: wikiStatus,
					json: { error: { code: 'NOT_FOUND', message: 'Wiki page not found' } }
				});
			const [, kind, scopeId, pageId, sub] = wiki;
			if (pageId === 'settings') {
				if (method === 'PATCH') {
					writes.push({ method, path, body: request.postDataJSON() });
					settings.registry_enabled = request.postDataJSON().registry_enabled;
				}
				return route.fulfill({ json: settings });
			}
			if (sub === 'versions') return route.fulfill({ json: versions });
			if (pageId === 'registry' && sub === 'overviews')
				return route.fulfill({ json: { overviews: [], next_before: null } });
			if (pageId === 'registry')
				return route.fulfill({
					json: {
						days: pages
							.filter((p) => p.registry_date)
							.sort((a, b) => (b.registry_date ?? '').localeCompare(a.registry_date ?? ''))
							.map((p) => ({
								date: p.registry_date,
								page_id: p.id,
								title: p.title,
								content: p.content,
								version: p.version,
								updated_at: now
							})),
						next_before: null
					}
				});
			const inScope = (p: StoredPage) => (kind === 'teams' ? p.team_id : p.project_id) === scopeId;
			const body = request.postData() ? request.postDataJSON() : null;
			if (method !== 'GET') writes.push({ method, path, body });
			if (!pageId && method === 'GET')
				return route.fulfill({ json: pages.filter(inScope).map(({ content: _content, ...summary }) => summary) });
			const treeOf = () =>
				pages
					.filter((p) => inScope(p) && !p.registry_date)
					.map((p, index) => ({
						id: p.id,
						parent_id: p.parent_id ?? null,
						position: p.position ?? index,
						kind: p.kind ?? 'page'
					}));
			if (pageId === 'tree' && method === 'GET') return route.fulfill({ json: treeOf() });
			if (sub === 'publish') {
				if (method === 'PUT')
					publication = {
						...body,
						url: body.mode === 'public' ? `${new URL(request.url()).origin}/share/wiki/tok123` : null
					};
				return route.fulfill({ json: publication });
			}
			if (sub === 'move') {
				const moved = pages.find((p) => p.id === pageId)!;
				moved.parent_id = body.parent_id;
				moved.position = body.index;
				return route.fulfill({ json: treeOf() });
			}
			if ((!pageId || pageId === 'tree') && method === 'POST') {
				const created: StoredPage = {
					id: `00000000-0000-0000-0000-${String(900 + nextId++).padStart(12, '0')}`,
					team_id: kind === 'teams' ? scopeId : null,
					project_id: kind === 'projects' ? scopeId : null,
					title: body.title.trim(),
					content: body.content,
					version: 1,
					updated_by_name: 'Ada',
					updated_at: now,
					created_at: now,
					created_by: 'user',
					registry_date: null,
					parent_id: body.parent_id ?? null,
					position: pages.length,
					kind: body.kind ?? 'page'
				};
				pages.unshift(created);
				return route.fulfill({ status: 201, json: created });
			}
			const stored = pages.find((p) => p.id === pageId && inScope(p));
			if (!stored)
				return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND', message: 'Wiki page not found' } } });
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
				json: [
					{
						id: statusId,
						team_id: teamId,
						name: 'Todo',
						slug: 'todo',
						category: 'unstarted',
						position: 0,
						is_default: true,
						project_ids: []
					}
				]
			});
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/members')
			return route.fulfill({ json: [{ user_id: 'user', name: 'Ada', email: 'ada@example.com', role }] });
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
	return { errors, writes, pages, settings };
}

function editor(page: Page) {
	return page.getByTestId('wiki-editor').locator('.tiptap[contenteditable="true"]');
}

const patches = (writes: { method: string; body: any }[]) => writes.filter((w) => w.method === 'PATCH');

test('a new page is named in place and its content autosaves', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`/test/teams/${teamId}`);
	await page.getByTestId('wiki-entry').click();
	await expect(page).toHaveURL(teamWikiPath);

	await expect(page.getByText('No pages yet')).toBeVisible();
	await page.getByRole('button', { name: 'Create the first page' }).click();
	await expect(page).toHaveURL(new RegExp(`${teamWikiPath}\\?page=`));
	expect(state.writes[0]).toMatchObject({ method: 'POST', body: { title: 'Untitled' } });

	const title = page.getByLabel('Page title');
	await expect(title).toBeFocused();
	await title.fill('Onboarding');
	await title.press('Enter');
	await expect(page.getByRole('heading', { name: 'Onboarding', level: 1 })).toBeVisible();
	await expect.poll(() => patches(state.writes).length).toBe(1);
	expect(patches(state.writes)[0].body).toEqual({ title: 'Onboarding', version: 1 });

	await editor(page).click();
	await page.keyboard.type('Run make dev to start.');
	await expect(page.getByTestId('wiki-save-state')).toHaveText('Unsaved');
	// Like an issue description, the save follows a pause in typing.
	await expect.poll(() => patches(state.writes).length, { timeout: 5000 }).toBe(2);
	expect(patches(state.writes)[1].body).toMatchObject({ version: 2 });
	expect(patches(state.writes)[1].body.content).toContain('Run make dev to start.');
	await expect(page.getByTestId('wiki-save-state')).toHaveText('Saved');

	await page.reload();
	await expect(page.getByRole('heading', { name: 'Onboarding', level: 1 })).toBeVisible();
	await expect(page.getByText('Run make dev to start.')).toBeVisible();
	await expect(
		page.getByRole('navigation', { name: 'Pages' }).getByRole('link', { name: 'Onboarding' })
	).toHaveAttribute('aria-current', 'page');
	expect(state.errors).toEqual([]);
});

test('the side panel shows details, an outline that jumps to headings, and embedded files', async ({ page }) => {
	const content =
		'<h2>Setup</h2><p>One</p><h3>Database</h3><p>Two</p>' +
		'<p><a class="editor-attachment" data-type="attachment" data-filename="schema.sql" data-size="2048" href="/api/assets/schema">schema.sql</a></p>';
	const state = await mockWorkspace(page, { seed: [{ title: 'Runbook', content }] });
	await page.goto(teamWikiPath);
	const details = page.getByRole('complementary', { name: 'Details' });
	await expect(details.getByText('Engineering')).toBeVisible();
	await expect(details.getByText('Created by')).toBeVisible();
	const outline = details.getByRole('navigation', { name: 'Outline' });
	await expect(outline.getByRole('button')).toHaveText(['Setup', 'Database']);
	await outline.getByRole('button', { name: 'Database' }).click();
	await expect(page.getByTestId('wiki-editor').getByRole('heading', { name: 'Database' })).toBeInViewport();
	await expect(details.getByRole('link', { name: /schema\.sql/ })).toHaveAttribute('href', '/api/assets/schema');
	expect(patches(state.writes)).toEqual([]);
	expect(state.errors).toEqual([]);
});

test('a save that conflicts with someone else keeps the text and offers the latest version', async ({ page }) => {
	const state = await mockWorkspace(page, {
		conflict: true,
		seed: [{ title: 'Shared notes', content: '<p>Base</p>' }]
	});
	await page.goto(teamWikiPath);
	await editor(page).click();
	await page.keyboard.press('End');
	await page.keyboard.type(' my edit');
	await expect(page.getByTestId('wiki-conflict')).toBeVisible({ timeout: 5000 });
	await expect(page.getByText('Base my edit')).toBeVisible();
	await page.getByRole('button', { name: 'Load latest version' }).click();
	await expect(page.getByTestId('wiki-conflict')).toHaveCount(0);
	await expect(page.getByText('Base my edit')).toHaveCount(0);
	await expect(page.getByText('Base', { exact: true })).toBeVisible();
	expect(patches(state.writes)).toHaveLength(1);
	expect(state.errors).toEqual([]);
});

test('history shows what changed and restores an earlier version', async ({ page }) => {
	const versions = [
		{
			id: 'r2',
			version: 3,
			old_title: 'Plan',
			new_title: 'Plan',
			old_content: '<p>Keep</p><p>Old step</p>',
			new_content: '<p>Keep</p><p>New step</p>',
			edited_by: 'user',
			edited_by_name: 'Ada',
			created_at: now,
			updated_at: now
		},
		{
			id: 'r1',
			version: 2,
			old_title: 'Draft',
			new_title: 'Plan',
			old_content: '<p>Keep</p><p>Old step</p>',
			new_content: '<p>Keep</p><p>Old step</p>',
			edited_by: null,
			edited_by_name: null,
			created_at: now,
			updated_at: now
		}
	];
	const state = await mockWorkspace(page, {
		seed: [{ title: 'Plan', content: '<p>Keep</p><p>New step</p>', version: 3 }],
		versions
	});
	await page.goto(teamWikiPath);
	await page.getByRole('button', { name: 'Version history' }).click();
	const dialog = page.getByRole('dialog', { name: 'Plan history' });
	await expect(dialog.getByText('Ada changed the content')).toBeVisible();
	const diff = dialog.getByTestId('text-diff');
	await expect(diff.getByRole('group', { name: 'Removed' })).toContainText('- Old step');
	await expect(diff.getByRole('group', { name: 'Added' })).toContainText('+ New step');
	await dialog.getByRole('checkbox', { name: 'Show changes' }).uncheck();
	await dialog.getByRole('region', { name: 'Before' }).getByRole('button', { name: 'Restore' }).click();
	await expect(dialog).toHaveCount(0);
	expect(patches(state.writes).at(-1)?.body).toEqual({ content: '<p>Keep</p><p>Old step</p>', version: 3 });
	await expect(page.getByText('Old step')).toBeVisible();
	expect(state.errors).toEqual([]);
});

test('the registry is one pinned entry that opens a read-only daily feed', async ({ page }) => {
	const state = await mockWorkspace(page, {
		seed: [
			{ title: 'Handbook' },
			{
				title: 'Registry 2026-01-02',
				registry_date: '2026-01-02',
				content: '<p><strong>09:05</strong> KUA-20: opened PR #161</p><p><strong>10:00</strong> Deployed</p>'
			},
			{ title: 'Registry 2026-01-01', registry_date: '2026-01-01', content: '<p><strong>08:00</strong> Started</p>' }
		]
	});
	await page.goto(teamWikiPath);
	const nav = page.getByRole('navigation', { name: 'Pages' });
	await expect(page.getByRole('heading', { name: 'Handbook', level: 1 })).toBeVisible();
	// Day pages are not listed among the pages; the registry is off.
	await expect(nav.getByRole('link', { name: /Registry 2026/ })).toHaveCount(0);
	await expect(nav.getByRole('link', { name: 'Registry' })).toHaveCount(0);

	await nav.getByRole('switch', { name: 'Daily registry' }).click();
	await expect(nav.getByRole('switch', { name: 'Daily registry' })).toBeChecked();
	expect(state.writes.at(-1)).toMatchObject({ method: 'PATCH', body: { registry_enabled: true } });

	await nav.getByRole('link', { name: 'Registry' }).click();
	const feed = page.getByTestId('registry-feed');
	await expect(feed.locator('ol h2')).toHaveCount(2);
	await expect(feed.getByText('2 entries')).toBeVisible();
	await expect(feed.locator('[data-type="mention"][data-identifier="KUA-20"]')).toBeVisible();
	await expect(feed.getByText('PR #161')).toBeVisible();
	await expect(feed.locator('[contenteditable="true"]')).toHaveCount(0);

	await feed.getByRole('searchbox', { name: 'Filter entries' }).fill('deployed');
	await expect(feed.locator('ol h2')).toHaveCount(1);
	await expect(feed.getByText('Started')).toHaveCount(0);
	expect(state.errors).toEqual([]);
});

test('folders hold pages, and pages move into them from the row menu', async ({ page }) => {
	const state = await mockWorkspace(page, { seed: [{ title: 'Handbook' }] });
	await page.goto(teamWikiPath);
	const tree = page.getByRole('tree', { name: 'Pages' });
	await expect(tree.getByRole('link', { name: 'Handbook' })).toBeVisible();

	await page.getByRole('button', { name: 'New folder' }).click();
	await expect.poll(() => state.writes.at(-1)?.body?.kind).toBe('folder');
	expect(state.writes.at(-1)?.body).toMatchObject({ kind: 'folder', parent_id: null });
	const folderId = state.pages[0].id;
	await expect(tree.getByRole('link', { name: 'New folder' })).toBeVisible();

	await tree.getByRole('button', { name: 'Actions for Handbook' }).click();
	await page.getByRole('menuitem', { name: 'Move to' }).click();
	await page.getByRole('menuitem', { name: 'New folder' }).click();
	await expect.poll(() => state.writes.at(-1)?.path).toContain('/move');
	expect(state.writes.at(-1)?.body).toEqual({ parent_id: folderId, index: 0 });
	await expect(tree.locator(`[data-page="${folderId}"]`)).toHaveAttribute('aria-expanded', 'true');
	await expect(tree.locator('[aria-level="2"]')).toContainText('Handbook');
	expect(state.errors).toEqual([]);
});

test('a page published publicly opens read-only from its link without signing in', async ({ page, browser }) => {
	const state = await mockWorkspace(page, { seed: [{ title: 'Handbook', content: '<p>Welcome</p>' }] });
	await page.goto(teamWikiPath);
	await page.getByRole('button', { name: 'Publish' }).click();
	const dialog = page.getByRole('dialog', { name: 'Publish page' });
	await dialog.getByRole('radio', { name: /^Public/ }).click();
	await dialog.getByRole('checkbox', { name: 'Include pages inside it' }).check();
	await dialog.getByRole('button', { name: 'Save' }).click();
	await expect(dialog.getByLabel('Public link')).toHaveValue(/\/share\/wiki\/tok123$/);
	expect(state.writes.at(-1)).toMatchObject({ method: 'PUT', body: { mode: 'public', children: true } });

	// A fresh browser with no session: only the public endpoint answers.
	const anonymous = await browser.newContext();
	const reader = await anonymous.newPage();
	const errors: string[] = [];
	reader.on('pageerror', (error) => errors.push(error.message));
	await reader.route('**/api/**', (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/public/wiki/tok123')
			return route.fulfill({
				json: {
					workspace_name: 'Test',
					root_id: 'root',
					pages: [
						{
							id: 'root',
							parent_id: null,
							title: 'Handbook',
							content: '<h2>Start</h2><p>Welcome</p>',
							position: 0,
							updated_at: now
						},
						{
							id: 'child',
							parent_id: 'root',
							title: 'Setup',
							content: '<p>Run make dev</p>',
							position: 0,
							updated_at: now
						}
					]
				}
			});
		if (path.startsWith('/api/public/wiki/'))
			return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
		return route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED' } } });
	});
	await reader.goto('/share/wiki/tok123');
	await expect(reader.getByRole('heading', { name: 'Handbook', level: 1 })).toBeVisible();
	await expect(reader.getByText('Welcome')).toBeVisible();
	await expect(reader.locator('[contenteditable="true"]')).toHaveCount(0);
	await expect(
		reader.getByRole('complementary', { name: 'Outline' }).getByRole('button', { name: 'Start' })
	).toBeVisible();
	await reader.getByRole('navigation', { name: 'Pages' }).getByRole('link', { name: 'Setup' }).click();
	await expect(reader.getByRole('heading', { name: 'Setup', level: 1 })).toBeVisible();
	await expect(reader.getByText('Run make dev')).toBeVisible();

	await reader.goto('/share/wiki/revoked');
	await expect(reader.getByText('This page is not available')).toBeVisible();
	expect(errors).toEqual([]);
	await anonymous.close();
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

test('guests read pages without editing controls', async ({ page }) => {
	const state = await mockWorkspace(page, { role: 'guest', seed: [{ title: 'Read me', content: '<p>Hello</p>' }] });
	await page.goto(teamWikiPath);
	await expect(page.getByRole('heading', { name: 'Read me', level: 1 })).toBeVisible();
	await expect(page.getByTestId('wiki-editor').locator('[contenteditable="true"]')).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Page actions' })).toHaveCount(0);
	await expect(page.getByRole('switch', { name: 'Daily registry' })).toHaveCount(0);
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
