import { expect, test, type Page } from '@playwright/test';

// KUA-139: the shortcut engine reads its definitions through a getter, so both
// translated labels and handlers that capture the reactive workspace slug stay
// current. The language test below is only a smoke check: setLocale() persists
// the locale and reloads the page by design, so it would pass with a stale
// engine too. The workspace-switch test is the one that guards the getter.

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
const otherWorkspace = {
	id: '00000000-0000-0000-0000-000000000003',
	name: 'Other Workspace',
	slug: 'other',
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

async function setup(page: Page, workspaces = [workspace]) {
	await page.routeWebSocket('**/api/workspaces/*/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: workspaces });
		for (const ws of workspaces) {
			if (path === `/api/workspaces/${ws.slug}`) return route.fulfill({ json: ws });
			if (path === `/api/workspaces/${ws.slug}/teams`) return route.fulfill({ json: [team] });
			if (path === `/api/workspaces/${ws.slug}/issues`) {
				return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
			}
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (/^\/api\/workspaces\/[^/]+\/(labels|members|views|favorites|projects|issue-templates|statuses|cycles)$/.test(path)) {
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

test('shortcut labels are translated after switching language (smoke check; setLocale reloads by design)', async ({ page }) => {
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
	await expect(page.getByRole('button', { name: /Vai all'Inbox/ })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(paletteInput).toHaveCount(0);
});

test('navigation shortcuts follow the current workspace after an in-app switch', async ({ page }) => {
	await setup(page, [workspace, otherWorkspace]);
	await page.goto('/test/my-issues');
	await expect(page.locator('nav').first()).toBeVisible();

	// Switch in-app: SvelteKit reuses the [workspaceSlug] layout, so a stale
	// engine would keep the slug captured when the page first loaded.
	await page.getByRole('button', { name: /Test Workspace/ }).click();
	await page.getByRole('button', { name: /Other Workspace/ }).click();
	await expect(page).toHaveURL(/\/other\/my-issues/);

	await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
	await page.keyboard.press('g');
	await page.keyboard.press('i');
	await expect(page).toHaveURL(/\/other\/inbox/);
});
