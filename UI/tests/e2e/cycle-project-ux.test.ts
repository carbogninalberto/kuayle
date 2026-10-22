import { expect, test, type Page } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const projectId = '00000000-0000-0000-0000-000000000020';
const statusId = '00000000-0000-0000-0000-000000000030';
const cycleId = '00000000-0000-0000-0000-000000000040';
const cyclePath = `/test/teams/${teamId}/cycles`;
const now = '2026-01-01T00:00:00Z';

async function mockWorkspace(page: Page, role = 'owner', icon = 'layers') {
	await page.routeWebSocket('**/ws', () => {});
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	const mutations: { method: string; path: string; body: any }[] = [];
	const queries: URLSearchParams[] = [];
	const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon, created_at: now };
	const project = { id: projectId, name: 'Launch', team_id: teamId, status: 'planned', created_at: now };
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
		if (path === `/api/workspaces/test/projects/${projectId}`)
			return route.fulfill({
				json: {
					...project,
					progress: { total: issues.filter((i) => i.project_id === projectId).length, completed: 0, cancelled: 0 }
				}
			});
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
