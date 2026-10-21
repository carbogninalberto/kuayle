import { test, expect, type Page } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const secondTeamId = '00000000-0000-0000-0000-000000000011';
const statusId = '00000000-0000-0000-0000-000000000020';
const teamPath = `/test/teams/${teamId}`;
const createdAt = '2026-01-01T00:00:00Z';

async function mockWorkspace(page: Page, history: unknown[] = []) {
	await page.routeWebSocket('**/ws', () => {});
	const unhandledPaths: string[] = [];
	const pageErrors: string[] = [];
	page.on('pageerror', (error) => pageErrors.push(error.message));
	const user = {
		id: userId,
		email: 'test@example.com',
		name: 'Test User',
		display_name: 'Test User',
		avatar_url: null
	};
	const workspace = {
		id: '00000000-0000-0000-0000-000000000002',
		name: 'Test Workspace',
		slug: 'test',
		logo_url: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const teams = [teamId, secondTeamId].map((id, index) => ({
		id,
		name: index === 0 ? 'Engineering' : 'Design',
		key: index === 0 ? 'ENG' : 'DES',
		description: null,
		color: '#6366f1',
		icon: 'layers',
		triage_enabled: false,
		parent_auto_close_enabled: false,
		sub_issue_auto_close_enabled: false,
		issue_copy_prompt: null,
		created_at: createdAt,
		updated_at: createdAt
	}));
	const issues = [1, 2, 3].map((number) => ({
		id: `00000000-0000-0000-0000-${String(100 + number).padStart(12, '0')}`,
		identifier: `ENG-${number}`,
		title: `Navigation issue ${number}`,
		description: null,
		status: 'todo',
		status_id: statusId,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		priority: 3,
		team_id: teamId,
		project_id: null,
		cycle_id: null,
		creator_id: userId,
		assignee_id: userId,
		creator: user,
		assignee: user,
		assignees: [user],
		parent_id: null,
		due_date: null,
		sort_order: number * 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	}));
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path.endsWith('/history')) return route.fulfill({ json: history });
		if (path.endsWith('/members')) return route.fulfill({ json: [{ user_id: userId, name: 'Test Editor', role: 'owner' }] });
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: teams });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({
				json: {
					data: issues,
					total_count: issues.length,
					page: 1,
					has_more: false
				}
			});
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/))
			return route.fulfill({
				json: [
					{
						id: statusId,
						team_id: url.pathname.split('/')[5],
						name: 'Todo',
						slug: 'todo',
						category: 'unstarted',
						color: '#6366f1',
						position: 0,
						is_default: true,
						project_ids: [],
						created_at: createdAt,
						updated_at: createdAt
					}
				]
			});
		const detailMatch = path.match(/^\/api\/workspaces\/test\/issues\/(ENG-[123])$/);
		if (detailMatch) return route.fulfill({ json: issues.find((issue) => issue.identifier === detailMatch[1]) });
		if (path.match(/^\/api\/workspaces\/test\/issues\/ENG-[123]\/github$/))
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (
			[
				'/api/workspaces/test/projects',
				'/api/workspaces/test/labels',
				'/api/workspaces/test/members',
				'/api/workspaces/test/views'
			].includes(path) ||
			path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/cycles$/) ||
			path.match(/^\/api\/workspaces\/test\/issues\/ENG-[123]\/(comments|history|relations|sub-issues)$/)
		)
			return route.fulfill({ json: [] });
		unhandledPaths.push(path);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return () => {
		expect(unhandledPaths).toEqual([]);
		expect(pageErrors).toEqual([]);
	};
}

const versions = [
 { id: 'a', issue_id: 'issue', user_id: userId, field: 'title', old_value: 'Original title', new_value: 'Updated title', created_at: '2026-01-01T01:00:00Z' },
 { id: 'b', issue_id: 'issue', user_id: userId, field: 'description', old_value: '<p>Old <strong>description</strong></p><input type="checkbox" checked>', new_value: '<p>New description</p><img src="x" onerror="window.historyXss=true"><script>window.historyXss=true</script>', created_at: '2026-01-02T01:00:00Z' },
 { id: 'c', issue_id: 'issue', user_id: null, field: 'description', old_value: '<p>New description</p>', new_value: '', created_at: '2026-01-03T01:00:00Z' }
];

