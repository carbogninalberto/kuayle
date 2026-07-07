import { expect, test } from '@playwright/test';

const workflowSortOrder = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

test('keeps dirty local theme when remote preferences are stale', async ({ page }) => {
	let preferenceGets = 0;
	let patchPayload: Record<string, unknown> | null = null;

	await page.addInitScript((order) => {
		localStorage.setItem(
			'kuayle-preferences',
			JSON.stringify({
				fontSize: 'default',
				pointerCursors: true,
				themeMode: 'dark',
				lightTheme: 'light',
				darkTheme: 'cyber-77',
				workflowSortMode: 'default',
				workflowSortOrder: order,
				teamWorkflowSortOverrides: {},
				localDirty: true
			})
		);
	}, workflowSortOrder);

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;

		if (path === '/api/auth/me') {
			return route.fulfill({
				json: {
					id: 'user-1',
					email: 'test@example.com',
					name: 'Test User',
					display_name: 'Test User',
					avatar_url: null
				}
			});
		}

		if (path === '/api/preferences' && request.method() === 'GET') {
			preferenceGets += 1;
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: workflowSortOrder,
					team_workflow_sort_overrides: {}
				}
			});
		}

		if (path === '/api/preferences' && request.method() === 'PATCH') {
			patchPayload = request.postDataJSON() as Record<string, unknown>;
			return route.fulfill({ json: patchPayload });
		}

		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: {
					id: 'workspace-1',
					name: 'Test Workspace',
					slug: 'test',
					logo_url: null,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			});
		}

		if (
			path === '/api/workspaces/test/teams' ||
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

		return route.fulfill({
			status: 404,
			json: { error: { code: 'UNHANDLED_TEST_ROUTE', message: `${request.method()} ${path}` } }
		});
	});

	await page.goto('/test/settings/preferences');

	await expect.poll(() => preferenceGets).toBeGreaterThan(0);
	await expect(page.locator('html')).toHaveClass(/cyber-77/);
	await expect.poll(() => patchPayload?.dark_theme).toBe('cyber-77');
	await expect
		.poll(() =>
			page.evaluate(() => JSON.parse(localStorage.getItem('kuayle-preferences') ?? '{}').localDirty)
		)
		.toBe(false);
	await expect(page.locator('html')).toHaveClass(/cyber-77/);
});
