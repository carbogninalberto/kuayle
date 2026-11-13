import { expect, test, type Locator, type Page } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const projectId = '00000000-0000-0000-0000-000000000020';
const statusId = '00000000-0000-0000-0000-000000000030';
const cycleId = '00000000-0000-0000-0000-000000000040';
const cyclePath = `/test/teams/${teamId}/cycles`;
const now = '2026-01-01T00:00:00Z';

async function mockWorkspace(
	page: Page,
	role = 'owner',
	icon = 'layers',
	projectLook: { color: string | null; icon: string | null } = { color: null, icon: null }
) {
	await page.routeWebSocket('**/ws', () => {});
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	const mutations: { method: string; path: string; body: any }[] = [];
	const queries: URLSearchParams[] = [];
	const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon, created_at: now };
	const project = {
		id: projectId,
		name: 'Launch',
		team_id: teamId,
		status: 'planned',
		...projectLook,
		created_at: now
	};
	const cycles = Array.from({ length: 7 }, (_, i) => ({
		id: i === 0 ? cycleId : `00000000-0000-0000-0000-${String(40 + i).padStart(12, '0')}`,
		team_id: teamId,
		number: i + 1,
		name: i === 0 ? 'Current cycle' : `Older cycle ${i}`,
		status: i === 0 ? 'active' : 'completed',
		start_date: null,
		end_date: null,
		completed_at: i ? `2026-01-${String(20 - i).padStart(2, '0')}T00:00:00Z` : null,
		progress: { total: 0, completed: 0, cancelled: 0 },
		created_at: now
	}));
	const issues = Array.from({ length: 65 }, (_, i) => ({
		id: `00000000-0000-0000-0000-${String(100 + i).padStart(12, '0')}`,
		identifier: `ENG-${i + 1}`,
		title: i === 64 ? 'Needle beyond fifty' : `Scoped issue ${i + 1}`,
		team_id: teamId,
		status_id: statusId,
		status: 'todo',
		priority: 3,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		cycle_id: i === 0 ? cycleId : null,
		project_id: i === 0 ? projectId : null,
		labels: [],
		assignees: [],
		sort_order: i * 1000,
		created_at: now,
		updated_at: now
	}));
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (['PATCH', 'POST', 'DELETE'].includes(request.method())) {
			mutations.push({ method: request.method(), path, body: request.postData() ? request.postDataJSON() : null });
		}
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user', name: 'Owner', email: 'owner@example.com' } });
		if (path === '/api/preferences') return route.fulfill({ json: { issues_group_by: 'status', theme_mode: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'workspace', slug: 'test', name: 'Test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: 'workspace', slug: 'test', name: 'Test', current_user_role: role } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === `/api/workspaces/test/teams/${teamId}`) {
			if (request.method() === 'PATCH') Object.assign(team, request.postDataJSON());
			return route.fulfill({ json: team });
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
		if (path === '/api/workspaces/test/projects' || path === `/api/workspaces/test/teams/${teamId}/projects`)
			return route.fulfill({ json: [project] });
		if (path === `/api/workspaces/test/projects/${projectId}`) {
			if (request.method() === 'PATCH') Object.assign(project, request.postDataJSON());
			return route.fulfill({
				json: {
					...project,
					progress: { total: issues.filter((i) => i.project_id === projectId).length, completed: 0, cancelled: 0 }
				}
			});
		}
		if (path === `/api/workspaces/test/teams/${teamId}/cycles`) return route.fulfill({ json: cycles });
		if (path === `/api/workspaces/test/teams/${teamId}/cycles/velocity`) return route.fulfill({ json: [] });
		if (path.startsWith(`/api/workspaces/test/teams/${teamId}/cycles/`)) {
			const cycle = cycles.find((c) => c.id === path.split('/').at(-1));
			return route.fulfill({
				json: {
					...cycle,
					progress: { total: issues.filter((i) => i.cycle_id === cycle?.id).length, completed: 0, cancelled: 0 }
				}
			});
		}
		if (path === '/api/workspaces/test/issues') {
			if (request.method() === 'POST') {
				const created = { ...issues[0], ...request.postDataJSON(), id: 'created', identifier: 'ENG-66' };
				issues.push(created);
				return route.fulfill({ json: created });
			}
			queries.push(url.searchParams);
			const search = url.searchParams.get('search')?.toLowerCase() ?? '';
			const filtered = issues.filter(
				(i) =>
					(!search || `${i.title} ${i.identifier}`.toLowerCase().includes(search)) &&
					(!url.searchParams.get('cycle') || i.cycle_id === url.searchParams.get('cycle')) &&
					(!url.searchParams.get('project') || i.project_id === url.searchParams.get('project'))
			);
			const pageNumber = Number(url.searchParams.get('page') ?? 1);
			const size = Number(url.searchParams.get('per_page') ?? 50);
			return route.fulfill({
				json: {
					data: filtered.slice((pageNumber - 1) * size, pageNumber * size),
					total_count: filtered.length,
					page: pageNumber,
					has_more: filtered.length > pageNumber * size
				}
			});
		}
		if (path === '/api/workspaces/test/issues/bulk' && request.method() === 'PATCH')
			// Fewer than selected: some issues are already in the project.
			return route.fulfill({ json: { updated: 3 } });
		const issue = issues.find((i) => path === `/api/workspaces/test/issues/${i.identifier}`);
		if (issue) {
			if (request.method() === 'PATCH') Object.assign(issue, request.postDataJSON());
			return route.fulfill({ json: issue });
		}
		if (path.endsWith('/github/status')) return route.fulfill({ json: { configured: false, repos: [] } });
		if (path.endsWith('/dev-machine-scope-setting')) return route.fulfill({ json: {} });
		if (
			['labels', 'members', 'views', 'dev-machine-environments', 'issue-templates', 'templates'].some((p) =>
				path.endsWith('/' + p)
			)
		)
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return { errors, mutations, queries };
}

test('cycle row menus open by mouse and keyboard without navigating, including archived cycles', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	await page.getByRole('button', { name: 'Actions for Current cycle' }).click();
	await expect(page.getByRole('button', { name: 'Edit cycle', exact: true })).toBeVisible();
	await expect(page).toHaveURL(cyclePath);
	await page.keyboard.press('Escape');
	await page.getByRole('button', { name: 'Actions for Current cycle' }).focus();
	await page.keyboard.press('Enter');
	await page.getByRole('button', { name: 'Delete cycle', exact: true }).click();
	await page.getByRole('button', { name: 'Cancel', exact: true }).click();
	expect(state.mutations).toEqual([]);
	await page.getByRole('button', { name: '1 older cycle(s) (archived)', exact: true }).click();
	await page.getByRole('button', { name: 'Actions for Older cycle 6' }).click();
	await expect(page.getByRole('button', { name: 'Edit cycle', exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Current cycle', exact: true }).locator('button')).toHaveCount(0);
	expect(state.errors).toEqual([]);
});

test('guests can navigate cycles but have no empty action menu or create action', async ({ page }) => {
	const state = await mockWorkspace(page, 'guest');
	await page.goto(cyclePath);
	await expect(page.getByRole('link', { name: 'Current cycle', exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: /Actions for/ })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'New cycle', exact: true })).toHaveCount(0);
	expect(state.errors).toEqual([]);
});

for (const kind of ['cycle', 'project'] as const) {
	const path = kind === 'cycle' ? `${cyclePath}/${cycleId}` : `/test/projects/${projectId}`;
	const scopeId = kind === 'cycle' ? cycleId : projectId;
	test(`${kind}: paginated multi-select server search, add and remove preserve scope`, async ({ page }) => {
		const state = await mockWorkspace(page);
		await page.goto(path);
		const scoped = page.getByTestId('scoped-issues');
		await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
		await expect(scoped.getByRole('radio', { name: 'Board view', exact: true })).toBeVisible();
		await scoped.getByRole('button', { name: 'Add issues', exact: true }).click();
		const picker = page.getByRole('dialog', { name: 'Add issues', exact: true });
		await expect(picker.getByRole('option').filter({ has: page.getByText('ENG-1', { exact: true }) })).toHaveCount(0);
		await picker.getByRole('button', { name: 'Load more', exact: true }).click();
		await expect(picker.getByRole('option').filter({ has: page.getByText('ENG-21', { exact: true }) })).toBeVisible();
		await picker.getByRole('combobox').fill('Needle');
		await expect(picker.getByRole('option', { name: /Needle beyond fifty/ })).toBeVisible();
		await picker.getByRole('combobox').press('Enter');
		await picker.getByRole('combobox').fill('Scoped issue 2');
		await picker
			.getByRole('option')
			.filter({ has: page.getByText('ENG-2', { exact: true }) })
			.click();
		await picker.getByRole('button', { name: 'Add issues (2)', exact: true }).click();
		await expect(picker).not.toBeVisible();
		await expect(scoped.getByText('Needle beyond fifty', { exact: true })).toBeVisible();
		const assignments = state.mutations.filter((m) => m.method === 'PATCH' && m.path.includes('/issues/'));
		expect(assignments).toHaveLength(2);
		for (const assignment of assignments) expect(assignment.body).toEqual({ [`${kind}_id`]: scopeId });
		expect(state.queries.some((query) => query.get('search') === 'Needle' && query.get('team') === teamId)).toBe(true);
		await scoped.getByText('Needle beyond fifty', { exact: true }).click({ button: 'right' });
		await page.getByRole('menuitem', { name: `Remove from ${kind}`, exact: true }).click();
		await expect(scoped.getByText('Needle beyond fifty', { exact: true })).toHaveCount(0);
		expect(state.mutations.at(-1)?.body).toEqual({ [`${kind}_id`]: '' });
		expect(state.mutations.some((m) => m.method === 'DELETE')).toBe(false);
		expect(state.errors).toEqual([]);
	});

	test(`${kind}: toolbar creation is prefilled with the scope`, async ({ page }) => {
		const state = await mockWorkspace(page);
		await page.goto(path);
		// Group headers also expose an icon-only "New issue" quick-add; target the labelled toolbar button.
		await page.getByTestId('scoped-issues').locator('button', { hasText: 'New issue' }).click();
		const dialog = page.getByRole('dialog');
		await dialog.getByPlaceholder('Issue title').fill(`Created in ${kind}`);
		await dialog.getByRole('button', { name: 'Create issue', exact: true }).click();
		await expect
			.poll(() => state.mutations.filter((m) => m.method === 'POST' && m.path.endsWith('/issues')).length)
			.toBe(1);
		expect(state.mutations.find((m) => m.method === 'POST' && m.path.endsWith('/issues'))?.body).toMatchObject({
			team_id: teamId,
			[`${kind}_id`]: scopeId
		});
		expect(state.errors).toEqual([]);
	});

	test(`${kind}: guest view has no mutation affordances`, async ({ page }) => {
		await mockWorkspace(page, 'guest');
		await page.goto(path);
		const scoped = page.getByTestId('scoped-issues');
		await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
		await expect(scoped.getByRole('button', { name: 'Add issues', exact: true })).toHaveCount(0);
		await expect(scoped.getByRole('button', { name: 'New issue', exact: true })).toHaveCount(0);
		await expect(scoped.getByRole('checkbox')).toHaveCount(0);
		await scoped.getByText('Scoped issue 1', { exact: true }).click({ button: 'right' });
		await expect(page.getByRole('menuitem', { name: `Remove from ${kind}`, exact: true })).toHaveCount(0);
		await expect(page.getByRole('menuitem', { name: 'Delete', exact: true })).toHaveCount(0);
	});
}

test('picker ignores stale searches and retains failed assignments for retry', async ({ page }) => {
	await mockWorkspace(page);
	let release!: () => void;
	let started!: () => void;
	const startedPromise = new Promise<void>((resolve) => (started = resolve));
	const releasePromise = new Promise<void>((resolve) => (release = resolve));
	await page.route('**/api/workspaces/test/issues?*', async (route) => {
		if (new URL(route.request().url()).searchParams.get('search') === 'Scoped') {
			started();
			await releasePromise;
		}
		await route.fallback();
	});
	let failOnce = true;
	await page.route('**/api/workspaces/test/issues/ENG-65', async (route) => {
		if (route.request().method() === 'PATCH' && failOnce) {
			failOnce = false;
			return route.fulfill({ status: 500, json: { error: { message: 'Temporary failure' } } });
		}
		return route.fallback();
	});
	await page.goto(`${cyclePath}/${cycleId}`);
	await page.getByTestId('scoped-issues').getByRole('button', { name: 'Add issues', exact: true }).click();
	const picker = page.getByRole('dialog', { name: 'Add issues', exact: true });
	await picker.getByRole('combobox').fill('Scoped');
	await startedPromise;
	await picker.getByRole('combobox').fill('Needle');
	await expect(picker.getByRole('option', { name: /Needle beyond fifty/ })).toBeVisible();
	const oldResponse = page.waitForResponse(
		(response) => new URL(response.url()).searchParams.get('search') === 'Scoped'
	);
	release();
	await oldResponse;
	await expect(picker.getByRole('option')).toHaveCount(1);
	await picker.getByRole('option', { name: /Needle beyond fifty/ }).click();
	await picker.getByRole('button', { name: 'Add issues (1)', exact: true }).click();
	await expect(picker.getByRole('alert')).toContainText('1 issues could not be updated');
	await expect(picker.getByRole('option', { selected: true })).toHaveCount(1);
	await picker.getByRole('button', { name: 'Add issues (1)', exact: true }).click();
	await expect(picker).not.toBeVisible();
	await expect(page.getByTestId('scoped-issues').getByText('Needle beyond fifty', { exact: true })).toBeVisible();
});

test('project Gantt returns directly to list and deletion requires confirmation', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`/test/projects/${projectId}`);
	const scoped = page.getByTestId('scoped-issues');
	await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
	// Browser tab title: the project first, then the team, then Kuayle.
	await expect(page).toHaveTitle('Launch < Engineering < Kuayle');
	await scoped.getByRole('button', { name: 'Gantt view', exact: true }).click();
	await expect(scoped.getByRole('radio', { name: 'List view', exact: true })).not.toBeChecked();
	await scoped.getByRole('radio', { name: 'List view', exact: true }).click();
	await expect(scoped.getByRole('radio', { name: 'List view', exact: true })).toBeChecked();
	await page.getByRole('button', { name: 'Project actions', exact: true }).click();
	await page.getByRole('button', { name: 'Delete project', exact: true }).click();
	await expect(page.getByRole('alertdialog')).toBeVisible();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel', exact: true }).click();
	expect(state.mutations.some((m) => m.method === 'DELETE')).toBe(false);
	expect(state.errors).toEqual([]);
});

