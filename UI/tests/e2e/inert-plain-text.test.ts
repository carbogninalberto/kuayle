import { test, expect, type Page } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000020';
const createdAt = '2026-01-01T00:00:00Z';

// Stored HTML whose only job is to be noticed if the page ever makes it live.
const probe = (name: string) => `<img src="/${name}-probe.png" onerror="window.plainTextProbe='${name}'">`;

async function mockWorkspace(page: Page) {
	await page.routeWebSocket('**/ws', () => {});
	const probeRequests: string[] = [];
	const pageErrors: string[] = [];
	page.on('pageerror', (error) => pageErrors.push(error.message));
	await page.route('**/*-probe.png', (route) => {
		probeRequests.push(new URL(route.request().url()).pathname);
		return route.abort();
	});
	const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
	const workspace = {
		id: '00000000-0000-0000-0000-000000000002',
		name: 'Test Workspace',
		slug: 'test',
		logo_url: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const team = {
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
		created_at: createdAt,
		updated_at: createdAt
	};
	const status = {
		id: statusId,
		team_id: teamId,
		name: 'Todo',
		slug: 'todo',
		category: 'unstarted',
		color: '#6366f1',
		position: 0,
		is_default: true,
		project_ids: [],
		created_at: createdAt,
		updated_at: createdAt
	};
	const issue = (number: number, description: string | null) => ({
		id: `00000000-0000-0000-0000-${String(100 + number).padStart(12, '0')}`,
		identifier: `ENG-${number}`,
		title: `Plain text issue ${number}`,
		description,
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
		parent_id: null as string | null,
		parent: null as unknown,
		due_date: null,
		sort_order: number * 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	});
	const parent = issue(1, `<p>Parent preview summary</p>${probe('parent')}`);
	const child = { ...issue(2, null), parent_id: parent.id, parent };
	const found = issue(3, `<p>Palette preview summary</p>${probe('palette')}`);

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
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') {
			const data = url.searchParams.get('search') ? [found] : [];
			return route.fulfill({ json: { data, total_count: data.length, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/issues/ENG-2') return route.fulfill({ json: child });
		if (path === '/api/workspaces/test/issues/ENG-2/github')
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/issues/ENG-2/forges')
			return route.fulfill({ json: { merge_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/)) return route.fulfill({ json: [status] });
		if (path.endsWith('/members')) return route.fulfill({ json: [{ user_id: userId, name: 'Test User', role: 'owner' }] });
		return route.fulfill({ json: [] });
	});
	return { probeRequests, pageErrors };
}

test('previews of stored descriptions never load or run the markup they contain', async ({ page }) => {
	const { probeRequests, pageErrors } = await mockWorkspace(page);
	await page.goto('/test/issue/ENG-2');

	// The parent preview is computed as soon as the issue loads.
	const parentLink = page.getByRole('button', { name: /ENG-1/ }).first();
	await expect(parentLink).toBeVisible();
	await parentLink.hover();
	await expect(page.getByText('Parent preview summary')).toBeVisible();

	await page.keyboard.press('ControlOrMeta+k');
	await page.getByPlaceholder('Type a command or search...').fill('summary');
	await expect(page.getByText('Plain text issue 3')).toBeVisible();
	await expect(page.getByText(/Palette preview/)).toBeVisible();

	expect(probeRequests).toEqual([]);
	expect(await page.evaluate(() => (window as unknown as { plainTextProbe?: string }).plainTextProbe)).toBeUndefined();
	await expect(page.locator('[onerror]')).toHaveCount(0);
	expect(pageErrors).toEqual([]);
});
