import { expect, test, type Page, type Route } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
const otherWorkspace = { id: 'workspace-2', slug: 'other', name: 'Other Workspace' };
const token = {
	id: 'token-1',
	name: 'CLI token',
	token_prefix: 'kuayle_pat_demo',
	scopes: ['issues:read'],
	workspace_slugs: null,
	expires_at: null,
	last_used_at: null,
	created_at: '2026-01-01T00:00:00Z'
};
const secret = 'kuayle_pat_test_plaintext_only_shown_once_0123456789';

type TokenHandler = (route: Route) => Promise<void>;
async function mockApi(page: Page, handleTokens: TokenHandler, failWorkspaces = false) {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path.startsWith('/api/tokens')) return handleTokens(route);
		if (path === '/api/auth/me')
			return route.fulfill({
				json: {
					id: 'user-1',
					name: 'Test User',
					display_name: 'Test User',
					email: 'test@example.com',
					avatar_url: null
				}
			});
		if (path === '/api/workspaces')
			return failWorkspaces
				? route.fulfill({ status: 500, json: { error: { message: 'Unavailable' } } })
				: route.fulfill({ json: [workspace, otherWorkspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
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
					team_workflow_sort_overrides: {}
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
}

test('creates a workspace-restricted token with exact preset scopes and protects its one-time display', async ({
	page,
	context
}) => {
	let payload: Record<string, unknown> | undefined;
	let release!: () => void;
	let started!: () => void;
	const waitForRelease = new Promise<void>((resolve) => {
		release = resolve;
	});
	const requestStarted = new Promise<void>((resolve) => {
		started = resolve;
	});
	await mockApi(page, async (route) => {
		if (route.request().method() === 'GET') return route.fulfill({ json: [] });
		payload = route.request().postDataJSON();
		started();
		await waitForRelease;
		await route.fulfill({ json: { ...token, ...payload, token: secret } });
	});
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	await page.goto('/test/settings/tokens');
	await page.getByRole('button', { name: 'New token', exact: true }).first().click();
	const createDialog = page.getByRole('dialog', { name: 'Create token', exact: true });
	await createDialog.getByLabel('Name', { exact: true }).fill('CLI token');
	await createDialog.getByRole('button', { name: 'Preset', exact: true }).click();
	await page.getByRole('option', { name: 'Developer (CLI)' }).click();
	await createDialog.getByRole('checkbox', { name: 'Test Workspace' }).check();
	await createDialog.getByRole('button', { name: 'Create token', exact: true }).click();
	await requestStarted;
	await expect(createDialog.getByRole('button', { name: 'Cancel' })).toBeDisabled();
	await page.keyboard.press('Escape');
	await expect(createDialog).toBeVisible();
	release();
	const createdDialog = page.getByRole('dialog', { name: 'Token created', exact: true });
	await expect(createdDialog).toBeVisible();
	await expect(createdDialog.locator('code')).toHaveText(secret);
	expect(payload?.workspace_slugs).toEqual(['test']);
	expect(payload?.scopes).toEqual(
		expect.arrayContaining(['issue:create', 'issue:update', 'label:manage', 'issues:read'])
	);
	expect(payload?.scopes).not.toContain('issue:delete_own');
	expect(payload?.scopes).not.toContain('workspace:manage');
	expect(new Date(payload?.expires_at as string).getTime()).toBeGreaterThan(Date.now());
	await page.keyboard.press('Escape');
	await expect(createdDialog).toBeVisible();
	await createdDialog.getByRole('button', { name: 'Copy', exact: true }).click();
	await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(secret);
	await createdDialog.getByRole('button', { name: 'Done', exact: true }).click();
	const confirmDialog = page.getByRole('dialog', { name: /saved the token/i });
	await expect(confirmDialog).toBeVisible();
	await confirmDialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(createdDialog.locator('code')).toHaveText(secret);
	await createdDialog.getByRole('button', { name: 'Done', exact: true }).click();
	await page
		.getByRole('dialog')
		.getByRole('button', { name: /saved|stored/i })
		.click();
	await expect(page.getByRole('dialog')).toHaveCount(0);
	await expect(page.getByText(secret)).toHaveCount(0);
	await expect(page.getByText('CLI token', { exact: true })).toBeVisible();
});

test('keeps failed revoke open and removes the correct token only after successful revocation', async ({ page }) => {
	let attempts = 0;
	await mockApi(page, async (route) => {
		if (route.request().method() === 'GET') return route.fulfill({ json: [token] });
		expect(new URL(route.request().url()).pathname).toBe('/api/tokens/token-1');
		attempts += 1;
		return attempts === 1
			? route.fulfill({ status: 500, json: { error: { message: 'Try again' } } })
			: route.fulfill({ status: 204 });
	});
	await page.goto('/test/settings/tokens');
	await page.getByRole('button', { name: 'Revoke token', exact: true }).click();
	const confirmation = page.getByRole('alertdialog', { name: 'Revoke token?' });
	await confirmation.getByRole('button', { name: 'Revoke token', exact: true }).click();
	await expect(confirmation).toBeVisible();
	await expect(confirmation.getByRole('button', { name: 'Revoke token', exact: true })).toBeEnabled();
	await confirmation.getByRole('button', { name: 'Revoke token', exact: true }).click();
	await expect(confirmation).toHaveCount(0);
	await expect(page.getByText('CLI token', { exact: true })).toHaveCount(0);
	await expect(page.getByText('No tokens yet', { exact: true })).toBeVisible();
	expect(attempts).toBe(2);
});

test('blocks submission if workspace permissions cannot be loaded', async ({ page }) => {
	await mockApi(page, (route) => route.fulfill({ json: [] }), true);
	await page.goto('/test/settings/tokens');
	await page.getByRole('button', { name: 'New token', exact: true }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Create token', exact: true });
	await dialog.getByLabel('Name', { exact: true }).fill('Do not broaden access');
	await expect(dialog.getByRole('alert')).toHaveText('Failed to load workspaces');
	await expect(dialog.getByRole('button', { name: 'Create token', exact: true })).toBeDisabled();
	await expect(dialog.getByRole('button', { name: 'Retry' })).toBeVisible();
});

test('renders token settings and navigation in Italian', async ({ page }) => {
	await page.addInitScript(() => localStorage.setItem('PARAGLIDE_LOCALE', 'it'));
	await mockApi(page, (route) => route.fulfill({ json: [] }));
	await page.goto('/test/settings/tokens');
	await expect(page.getByRole('heading', { name: 'Token API', exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Token API', exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Nuovo token', exact: true }).first().click();
	await expect(page.getByRole('dialog', { name: 'Crea token', exact: true })).toBeVisible();
	await expect(page.getByLabel('Nome', { exact: true })).toBeVisible();
});

test('validates custom expiration and preserves custom permissions across a failed create', async ({ page }) => {
	let attempts = 0;
	let payload: Record<string, unknown> | undefined;
	await mockApi(page, async (route) => {
		if (route.request().method() === 'GET') return route.fulfill({ json: [] });
		attempts += 1;
		payload = route.request().postDataJSON();
		if (attempts === 1) return route.fulfill({ status: 500, json: { error: { message: 'Try again' } } });
		return route.fulfill({ json: { ...token, ...payload, token: secret } });
	});
	await page.goto('/test/settings/tokens');
	await page.getByRole('button', { name: 'New token', exact: true }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Create token', exact: true });
	await dialog.getByLabel('Name', { exact: true }).fill('Custom workflow');
	await dialog.getByRole('button', { name: 'Issues', exact: true }).click();
	await page.getByRole('option', { name: 'No access', exact: true }).click();
	await expect(dialog.getByRole('button', { name: 'Preset', exact: true })).toHaveText(/Custom/);
	await dialog.getByRole('button', { name: 'Expiration', exact: true }).click();
	await page.getByRole('option', { name: 'Custom date', exact: true }).click();
	await expect(dialog.getByRole('button', { name: 'Create token', exact: true })).toBeDisabled();
	const date = new Date(Date.now() + 30 * 86400000).toISOString().slice(0, 10);
	await dialog.getByLabel('Custom date', { exact: true }).fill(date);
	await dialog.getByRole('button', { name: 'Create token', exact: true }).click();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('Custom workflow');
	await expect(dialog.getByRole('button', { name: 'Create token', exact: true })).toBeEnabled();
	await dialog.getByRole('button', { name: 'Create token', exact: true }).click();
	await expect(page.getByRole('dialog', { name: 'Token created', exact: true })).toBeVisible();
	expect(payload?.workspace_slugs).toBeUndefined();
	expect(payload?.scopes).not.toContain('issues:read');
	expect(payload?.scopes).not.toContain('issue:create');
	expect(payload?.scopes).toContain('comments:read');
	expect(payload?.expires_at).toBeDefined();
	expect(attempts).toBe(2);
});

test('retries a failed token list without treating it as an empty list', async ({ page }) => {
	let attempts = 0;
	await mockApi(page, (route) => {
		attempts += 1;
		return attempts === 1
			? route.fulfill({ status: 500, json: { error: { message: 'Unavailable' } } })
			: route.fulfill({ json: [token] });
	});
	await page.goto('/test/settings/tokens');
	await expect(page.getByRole('main').getByRole('alert')).toHaveText('Failed to load tokens');
	await expect(page.getByText('No tokens yet', { exact: true })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'New token', exact: true })).toBeDisabled();
	await page.getByRole('button', { name: 'Retry', exact: true }).click();
	await expect(page.getByText('CLI token', { exact: true })).toBeVisible();
	await expect(page.getByRole('main').getByRole('alert')).toHaveCount(0);
	expect(attempts).toBe(2);
});