test('mobile scoped pages fit the viewport and project indexes hide guest creation', async ({ page }, testInfo) => {
	await mockWorkspace(page, 'guest');
	await page.setViewportSize({ width: 390, height: 844 });
	for (const path of [`${cyclePath}/${cycleId}`, `/test/projects/${projectId}`]) {
		await page.goto(path);
		await expect(page.getByTestId('scoped-issues').getByText('Scoped issue 1', { exact: true })).toBeVisible();
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
		await page.screenshot({
			path: testInfo.outputPath(path.includes('/cycles/') ? 'cycle-mobile.png' : 'project-mobile.png')
		});
	}
	for (const path of ['/test/projects', `/test/teams/${teamId}/projects`]) {
		await page.goto(path);
		await expect(page.getByRole('link', { name: /Launch/ }).last()).toBeVisible();
		await expect(page.getByRole('button', { name: 'New project', exact: true })).toHaveCount(0);
	}
});

test('project colour and icon show on the project page and only on issues that have a project', async ({ page }) => {
	const state = await mockWorkspace(page, 'owner', 'layers', { color: '#ef4444', icon: 'Rocket' });
	await page.goto(`/test/projects/${projectId}`);
	const scoped = page.getByTestId('scoped-issues');
	await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
	await expect(page.getByTestId('project-appearance-trigger').locator('svg')).toHaveCSS('color', 'rgb(239, 68, 68)');
	await expect(scoped.getByTestId('project-chip')).toHaveCount(1);
	await expect(scoped.getByTestId('project-chip')).toHaveAttribute('title', 'Launch');
	expect(state.errors).toEqual([]);
});

