import { test, expect, type Page, type Locator } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const user = { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: 'owner' };
const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', triage_enabled: false };

async function setup(page: Page, theme: 'light' | 'dark') {
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: theme, light_theme: 'light', dark_theme: 'dark', issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/workspaces/test/issue-templates') return route.fulfill({ json: [{ id: 'template', title: 'Template issue', description: '<p>Template description</p>', priority: 0, label_ids: [] }] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/views') return route.fulfill({ json: [{ id: 'view', name: 'My view', filters: {}, is_shared: false }] });
		if (/\/api\/workspaces\/test\/(projects|labels|members)$/.test(path) || path === `/api/workspaces/test/teams/${teamId}/statuses` || path === `/api/workspaces/test/teams/${teamId}/cycles`) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: path } } });
	});
	await page.goto(`/test/teams/${teamId}`);
	await expect(page.locator('html')).toHaveClass(new RegExp(theme));
}

async function expectAlignedWithNavigation(element: Locator) {
	// Read both positions in the same frame while the mobile sheet is animating.
	await expect.poll(() => element.evaluate((node) => {
		const inbox = node.closest('nav')!.querySelector('a[href="/test/inbox"] svg')!;
		return Math.abs(node.getBoundingClientRect().x - inbox.getBoundingClientRect().x);
	})).toBeLessThanOrEqual(2);
}

for (const theme of ['light', 'dark'] as const) {
	for (const width of [1280, 390]) {
		test(`issue title remains prominent and unboxed at ${width}px in ${theme}`, async ({ page }) => {
			await page.setViewportSize({ width, height: 900 });
			await setup(page, theme);
			const trigger = page.getByRole('button', { name: width < 640 ? 'Create issue' : 'New issue', exact: true }).first();
			await trigger.click();
			const title = page.locator('#create-issue-title');
			await expect(title).toBeFocused();
			await expect(title).toHaveCSS('font-size', '18px');
			await expect(title).toHaveCSS('font-weight', '600');
			await expect(title).not.toHaveCSS('box-shadow', 'none');
			await expect(title).toHaveCSS('border-top-width', '0px');
			await expect(title).toHaveCSS('padding-left', '0px');
			await expect(title).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
			await title.fill('A prominent issue title');
			await title.press('Tab');
			await expect(title).not.toBeFocused();
			await page.screenshot({ path: test.info().outputPath('issue-title.png') });
			await page.keyboard.press('Escape');
			await expect(title).toHaveCount(0);
		});

		test(`sidebar rows align with navigation at ${width}px in ${theme}`, async ({ page }) => {
			await page.setViewportSize({ width, height: 900 });
			await setup(page, theme);
			if (width < 640) await page.getByRole('button', { name: 'Open navigation' }).click();
			const nav = page.locator('nav:visible');
			const inbox = nav.getByRole('link', { name: 'Inbox', exact: true });
			await expect(inbox).toBeVisible();
			for (const name of ['Teams', 'Views', 'Projects']) {
				const label = nav.getByRole('button', { name, exact: true }).getByText(name, { exact: true });
				await expect(label).toBeVisible();
				await expectAlignedWithNavigation(label);
			}
			const teamButton = nav.getByRole('button', { name: 'Engineering', exact: true });
			const icon = teamButton.locator('svg').first();
			await expectAlignedWithNavigation(icon);
			await expect(icon).toHaveCSS('width', '16px');
			await page.screenshot({ path: test.info().outputPath('sidebar.png'), animations: 'disabled' });
			await teamButton.focus();
			await page.keyboard.press('Enter');
			await expect(nav.getByRole('link', { name: 'Issues', exact: true })).toHaveCount(0);
			await page.keyboard.press('Enter');
			await expect(nav.getByRole('link', { name: 'Issues', exact: true })).toBeVisible();
		});
	}
}
