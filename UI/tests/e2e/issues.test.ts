import { test, expect } from '@playwright/test';

test('redirects to login when not authenticated', async ({ page }) => {
	await page.goto('/');
	await expect(page).toHaveURL(/.*login/);
});

test('loads more on scroll without duplicating the initial request', async ({ page }) => {
	const teamId = '00000000-0000-0000-0000-000000000010';
	const issueRequests: URL[] = [];
	const unhandledPaths: string[] = [];
	let workspaceRequests = 0;
	let releaseMetadata!: () => void;
	const metadataReady = new Promise<void>((resolve) => {
		releaseMetadata = resolve;
	});

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const requestUrl = new URL(route.request().url());
		const path = requestUrl.pathname;
		if (!path.startsWith('/api/')) return route.continue();

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
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		}

		if (path === '/api/workspaces/test') {
			workspaceRequests += 1;
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

		if (
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === `/api/workspaces/test/teams/${teamId}/statuses`
		) {
			return route.fulfill({ json: [] });
		}

		if (path === '/api/workspaces/test/views' || path === `/api/workspaces/test/teams/${teamId}/cycles`) {
			await metadataReady;
			return route.fulfill({ json: [] });
		}

		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}

		if (path === '/api/workspaces/test/issues') {
			issueRequests.push(requestUrl);
			releaseMetadata();
			return route.fulfill({
				json: {
					data: [],
					total_count: 51,
					page: Number(requestUrl.searchParams.get('page') ?? 1),
					has_more: true
				}
			});
		}

		unhandledPaths.push(path);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});

	await page.goto(`/test/teams/${teamId}`);

	await expect.poll(() => issueRequests.length).toBe(1);
	await expect(page.getByRole('button', { name: 'Load more' })).toBeVisible();
	await page.waitForTimeout(750);

	expect(issueRequests).toHaveLength(1);
	expect(issueRequests[0].searchParams.has('page')).toBe(false);
	expect(workspaceRequests).toBe(1);
	expect(unhandledPaths).toEqual([]);

	await page.getByRole('button', { name: 'Load more' }).evaluate((button) => {
		const scrollContainer = button.parentElement?.parentElement;
		if (!scrollContainer) throw new Error('Missing issue list scroll container');
		scrollContainer.scrollTop = scrollContainer.scrollHeight;
		scrollContainer.dispatchEvent(new Event('scroll'));
	});
	await expect.poll(() => issueRequests.length).toBe(2);
	expect(issueRequests[1].searchParams.get('page')).toBe('2');
});