test('a project manager picks an icon and a colour, and can reset them', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`/test/projects/${projectId}`);
	await page.getByTestId('project-appearance-trigger').click();
	await page.getByPlaceholder('Search Lucide icons', { exact: true }).fill('Rocket');
	await page.getByRole('button', { name: 'Use Rocket icon', exact: true }).click();
	await expect.poll(() => state.mutations.some((m) => m.method === 'PATCH' && m.body.icon === 'Rocket')).toBe(true);
	await page.getByTestId('project-appearance-trigger').click();
	await page.getByRole('button', { name: 'Use color #3b82f6', exact: true }).click();
	await expect.poll(() => state.mutations.some((m) => m.method === 'PATCH' && m.body.color === '#3b82f6')).toBe(true);
	await expect(page.getByTestId('project-appearance-trigger').locator('svg')).toHaveCSS('color', 'rgb(59, 130, 246)');
	await page.getByRole('button', { name: 'Reset', exact: true }).click();
	await expect.poll(() => state.mutations.some((m) => m.method === 'PATCH' && m.body.icon === '' && m.body.color === '')).toBe(true);
	expect(state.errors).toEqual([]);
});

test('guests see the project icon but cannot change it', async ({ page }) => {
	await mockWorkspace(page, 'guest', 'layers', { color: '#22c55e', icon: 'emoji:🚀' });
	await page.goto(`/test/projects/${projectId}`);
	await expect(page.getByTestId('project-appearance-trigger')).toBeDisabled();
	await expect(page.getByTestId('project-appearance-trigger')).toContainText('🚀');
});

