import { test, expect, type Page } from '@playwright/test';

// KUA-68: issue creation must stay out of settings pages, and the creation
// dialog shares the issue details editor behaviour (AI rework) and can expand.

const teamId = '00000000-0000-0000-0000-000000000010';
const user = { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', logo_url: null, current_user_role: 'owner' };
const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', triage_enabled: false };

async function setup(page: Page) {
	const expansions: Record<string, unknown>[] = [];
	await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
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
		if (path === '/api/workspaces/test/issues/expand-description' && request.method() === 'POST') {
			expansions.push(request.postDataJSON());
			return route.fulfill({ json: { description: '<p>Expanded by AI</p>' } });
		}
		if (path === '/api/workspaces/test/issues') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (/\/api\/workspaces\/test\/teams\/[^/]+\/(statuses|cycles)$/.test(path)) return route.fulfill({ json: [] });
		if (/\/api\/workspaces\/test\/(labels|members|views|favorites|projects|issue-templates)$/.test(path)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: path } } });
	});
	return { expansions };
}

const createDialogTitle = (page: Page) => page.locator('#create-issue-title');

async function focusPage(page: Page) {
	await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
}

function commandPalette(page: Page) {
	const input = page.getByPlaceholder('Type a command or search...');
	return { input, root: page.locator('div.fixed.inset-0').filter({ has: input }) };
}

/** Opens the palette with a key, checks it offers no issue creation, and closes it. */
async function expectPaletteWithoutCreate(page: Page, key: string) {
	const palette = commandPalette(page);
	await focusPage(page);
	await page.keyboard.press(key);
	await expect(palette.input).toBeVisible();
	await expect(palette.root.getByText('Go to Inbox', { exact: true })).toBeVisible();
	await expect(palette.root.getByText('Create issue', { exact: true })).toHaveCount(0);
	// Searching for it and confirming must not create anything either.
	await palette.input.fill('create');
	await page.keyboard.press('Enter');
	await page.waitForTimeout(200);
	await expect(createDialogTitle(page)).toHaveCount(0);
	await page.keyboard.press('Escape');
	await expect(palette.input).toHaveCount(0);
}

async function waitForTeams(page: Page) {
	await expect(page.locator('nav').getByText('Engineering', { exact: true }).first()).toBeVisible();
}

test('settings pages ignore the create shortcut and keep native paste', async ({ page }) => {
	await setup(page);
	await page.goto('/test/settings/profile');
	const name = page.locator('input[type="text"]').first();
	await expect(name).toHaveValue('Test User');

	// Outside any field: neither C nor copy/paste chords open issue creation.
	await focusPage(page);
	await page.keyboard.press('c');
	await page.keyboard.press('ControlOrMeta+c');
	await page.keyboard.press('ControlOrMeta+v');
	await page.waitForTimeout(200);
	await expect(createDialogTitle(page)).toHaveCount(0);

	// The command palette still opens (Cmd/Ctrl+K and /) but offers no issue creation.
	await expectPaletteWithoutCreate(page, 'ControlOrMeta+k');
	await expectPaletteWithoutCreate(page, '/');

	// Inside a settings input, paste and typing stay with the input.
	await name.fill('');
	const pasteHandledElsewhere = await name.evaluate((input) => {
		const data = new DataTransfer();
		data.setData('text/plain', 'Pasted name');
		const event = new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true });
		input.dispatchEvent(event);
		return event.defaultPrevented;
	});
	expect(pasteHandledElsewhere).toBe(false);
	await page.keyboard.press('ControlOrMeta+v');
	await page.keyboard.type('c');
	await expect(name).toHaveValue(/c$/);
	await page.waitForTimeout(200);
	await expect(createDialogTitle(page)).toHaveCount(0);
});

test('the create shortcut still works outside settings, but not on copy', async ({ page }) => {
	await setup(page);
	await page.goto(`/test/teams/${teamId}`);
	await waitForTeams(page);
	await focusPage(page);

	await page.keyboard.press('ControlOrMeta+c');
	await page.waitForTimeout(200);
	await expect(createDialogTitle(page)).toHaveCount(0);

	await page.keyboard.press('c');
	await expect(createDialogTitle(page)).toBeFocused();
	await page.keyboard.press('Escape');
	await expect(createDialogTitle(page)).toHaveCount(0);

	// Outside settings the palette keeps its create command.
	const palette = commandPalette(page);
	await focusPage(page);
	await page.keyboard.press('ControlOrMeta+k');
	await palette.root.getByText('Create issue', { exact: true }).click();
	await expect(createDialogTitle(page)).toBeFocused();
	await page.keyboard.press('Escape');
	await expect(createDialogTitle(page)).toHaveCount(0);

	// Leaving settings restores the shortcut.
	await page.goto('/test/settings/profile');
	await expect(page.locator('input[type="text"]').first()).toHaveValue('Test User');
	await focusPage(page);
	await page.keyboard.press('c');
	await page.waitForTimeout(200);
	await expect(createDialogTitle(page)).toHaveCount(0);
	await page.goto('/test/my-issues');
	await waitForTeams(page);
	await focusPage(page);
	await page.keyboard.press('c');
	await expect(createDialogTitle(page)).toBeFocused();
});

test('the creation dialog is wider than before and can expand', async ({ page }) => {
	await page.setViewportSize({ width: 1440, height: 900 });
	await setup(page);
	await page.goto(`/test/teams/${teamId}`);
	await page.getByRole('button', { name: 'New issue', exact: true }).click();
	const dialog = page.getByRole('dialog');
	await expect(createDialogTitle(page)).toBeFocused();

	const width = async () => (await dialog.boundingBox())!.width;
	const normal = await width();
	expect(normal).toBeGreaterThan(700);

	const expand = dialog.getByRole('button', { name: 'Expand', exact: true });
	await expand.click();
	await expect(dialog.getByRole('button', { name: 'Collapse', exact: true })).toHaveAttribute('aria-pressed', 'true');
	await expect.poll(width).toBeGreaterThan(normal + 300);
	expect((await dialog.boundingBox())!.height).toBeGreaterThan(780);

	await dialog.getByRole('button', { name: 'Collapse', exact: true }).click();
	await expect.poll(width).toBeLessThan(normal + 1);

	// Footers keep a compact horizontal inset.
	const footer = dialog.locator('[data-slot="dialog-footer"]');
	await expect(footer).toHaveCSS('padding-left', '12px');
	await expect(footer).toHaveCSS('padding-right', '12px');

	await dialog.getByRole('button', { name: 'Close', exact: true }).click();
	await expect(createDialogTitle(page)).toHaveCount(0);
});

test('AI rework is available while creating an issue', async ({ page }) => {
	const { expansions } = await setup(page);
	await page.goto(`/test/teams/${teamId}`);
	await page.getByRole('button', { name: 'New issue', exact: true }).click();
	await createDialogTitle(page).fill('Login is broken');
	const editor = page.getByRole('dialog').locator('.ProseMirror');
	await editor.click();
	await page.keyboard.type('fix login');
	await editor.locator('p').first().evaluate((element) => {
		const range = document.createRange();
		range.selectNodeContents(element);
		const selection = window.getSelection()!;
		selection.removeAllRanges();
		selection.addRange(range);
	});
	await page.getByTitle('Rework with AI', { exact: true }).click();
	await expect(editor).toContainText('Expanded by AI');
	await expect(editor).not.toContainText('fix login');
	expect(expansions).toHaveLength(1);
	expect(expansions[0]).toMatchObject({ title: 'Login is broken', selected_text: 'fix login' });
	expect(String(expansions[0].description)).toContain('fix login');
});