test('reviews complete changes in order and safely renders historical HTML', async ({ page }, testInfo) => {
 const healthy = await mockWorkspace(page, [...versions].reverse());
 await page.goto('/test/issue/ENG-1');
 await page.getByRole('button', { name: 'Version history', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'ENG-1 version history' });
 await expect(dialog.getByRole('region', { name: 'After', exact: true })).toContainText('Empty');
 await expect(dialog.getByText(/Unknown author changed the description/)).toBeVisible();
 await expect(dialog.getByRole('button', { name: 'Next change' })).toBeDisabled();
 await dialog.getByRole('button', { name: 'Previous change' }).click();
 await expect(dialog.getByRole('region', { name: 'Before', exact: true })).toContainText('Old description');
 await expect(dialog.getByRole('region', { name: 'After', exact: true })).toContainText('New description');
 await expect(dialog.getByRole('region', { name: 'Before', exact: true }).getByRole('checkbox')).toBeDisabled();
 expect(await page.evaluate(() => (window as any).historyXss)).toBeUndefined();
 await expect(dialog.locator('script, [onerror]')).toHaveCount(0);
 await dialog.getByLabel('Show HTML source').check();
 await expect(dialog.getByRole('region', { name: 'Before', exact: true })).toContainText('<strong>description</strong>');
 await dialog.getByRole('button', { name: 'Previous change' }).click();
 await expect(dialog.getByRole('region', { name: 'Before', exact: true })).toContainText('Original title');
 await expect(dialog.getByRole('region', { name: 'After', exact: true })).toContainText('Updated title');
 await expect(dialog.getByText(/Test Editor changed the title/)).toBeVisible();
 await expect(dialog.getByRole('button', { name: 'Previous change' })).toBeDisabled();
 await page.screenshot({ path: testInfo.outputPath('version-history-desktop.png') });
 await page.keyboard.press('Escape');
 await expect(dialog).not.toBeVisible();
 healthy();
});

test('distinguishes empty history from a failed request and retries', async ({ page }) => {
 await mockWorkspace(page);
 await page.goto('/test/issue/ENG-1');
 await page.getByRole('button', { name: 'Version history', exact: true }).click();
 await expect(page.getByText(/No title or description changes recorded yet/)).toBeVisible();
 await page.keyboard.press('Escape');
 await page.route('**/issues/ENG-1/history', route => route.fulfill({ status: 500, json: { error: { message: 'Unavailable' } } }));
 await page.getByRole('button', { name: 'Version history', exact: true }).click();
 await expect(page.getByRole('dialog', { name: 'ENG-1 version history' }).getByRole('alert')).toContainText('Could not load version history');
 await page.route('**/issues/ENG-1/history', route => route.fulfill({ json: versions }));
 await page.getByRole('button', { name: 'Try again' }).click();
 await expect(page.getByRole('combobox', { name: 'Recorded change' })).toBeVisible();
});

test('keeps long descriptions readable within a mobile viewport', async ({ page }, testInfo) => {
 await page.setViewportSize({ width: 390, height: 844 });
 await mockWorkspace(page, [{ ...versions[1], new_value: '<p>' + 'long-content'.repeat(3000) + '</p>' }]);
 await page.goto('/test/issue/ENG-1');
 await page.getByRole('button', { name: 'Version history', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'ENG-1 version history' });
 await expect(dialog.getByRole('region', { name: 'After', exact: true })).toContainText('long-content');
 const bounds = await dialog.boundingBox();
 expect(bounds!.x).toBeGreaterThanOrEqual(0);
 expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390);
 expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(844);
 expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
 await page.screenshot({ path: testInfo.outputPath('version-history-mobile.png') });
});

test('reviews history from the project issue sheet', async ({ page }) => {
 const healthy = await mockWorkspace(page, [...versions].reverse());
 const projectId = '00000000-0000-0000-0000-000000000030';
 const project = {
  id: projectId, name: 'History project', description: null, status: 'planned', team_id: teamId,
  lead_id: null, start_date: null, target_date: null, sort_order: 0, created_at: createdAt, updated_at: createdAt
 };
 await page.route(`**/api/workspaces/test/projects/${projectId}`, route => route.fulfill({ json: project }));
 // Navigation checks the accessible project list before rendering this route.
 await page.route('**/api/workspaces/test/projects', route => route.fulfill({ json: [project] }));
 await page.route('**/api/workspaces/test/github/status', route => route.fulfill({ json: { repos: [] } }));
 await page.route('**/api/workspaces/test/dev-machine-scope-setting?*', route => route.fulfill({ json: {} }));
 await page.route('**/api/workspaces/test/dev-machine-environments', route => route.fulfill({ json: [] }));
 await page.goto(`/test/projects/${projectId}`);
 await page.getByText('Navigation issue 1', { exact: true }).click();
 const sheet = page.getByRole('dialog', { name: 'ENG-1', exact: true });
 await expect(sheet).toBeVisible();
 await sheet.getByRole('button', { name: 'Version history', exact: true }).click();
 const history = page.getByRole('dialog', { name: 'ENG-1 version history', exact: true });
 await expect(history.getByRole('region', { name: 'After', exact: true })).toContainText('Empty');
 await page.keyboard.press('Escape');
 await expect(history).not.toBeVisible();
 await expect(sheet).toBeVisible();
 healthy();
});

test('uses the selected application language for history', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('PARAGLIDE_LOCALE', 'it'));
 await mockWorkspace(page, [...versions].reverse());
 await page.goto('/test/issue/ENG-1');
 await page.getByRole('button', { name: 'Cronologia delle versioni', exact: true }).click();
 const history = page.getByRole('dialog', { name: 'Cronologia delle versioni di ENG-1' });
 await expect(history.getByRole('region', { name: 'Dopo', exact: true })).toContainText('Vuoto');
 await expect(history.getByRole('button', { name: 'Modifica precedente' })).toBeVisible();
 await expect(history.getByText(/Autore sconosciuto ha modificato la descrizione/)).toBeVisible();
});