test('existing single-select parent picker still applies one selection immediately', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`${cyclePath}/${cycleId}`);
	await page.getByTestId('scoped-issues').getByText('Scoped issue 1', { exact: true }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Mark as', exact: true }).hover();
	await page.getByRole('menuitem', { name: 'Sub-issue of...', exact: true }).click();
	const picker = page.getByRole('dialog', { name: 'Set parent issue', exact: true });
	await picker.getByRole('combobox').fill('Needle');
	await picker.getByRole('option', { name: /Needle beyond fifty/ }).click();
	await expect(picker).not.toBeVisible();
	expect(state.mutations.find((m) => m.path.endsWith('/issues/ENG-1'))?.body).toEqual({
		parent_id: '00000000-0000-0000-0000-000000000164'
	});
	expect(state.errors).toEqual([]);
});

test('bulk actions set the project of the selected issues and can remove it', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(`${cyclePath}/${cycleId}`);
	const scoped = page.getByTestId('scoped-issues');
	const actions = page.getByRole('button', { name: 'Actions', exact: true });
	const bulkRequests = () => state.mutations.filter((m) => m.path === '/api/workspaces/test/issues/bulk');
	await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
	await page.keyboard.press('Control+a');
	await actions.click();
	await page.getByRole('button', { name: /Set project/ }).click();
	await page.getByRole('button', { name: 'Launch', exact: true }).click();
	await expect.poll(() => bulkRequests().length).toBe(1);
	expect(bulkRequests()[0].body.project_id).toBe(projectId);
	expect(bulkRequests()[0].body.issue_ids.length).toBeGreaterThan(0);
	// The toast reports what the server changed, not the selection size.
	await expect(page.getByText('Moved 3 issues to the project')).toBeVisible();
	// The selection is cleared once the update lands, which hides the bar.
	await expect(actions).toHaveCount(0);

	await page.keyboard.press('Control+a');
	await actions.click();
	await page.getByRole('button', { name: /Set project/ }).click();
	await page.getByRole('button', { name: 'No project', exact: true }).click();
	await expect.poll(() => bulkRequests().length).toBe(2);
	expect(bulkRequests()[1].body.project_id).toBe('');
	expect(state.errors).toEqual([]);
});

