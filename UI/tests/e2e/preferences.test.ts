import { expect, test, type Page } from '@playwright/test';

const workflowSortOrder = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

async function mockProfile(page: Page, timezone: string | undefined, darkTheme: string) {
	const record = { gets: 0, patches: [] as Record<string, unknown>[] };
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
			record.gets++;
			return route.fulfill({
				json: {
					timezone,
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: darkTheme,
					workflow_sort_mode: 'default',
					workflow_sort_order: workflowSortOrder,
					team_workflow_sort_overrides: {}
				}
			});
		}

		if (path === '/api/preferences' && request.method() === 'PATCH') {
			const patch = request.postDataJSON();
			record.patches.push(patch);
			return route.fulfill({ json: patch });
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

	return record;
}

test('keeps dirty local theme when remote preferences are stale', async ({ page }) => {
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

	const record = await mockProfile(page, 'America/Chicago', 'dark');

	await page.goto('/test/settings/preferences');

	await expect.poll(() => record.gets).toBeGreaterThan(0);
	await expect(page.locator('html')).toHaveClass(/cyber-77/);
	await expect.poll(() => record.patches.at(-1)?.dark_theme).toBe('cyber-77');
	await expect
		.poll(() =>
			page.evaluate(() => JSON.parse(localStorage.getItem('kuayle-preferences') ?? '{}').localDirty)
		)
		.toBe(false);
	expect(record.patches.at(-1)).not.toHaveProperty('timezone');
	await expect(page.getByRole('combobox', { name: 'Timezone' })).toHaveText('America/Chicago');
	await expect(page.locator('html')).toHaveClass(/cyber-77/);
});

for (const [scenario, profileZone] of [
	['uses the saved profile zone on a new browser', 'Pacific/Auckland'],
	['fills an empty profile zone from the browser', ''],
	['leaves older preference responses alone', undefined]
] as const) {
	test(scenario, async ({ page }) => {
		const { patches } = await mockProfile(page, profileZone, 'cyber-77');

		await page.goto('/test/settings/preferences');

		await expect(page.locator('html')).toHaveClass(/cyber-77/);
		if (profileZone === '') {
			const browserZone = await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
			await expect.poll(() => patches).toEqual([{ timezone: browserZone }]);
			await expect(page.getByRole('combobox', { name: 'Timezone' })).toHaveText(browserZone);
		} else {
			await expect(page.getByRole('combobox', { name: 'Timezone' })).toHaveText(profileZone ?? 'UTC');
			expect(patches).toEqual([]);
		}
	});
}
