import { expect, test, type Page } from '@playwright/test';

// KUA-139: the shortcut engine must read its definitions lazily so translated
// labels follow the selected language. setLocale() persists the locale and then
// reloads the page by design, so the language switch below goes through that reload.

const user = {
	id: '00000000-0000-0000-0000-000000000001',
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
	current_user_role: 'owner'
};
const team = {
	id: '00000000-0000-0000-0000-000000000010',
	name: 'Engineering',
	key: 'ENG',
	color: '#6366f1',
	icon: 'layers',
	triage_enabled: false
};

async function setup(page: Page) {
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues') {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		if (/\/api\/workspaces\/test\/(labels|members|views|favorites|projects|issue-templates|statuses|cycles)$/.test(path)) {
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ json: [] });
	});
}

async function openShortcutHelp(page: Page) {
	await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
	await page.keyboard.press('Shift+?');
	const dialog = page.getByRole('dialog');
	await expect(dialog).toBeVisible();
	return dialog;
}

test('shortcut labels and command palette follow the selected language', async ({ page }) => {
	await setup(page);
	await page.goto('/test/my-issues');
	await expect(page.locator('nav').first()).toBeVisible();

	const help = await openShortcutHelp(page);
	await expect(help).toContainText('Go to Inbox');
	await page.keyboard.press('Escape');
	await expect(help).toHaveCount(0);

	// Selecting Italiano calls setLocale, which reloads by design. Wait for the
	// reloaded Preferences page to show Italian before going back to the workspace.
	await page.goto('/test/settings/preferences');
	const language = page.getByRole('combobox').filter({ hasText: 'English' });
	await language.click();
	await page.getByRole('option', { name: /Italiano/ }).click();
	await expect(page.getByRole('combobox').filter({ hasText: 'Italiano' })).toBeVisible();

	await page.goto('/test/my-issues');
	await expect(page.locator('nav').first()).toBeVisible();

	const translatedHelp = await openShortcutHelp(page);
	await expect(translatedHelp).toContainText("Vai all'Inbox");
	await expect(translatedHelp).not.toContainText('Go to Inbox');
	await page.keyboard.press('Escape');
	await expect(translatedHelp).toHaveCount(0);

	await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
	await page.keyboard.press('ControlOrMeta+k');
	const paletteInput = page.getByPlaceholder('Digita un comando o cerca...');
	await expect(paletteInput).toBeVisible();
	const palette = page.locator('div.fixed.inset-0').filter({ has: paletteInput });
	await expect(palette.getByRole('button', { name: /Vai all'Inbox/ })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(paletteInput).toHaveCount(0);
});