test.describe('KUA-65: bulk actions stay clickable over the issue list', () => {
	async function selectAllInCycle(page: Page, viewport?: { width: number; height: number }) {
		const state = await mockWorkspace(page);
		// The stock fixture has one issue in the cycle; fill the list so rows really scroll under the bar.
		await page.route('**/api/workspaces/test/issues?**', (route) => {
			const url = new URL(route.request().url());
			if (route.request().method() !== 'GET' || url.pathname !== '/api/workspaces/test/issues') return route.fallback();
			const issues = Array.from({ length: 40 }, (_, i) => ({
				id: `00000000-0000-0000-0000-${String(300 + i).padStart(12, '0')}`,
				identifier: `ENG-${i + 1}`,
				title: `Scoped issue ${i + 1}`,
				team_id: teamId,
				status_id: statusId,
				status: 'todo',
				priority: 3,
				status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
				cycle_id: cycleId,
				project_id: null,
				labels: [],
				assignees: [],
				sort_order: i * 1000,
				created_at: now,
				updated_at: now
			}));
			return route.fulfill({ json: { data: issues, total_count: issues.length, page: 1, has_more: false } });
		});
		if (viewport) await page.setViewportSize(viewport);
		await page.goto(`${cyclePath}/${cycleId}`);
		const scoped = page.getByTestId('scoped-issues');
		await expect(scoped.getByText('Scoped issue 1', { exact: true })).toBeVisible();
		await page.keyboard.press('Control+a');
		await expect(page.getByRole('button', { name: 'Actions', exact: true })).toBeVisible();
		return { state, scoped };
	}

	async function openIssueMenu(page: Page, scoped: Locator) {
		await scoped.getByText('Scoped issue 1', { exact: true }).click({ button: 'right' });
		await expect(page.getByRole('menu')).toBeVisible();
	}

	async function actionsBox(page: Page) {
		const box = await page.getByRole('button', { name: 'Actions', exact: true }).boundingBox();
		expect(box).not.toBeNull();
		return box!;
	}

	/** The bar is fixed over the list; a row must really sit under it for this regression to mean anything. */
	async function expectRowBeneath(page: Page, x: number, y: number) {
		const labels = await page.evaluate(
			({ x, y }) =>
				document
					.elementsFromPoint(x, y)
					.map((element) => element.getAttribute('aria-label'))
					.filter((label): label is string => !!label),
			{ x, y }
		);
		expect(labels.some((label) => /^ENG-\d+$/.test(label))).toBe(true);
	}

	const bulkSearch = (page: Page) => page.locator('#bulk-actions-search');

	test('Actions opens bulk actions while the issue context menu is open', async ({ page }) => {
		const { state, scoped } = await selectAllInCycle(page);
		await openIssueMenu(page, scoped);
		const box = await actionsBox(page);
		const x = box.x + box.width / 2;
		const y = box.y + box.height / 2;
		await expectRowBeneath(page, x, y);
		const url = page.url();
		await page.mouse.click(x, y);
		await expect(bulkSearch(page)).toBeVisible();
		await expect(bulkSearch(page)).toBeFocused();
		await expect(page).toHaveURL(url);
		expect(new URL(page.url()).pathname).not.toContain('/issue/');
		await expect(page.getByRole('menu')).toBeHidden();
		expect(state.errors).toEqual([]);
	});

	test('edge clicks on the Actions button open bulk actions and bar gaps never open the issue', async ({ page }) => {
		const { state, scoped } = await selectAllInCycle(page);
		const positions = [
			{ x: (box: { x: number }) => box.x + 2, y: (box: { y: number; height: number }) => box.y + box.height / 2 },
			{
				x: (box: { x: number; width: number }) => box.x + box.width - 2,
				y: (box: { y: number; height: number }) => box.y + box.height / 2
			},
			{ x: (box: { x: number; width: number }) => box.x + box.width / 2, y: (box: { y: number }) => box.y + 2 },
			{
				x: (box: { x: number; width: number }) => box.x + box.width / 2,
				y: (box: { y: number; height: number }) => box.y + box.height - 2
			}
		];
		for (const position of positions) {
			await openIssueMenu(page, scoped);
			const box = await actionsBox(page);
			const x = position.x(box);
			const y = position.y(box);
			await expectRowBeneath(page, x, y);
			const url = page.url();
			await page.mouse.click(x, y);
			await expect(bulkSearch(page)).toBeVisible();
			await expect(page).toHaveURL(url);
			await page.keyboard.press('Escape');
			await expect(bulkSearch(page)).toHaveCount(0);
		}
		// The bar itself swallows clicks in its padding instead of passing them to the row below.
		await openIssueMenu(page, scoped);
		const box = await actionsBox(page);
		const url = page.url();
		await page.mouse.click(box.x - 3, box.y + box.height / 2);
		await expect(page).toHaveURL(url);
		expect(new URL(page.url()).pathname).not.toContain('/issue/');
		await expect(bulkSearch(page)).toHaveCount(0);
		await expect(page.getByRole('menu')).toBeHidden();
		expect(state.errors).toEqual([]);
	});

	test('Actions opens bulk actions over the issue list on a small viewport', async ({ page }) => {
		const { state, scoped } = await selectAllInCycle(page, { width: 390, height: 844 });
		await openIssueMenu(page, scoped);
		const box = await actionsBox(page);
		const x = box.x + box.width / 2;
		const y = box.y + box.height / 2;
		await expectRowBeneath(page, x, y);
		const url = page.url();
		await page.mouse.click(x, y);
		await expect(bulkSearch(page)).toBeVisible();
		await expect(page).toHaveURL(url);
		await expect(page.getByRole('menu')).toBeHidden();
		expect(state.errors).toEqual([]);
	});

	test('Actions behaves the same with the issue context menu closed', async ({ page }) => {
		const { state } = await selectAllInCycle(page);
		const box = await actionsBox(page);
		const x = box.x + box.width / 2;
		const y = box.y + box.height / 2;
		await expectRowBeneath(page, x, y);
		const url = page.url();
		await page.mouse.click(x, y);
		await expect(bulkSearch(page)).toBeVisible();
		await expect(page).toHaveURL(url);
		expect(new URL(page.url()).pathname).not.toContain('/issue/');
		expect(state.errors).toEqual([]);
	});
});

