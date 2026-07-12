import { expect, test } from '@playwright/test';

test('shows workspace analytics and opens the explorer', async ({ page }) => {
	const teamId = '00000000-0000-0000-0000-000000000010';
	let scopedOverviewRequests = 0;
	const issueListQueries: URLSearchParams[] = [];
	const pageErrors: Error[] = [];
	page.on('pageerror', (error) => {
		pageErrors.push(error);
	});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const requestUrl = new URL(route.request().url());
		const path = requestUrl.pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'test@example.com',
					name: 'Test User',
					display_name: 'Test User',
					avatar_url: null
				}
			});
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}
				}
			});
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000002',
					name: 'Test Workspace',
					slug: 'test',
					logo_url: null,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			});
		}
		if (path === '/api/workspaces') {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-000000000002',
						name: 'Test Workspace',
						slug: 'test',
						logo_url: null,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			});
		}
		if (path === '/api/workspaces/test/teams') {
			return route.fulfill({
				json: [
					{
						id: teamId,
						name: 'Engineering',
						key: 'ENG',
						description: null,
						color: '#6366f1',
						icon: 'layers',
						triage_enabled: false,
						parent_auto_close_enabled: false,
						sub_issue_auto_close_enabled: false,
						issue_copy_prompt: null,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			});
		}
		if (path === `/api/workspaces/test/teams/${teamId}/statuses`) {
			return route.fulfill({
				json: [
					{
						id: '00000000-0000-0000-0000-000000000003',
						team_id: teamId,
						name: 'In progress',
						slug: 'in-progress',
						category: 'started',
						color: '#6366f1',
						position: 2,
						is_default: false,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			});
		}
		if (
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === '/api/workspaces/test/views'
		) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		if (path === '/api/workspaces/test/analytics/overview') {
			if (requestUrl.searchParams.get('team_id') === teamId) scopedOverviewRequests += 1;
			return route.fulfill({
				json: {
					total_issues: 42,
					open_issues: 18,
					completed_issues: 24,
					overdue_issues: 3,
					total_projects: 4,
					total_members: 8,
					started_issues: 6,
					unassigned_issues: 2,
					completion_rate: 57.14,
					avg_lead_time_hours: 48,
					avg_cycle_time_hours: 24
				}
			});
		}
		if (path === '/api/workspaces/test/analytics/distribution') {
			return route.fulfill({
				json: {
					by_status: [
						{
							status_id: '00000000-0000-0000-0000-000000000004',
							name: 'Backlog',
							color: null,
							category: 'backlog',
							count: 5
						},
						{
							status_id: '00000000-0000-0000-0000-000000000003',
							name: 'In progress',
							color: '#6366f1',
							category: 'started',
							count: 18
						}
					],
					by_priority: [{ priority: 2, count: 12 }]
				}
			});
		}
		if (path === '/api/workspaces/test/analytics/burnup') {
			return route.fulfill({
				json: {
					interval: 'week',
					from: '2026-04-14',
					to: '2026-07-12',
					points: [{ date: '2026-07-06', created: 5, completed: 3, total_created: 42, total_completed: 24, scope: 18 }]
				}
			});
		}
		if (path === '/api/workspaces/test/analytics/insights') {
			const requestedSlice = requestUrl.searchParams.get('slice') ?? 'none';
			const group =
				requestedSlice === 'cycle'
					? { key: '__null__', label: 'No cycle' }
					: requestedSlice === 'status_type'
						? { key: 'started', label: 'Started' }
						: requestedSlice === 'team'
							? { key: teamId, label: 'Engineering' }
							: {
									key: '00000000-0000-0000-0000-000000000003',
									label: 'In progress',
									color: '#6366f1'
								};
			return route.fulfill({
				json: {
					measure: 'issue_count',
					slice: requestedSlice,
					segment: 'none',
					unit: 'issues',
					total_count: 42,
					aggregate: 42,
					groups: [
						{
							...group,
							count: 18,
							value: 18,
							segments: []
						}
					],
					points: []
				}
			});
		}
		if (path === '/api/workspaces/test/issues') {
			issueListQueries.push(requestUrl.searchParams);
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});

	await page.goto('/test/insights');
	await expect(page.getByRole('heading', { name: 'Insights' })).toBeVisible();
	await expect(page.getByText('Total issues')).toBeVisible();
	await expect(page.getByText('42', { exact: true })).toBeVisible();
	await expect(page.getByText('Completion rate')).toBeVisible();
	await expect(page.getByText('57%')).toBeVisible();
	await page.getByLabel('Team scope').click();
	await page.getByRole('option', { name: 'Engineering' }).click();
	await expect(page).toHaveURL(new RegExp(`team=${teamId}`));
	await expect.poll(() => scopedOverviewRequests).toBeGreaterThan(0);
	await expect(page.getByText('Using Engineering custom statuses')).toBeVisible();

	await page.getByRole('tab', { name: 'Explore' }).click();
	await expect(page).toHaveURL(/tab=explore/);
	await expect(page.getByLabel('Measure')).toContainText('Issue count');
	await page.getByLabel('Group by').click();
	await page.getByRole('option', { name: 'Status', exact: true }).click();
	await expect(page.getByLabel('Group by')).toContainText('Status');
	await page.getByLabel('Date range').click();
	await expect(page.getByRole('button', { name: '90 days' })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(page.getByText('In progress', { exact: true })).toBeVisible();

	await page.goto('/test/insights?tab=explore&slice=cycle');
	await expect(page.getByText('No cycle', { exact: true })).toBeVisible();
	await page.getByText('No cycle', { exact: true }).click();
	await expect(page).toHaveURL('/test/my-issues?cycle=none');
	await expect.poll(() => issueListQueries.length).toBeGreaterThan(0);
	const drillDownQuery = issueListQueries.at(-1)!;
	expect(drillDownQuery.get('cycle')).toBe('none');
	expect(drillDownQuery.has('assignee')).toBe(false);
	expect(drillDownQuery.has('creator')).toBe(false);

	await page.goto('/test/insights?tab=explore&slice=status_type');
	const statusTypeQueryCount = issueListQueries.length;
	await page.getByText('Started', { exact: true }).click();
	await expect(page).toHaveURL('/test/my-issues?status_type=started');
	await expect.poll(() => issueListQueries.length).toBeGreaterThan(statusTypeQueryCount);
	const statusTypeQuery = issueListQueries.at(-1)!;
	expect(statusTypeQuery.get('status_type')).toBe('started');
	expect(statusTypeQuery.has('assignee')).toBe(false);
	expect(statusTypeQuery.has('creator')).toBe(false);

	await page.goto('/test/insights?tab=explore&slice=team');
	const teamQueryCount = issueListQueries.length;
	await page.getByText('Engineering', { exact: true }).last().click();
	await expect(page).toHaveURL(`/test/my-issues?team=${teamId}`);
	await expect.poll(() => issueListQueries.length).toBeGreaterThan(teamQueryCount);
	expect(issueListQueries.at(-1)!.get('team')).toBe(teamId);

	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/insights?tab=explore');
	await expect(page.getByRole('tab', { name: 'Explore' })).toBeVisible();
	await expect(page.getByLabel('Measure')).toBeVisible();
	await expect.poll(() => pageErrors.map((error) => error.message)).toEqual([]);
});
