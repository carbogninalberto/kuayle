import { test, expect, type Page } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const secondTeamId = '00000000-0000-0000-0000-000000000011';
const statusId = '00000000-0000-0000-0000-000000000020';
const teamPath = `/test/teams/${teamId}`;
const createdAt = '2026-01-01T00:00:00Z';

async function mockWorkspace(page: Page) {
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

async function expectLayout(page: Page, layout: 'List' | 'Board') {
	await expect(page.getByRole('radio', { name: `${layout} view`, exact: true })).toBeChecked();
	await expect(page.getByText('Navigation issue 1', { exact: true })).toBeVisible();
}

test('returns directly to the team board after navigating through multiple issues', async ({ page }) => {
	const assertHealthy = await mockWorkspace(page);
	await page.goto(teamPath);
	await expectLayout(page, 'List');
	await page.getByRole('radio', { name: 'Board view', exact: true }).click();
	await expectLayout(page, 'Board');
	await page.getByText('Navigation issue 1', { exact: true }).click();
	await expect(page).toHaveURL('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Next issue (J)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-2');
	await page.getByRole('button', { name: 'Next issue (J)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-3');
	await page.goBack();
	await expect(page).toHaveURL(teamPath);
	await expectLayout(page, 'Board');
	assertHealthy();
});

test('returns to the team list after navigating next and previous', async ({ page }) => {
	const assertHealthy = await mockWorkspace(page);
	await page.goto(teamPath);
	await expectLayout(page, 'List');
	await page.getByText('Navigation issue 1', { exact: true }).click();
	await expect(page).toHaveURL('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Next issue (J)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-2');
	await page.getByRole('button', { name: 'Previous issue (K)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-1');
	await page.goBack();
	await expect(page).toHaveURL(teamPath);
	await expectLayout(page, 'List');
	assertHealthy();
});

test('shares the session layout across teams and My Issues and returns to My Issues', async ({ page }) => {
	const assertHealthy = await mockWorkspace(page);
	await page.goto(teamPath);
	await page.getByRole('radio', { name: 'Board view', exact: true }).click();
	await expectLayout(page, 'Board');
	await page.locator(`a[href="/test/teams/${secondTeamId}"]`).first().click();
	await expect(page).toHaveURL(`/test/teams/${secondTeamId}`);
	await expectLayout(page, 'Board');
	await page.locator('a[href="/test/my-issues"]').first().click();
	await expect(page).toHaveURL('/test/my-issues');
	await expectLayout(page, 'Board');
	await page.getByText('Navigation issue 1', { exact: true }).click();
	await expect(page).toHaveURL('/test/issue/ENG-1');
	await page.getByRole('button', { name: 'Next issue (J)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-2');
	await page.goBack();
	await expect(page).toHaveURL('/test/my-issues');
	await expectLayout(page, 'Board');
	await page.getByRole('radio', { name: 'List view', exact: true }).click();
	await expectLayout(page, 'List');
	await page.getByText('Navigation issue 1', { exact: true }).click();
	await expect(page).toHaveURL('/test/issue/ENG-1');
	await page.goBack();
	await expectLayout(page, 'List');
	assertHealthy();
});

test('loads adjacent issues when opening a direct link in a fresh session', async ({ page }) => {
	const assertHealthy = await mockWorkspace(page);
	await page.goto('/test/issue/ENG-2');
	await expect(page.getByText('Navigation issue 2', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Next issue (J)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-3');
	await expect(page.getByText('Navigation issue 3', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Previous issue (K)' }).click();
	await expect(page).toHaveURL('/test/issue/ENG-2');
	await expect(page.getByText('Navigation issue 2', { exact: true })).toBeVisible();
	assertHealthy();
});