test('dragging an issue to another status group sets its status, mapped to the issue team', async ({ page }) => {
	const state = await mockWorkspace(page);
	const otherTeamId = '00000000-0000-0000-0000-000000000011';
	const doneId = '00000000-0000-0000-0000-000000000031';
	const otherTodoId = '00000000-0000-0000-0000-000000000032';
	const otherDoneId = '00000000-0000-0000-0000-000000000033';
	const status = (id: string, team_id: string, name: string, category: string, position: number) => ({
		id,
		team_id,
		name,
		slug: name.toLowerCase(),
		category,
		position,
		is_default: position === 0,
		project_ids: []
	});
	const info = (id: string, name: string, category: string, position: number) => ({ id, name, category, color: '#6366f1', position });
	const issue = (n: number, team_id: string, status_id: string, status_info: object) => ({
		id: `00000000-0000-0000-0000-${String(500 + n).padStart(12, '0')}`,
		identifier: `ENG-${n}`,
		title: `Drag issue ${n}`,
		team_id,
		status_id,
		status: 'todo',
		priority: 3,
		status_info,
		cycle_id: cycleId,
		project_id: projectId,
		labels: [],
		assignees: [],
		sort_order: n * 1000,
		created_at: now,
		updated_at: now
	});
	const todo = info(statusId, 'Todo', 'unstarted', 0);
	const done = info(doneId, 'Done', 'completed', 1);
	const issues = [
		issue(1, teamId, statusId, todo),
		issue(2, teamId, doneId, done),
		issue(3, otherTeamId, otherTodoId, info(otherTodoId, 'todo', 'unstarted', 0))
	];
	await page.route('**/api/workspaces/test/teams', (route) =>
		route.fulfill({
			json: [
				{ id: teamId, name: 'Engineering', key: 'ENG', created_at: now },
				{ id: otherTeamId, name: 'Design', key: 'DES', created_at: now }
			]
		})
	);
	await page.route('**/api/workspaces/test/teams/*/statuses', (route) =>
		route.fulfill({
			json: route.request().url().includes(otherTeamId)
				? [status(otherTodoId, otherTeamId, 'todo', 'unstarted', 0), status(otherDoneId, otherTeamId, 'Finished', 'completed', 1)]
				: [status(statusId, teamId, 'Todo', 'unstarted', 0), status(doneId, teamId, 'Done', 'completed', 1)]
		})
	);
	await page.route('**/api/workspaces/test/issues?**', (route) =>
		route.fulfill({ json: { data: issues, total_count: issues.length, page: 1, has_more: false } })
	);
	await page.route(`**/api/workspaces/test/teams/${otherTeamId}/cycles`, (route) => route.fulfill({ json: [] }));
	// A workspace-wide project mixes teams, so statuses of both teams are in play.
	await page.route(`**/api/workspaces/test/projects/${projectId}`, (route) =>
		route.fulfill({
			json: { id: projectId, name: 'Launch', team_id: null, status: 'planned', created_at: now, progress: { total: 3, completed: 0, cancelled: 0 } }
		})
	);
	await page.goto(`/test/projects/${projectId}`);
	const groups = page.getByTestId('issue-group');
	// Engineering Todo and Done, Design todo, plus Design's unused Finished as an empty group.
	await expect(groups).toHaveCount(4);
	const patches = () => state.mutations.filter((m) => m.method === 'PATCH' && m.path.includes('/issues/'));

	// Same team: the Done group of Engineering.
	await page.getByText('Drag issue 1', { exact: true }).dragTo(groups.filter({ hasText: 'Drag issue 2' }));
	await expect.poll(patches).toHaveLength(1);
	expect(patches()[0].path).toMatch(/\/issues\/ENG-1$/);
	expect(patches()[0].body).toEqual({ status_id: doneId });

	// Another team: the Design issue lands on its own team status of the same category.
	await page.getByText('Drag issue 3', { exact: true }).dragTo(groups.filter({ hasText: 'Drag issue 2' }));
	await expect.poll(patches).toHaveLength(2);
	expect(patches()[1].body).toEqual({ status_id: otherDoneId });

	// Text dragged in from elsewhere that matches an identifier changes nothing.
	await page.getByTestId('issue-group').first().evaluate((el) => {
		const data = new DataTransfer();
		data.setData('text/plain', 'ENG-2');
		el.dispatchEvent(new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true }));
		el.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }));
	});
	await page.waitForTimeout(300);
	expect(patches()).toHaveLength(2);
	expect(state.errors).toEqual([]);
});

test('an issue can be dragged to a status that has no issues yet', async ({ page }) => {
	const state = await mockWorkspace(page);
	const doneId = '00000000-0000-0000-0000-000000000031';
	const status = (id: string, name: string, category: string, position: number) => ({
		id,
		team_id: teamId,
		name,
		slug: name.toLowerCase(),
		category,
		position,
		is_default: position === 0,
		project_ids: []
	});
	const issue = {
		id: '00000000-0000-0000-0000-000000000601',
		identifier: 'ENG-1',
		title: 'Only issue',
		team_id: teamId,
		status_id: statusId,
		status: 'todo',
		priority: 3,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		cycle_id: cycleId,
		project_id: projectId,
		labels: [],
		assignees: [],
		sort_order: 1000,
		created_at: now,
		updated_at: now
	};
	await page.route('**/api/workspaces/test/teams/*/statuses', (route) =>
		route.fulfill({ json: [status(statusId, 'Todo', 'unstarted', 0), status(doneId, 'Done', 'completed', 1)] })
	);
	await page.route('**/api/workspaces/test/issues?**', (route) =>
		route.fulfill({ json: { data: [issue], total_count: 1, page: 1, has_more: false } })
	);
	await page.goto(`/test/teams/${teamId}`);
	await expect(page.getByText('Only issue', { exact: true })).toBeVisible();
	// Done has no issues but is still a group to drop on.
	const done = page.getByText('Done', { exact: true }).first();
	await expect(done).toBeVisible();
	const patches = () => state.mutations.filter((m) => m.method === 'PATCH' && m.path.includes('/issues/'));
	await page.getByText('Only issue', { exact: true }).dragTo(done);
	await expect.poll(patches).toHaveLength(1);
	expect(patches()[0].path).toMatch(/\/issues\/ENG-1$/);
	expect(patches()[0].body).toEqual({ status_id: doneId });
	expect(state.errors).toEqual([]);
});

for (const path of [`${cyclePath}/${cycleId}`, `/test/teams/${teamId}`]) {
	test(`bulk Set project offers only projects the selected issues can join on ${path}`, async ({ page }) => {
		await mockWorkspace(page);
		const team = (id: string, name: string, is_private: boolean) => ({ id, name, key: name.slice(0, 3).toUpperCase(), is_private, created_at: now });
		const project = (id: string, name: string, team_id: string | null) => ({ id, name, team_id, status: 'planned', created_at: now });
		const designId = '00000000-0000-0000-0000-000000000071';
		const privateTeamId = '00000000-0000-0000-0000-000000000072';
		await page.route('**/api/workspaces/test/teams', (route) =>
			route.fulfill({ json: [team(teamId, 'Engineering', false), team(designId, 'Design', false), team(privateTeamId, 'Secret', true)] })
		);
		await page.route('**/api/workspaces/test/projects', (route) =>
			route.fulfill({
				json: [
					project(projectId, 'Launch', teamId),
					project('00000000-0000-0000-0000-000000000081', 'Website', designId),
					project('00000000-0000-0000-0000-000000000082', 'Company', null),
					project('00000000-0000-0000-0000-000000000083', 'Hidden', privateTeamId)
				]
			})
		);
		await page.goto(path);
		await expect(page.getByText('Scoped issue 1', { exact: true }).first()).toBeVisible();
		await page.keyboard.press('Control+a');
		await page.getByRole('button', { name: 'Actions', exact: true }).click();
		await page.getByRole('button', { name: /Set project/ }).click();
		for (const name of ['Launch', 'Website', 'Company'])
			await expect(page.getByRole('button', { name, exact: true })).toBeVisible();
		// A private team's project cannot take another team's issues.
		await expect(page.getByRole('button', { name: 'Hidden', exact: true })).toHaveCount(0);
	});
}

for (const theme of ['light', 'dark', 'cyber-77']) {
 test(`calendar and selector triggers preserve keyboard focus in ${theme}`, async ({ page }, testInfo) => {
  await page.addInitScript((theme) => {
   localStorage.setItem('kuayle-preferences', JSON.stringify({ themeMode: theme === 'light' ? 'light' : 'dark', lightTheme: 'light', darkTheme: theme === 'cyber-77' ? theme : 'dark', localDirty: true }));
  }, theme);
  const state = await mockWorkspace(page);
  await page.goto(`/test/projects/${projectId}`);
  const trigger = page.getByRole('button', { name: 'Set start', exact: true });
  await trigger.focus();
  await trigger.press('Enter');
  await expect(page.getByRole('grid').first()).toBeVisible();
  await expect(page.locator('button button')).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath(`calendar-${theme}.png`) });
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await page.getByTestId('scoped-issues').locator('button', { hasText: 'New issue' }).click();
  const dialog = page.getByRole('dialog', { name: 'New issue', exact: true });
  await expect(dialog).toBeVisible();
  await expect(page.locator('button button')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  expect(state.errors).toEqual([]);
 });
}

test('review regression: command backdrop dismisses from the bottom of the viewport', async ({ page }, testInfo) => {
	await mockWorkspace(page);
	await page.goto(cyclePath);
	await page.getByRole('link', { name: 'Current cycle', exact: true }).waitFor();
	await page.keyboard.press('ControlOrMeta+k');
	const search = page.getByPlaceholder('Type a command or search...');
	await expect(search).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath('command-backdrop.png'), animations: 'disabled' });
	await page.mouse.click(20, page.viewportSize()!.height - 20);
	await expect(search).not.toBeVisible();
});

test('review regression: saved legacy team icons render and remain selectable', async ({ page }) => {
	const state = await mockWorkspace(page, 'owner', 'Github');
	await page.goto(`/test/settings/teams/${teamId}`);
	const picker = page.getByRole('button', { name: /Choose icon or emoji/ });
	await expect(picker.locator('svg path').first()).toHaveAttribute('d', /^M15 22v-4/);
	await picker.click();
	const search = page.getByPlaceholder('Search Lucide icons', { exact: true });
	for (const name of ['Github', 'Gitlab', 'Figma', 'Slack', 'Trello', 'Twitter', 'Youtube', 'Linkedin', 'Facebook', 'Instagram', 'Dribbble', 'Codepen', 'Codesandbox', 'Framer', 'Pocket', 'Twitch', 'Chromium', 'RailSymbol']) {
		await search.fill(name);
		const label = name.replace(/([a-z0-9])([A-Z])/g, '$1 $2');
		await expect(page.getByRole('button', { name: `Use ${label} icon`, exact: true })).toBeVisible();
	}
	await search.fill('Slack');
	await page.getByRole('button', { name: 'Use Slack icon', exact: true }).click();
	await expect.poll(() => state.mutations.some((m) => m.method === 'PATCH' && m.body.icon === 'Slack')).toBe(true);
	await expect(picker.locator('svg path').first()).toHaveAttribute('d', /^M19 8\.5V10/);
	expect(state.errors).toEqual([]);
});

test('review regression: action menus retain compact item spacing', async ({ page }) => {
	await mockWorkspace(page);
	await page.goto(cyclePath);
	await page.getByRole('button', { name: 'Actions for Current cycle' }).click();
	await expect.poll(async () => {
		const edit = await page.getByRole('button', { name: 'Edit cycle', exact: true }).boundingBox();
		const complete = await page.getByRole('button', { name: 'Complete cycle', exact: true }).boundingBox();
		return complete!.y - (edit!.y + edit!.height);
	}).toBeLessThanOrEqual(4);
});

test('issue context menu offers only labels that apply to the issue project', async ({ page }) => {
	const state = await mockWorkspace(page);
	const label = (id: string, name: string, project_id: string | null) => ({
		id,
		name,
		color: '#ef4444',
		description: null,
		parent_id: null,
		project_id,
		created_at: now,
		updated_at: now
	});
	await page.route('**/api/workspaces/test/labels', (route) =>
		route.fulfill({
			json: [
				label('label-workspace', 'Bug', null),
				label('label-launch', 'Launch only', projectId),
				label('label-other', 'Other project only', '00000000-0000-0000-0000-000000000099')
			]
		})
	);
	await page.goto(`/test/projects/${projectId}`);
	await page.getByTestId('scoped-issues').getByText('Scoped issue 1', { exact: true }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Labels', exact: true }).hover();
	await expect(page.getByRole('menuitemcheckbox', { name: 'Bug', exact: true })).toBeVisible();
	await expect(page.getByRole('menuitemcheckbox', { name: 'Launch only', exact: true })).toBeVisible();
	await expect(page.getByRole('menuitemcheckbox', { name: 'Other project only', exact: true })).toHaveCount(0);
	await page.getByRole('menuitemcheckbox', { name: 'Launch only', exact: true }).click();
	await expect
		.poll(() => state.mutations.find((m) => m.method === 'PATCH' && m.path.endsWith('/issues/ENG-1'))?.body)
		.toEqual({ label_ids: ['label-launch'] });
	expect(state.errors).toEqual([]);
});

test('issue context menu lists the team projects first, then Teams > team > projects', async ({ page }) => {
	const state = await mockWorkspace(page);
	const team = (id: string, name: string, key: string) => ({ id, name, key, is_private: false, created_at: now });
	const project = (id: string, name: string, team_id: string | null) => ({ id, name, team_id, status: 'planned', created_at: now });
	const designId = '00000000-0000-0000-0000-000000000071';
	const websiteId = '00000000-0000-0000-0000-000000000081';
	await page.route('**/api/workspaces/test/teams', (route) =>
		route.fulfill({ json: [team(teamId, 'Engineering', 'ENG'), team(designId, 'Design', 'DES')] })
	);
	await page.route('**/api/workspaces/test/projects', (route) =>
		route.fulfill({
			json: [
				project(projectId, 'Launch', teamId),
				project(websiteId, 'Website', designId),
				project('00000000-0000-0000-0000-000000000082', 'Company', null)
			]
		})
	);
	await page.goto(`${cyclePath}/${cycleId}`);
	await page.getByTestId('scoped-issues').getByText('Scoped issue 1', { exact: true }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Project', exact: true }).hover();
	await expect(page.getByRole('menuitem', { name: 'No project', exact: true })).toBeVisible();
	// The issue's own team is listed directly; other teams' and workspace-wide projects sit one level down.
	await expect(page.getByRole('menuitem', { name: 'Launch', exact: true })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: 'Website', exact: true })).toHaveCount(0);
	await page.getByRole('menuitem', { name: 'Teams', exact: true }).hover();
	await expect(page.getByRole('menuitem', { name: 'Engineering', exact: true })).toHaveCount(0);
	await page.getByRole('menuitem', { name: 'Design', exact: true }).hover();
	await expect(page.getByRole('menuitem', { name: 'Company', exact: true })).toHaveCount(0);
	await page.getByRole('menuitem', { name: 'Website', exact: true }).click();
	await expect
		.poll(() => state.mutations.find((m) => m.method === 'PATCH' && m.path.endsWith('/issues/ENG-1'))?.body)
		.toEqual({ project_id: websiteId });
	expect(state.errors).toEqual([]);
});

test('project name renames inline with Enter, Esc cancels and empty is rejected', async ({
	page
}) => {
	const state = await mockWorkspace(page);
	const patches = () =>
		state.mutations.filter((m) => m.method === 'PATCH' && m.path.endsWith(`/projects/${projectId}`));
	await page.goto(`/test/projects/${projectId}`);
	await expect(page.getByText('Launch', { exact: true }).first()).toBeVisible();

	await page.getByRole('button', { name: 'Rename project', exact: true }).click();
	await page.getByRole('textbox', { name: 'Rename project', exact: true }).fill('Changed');
	await page.keyboard.press('Escape');
	expect(patches()).toHaveLength(0);
	await expect(page.getByRole('textbox', { name: 'Rename project', exact: true })).toHaveCount(0);

	await page.getByRole('button', { name: 'Rename project', exact: true }).click();
	await page.getByRole('textbox', { name: 'Rename project', exact: true }).fill('   ');
	await page.keyboard.press('Enter');
	expect(patches()).toHaveLength(0);

	await page.getByRole('textbox', { name: 'Rename project', exact: true }).fill('  Relaunch  ');
	await page.keyboard.press('Enter');
	await expect.poll(() => patches().length).toBe(1);
	expect(patches()[0].body).toEqual({ name: 'Relaunch' });
	await expect(page.getByText('Relaunch', { exact: true }).first()).toBeVisible();
	expect(state.errors).toEqual([]);
});

test('guests cannot rename a project', async ({ page }) => {
	await mockWorkspace(page, 'guest');
	await page.goto(`/test/projects/${projectId}`);
	await expect(page.getByText('Launch', { exact: true }).first()).toBeVisible();
	await expect(page.getByRole('button', { name: 'Rename project', exact: true })).toHaveCount(0);
});
