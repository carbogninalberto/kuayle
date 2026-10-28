import { test, expect, type Page } from '@playwright/test';
async function setup(page: Page, owner = true, linked: number | null = null) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test', current_user_role: owner ? 'owner' : 'member' };
	const status = {
		configured: true,
		owner,
		enabled: true,
		bot_username: 'fixture_bot',
		status: 'connected',
		allowed_user_ids: [12345],
		linked_user_id: linked
	};
	const writes: Record<string, unknown>[] = [];
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Test', email: 'fixture@example.test' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path.endsWith('/telegram/link'))
			return route.fulfill({ json: { command: '/start one-time-fixture', expires_in: 600 } });
		if (path.endsWith('/telegram')) {
			if (route.request().method() === 'PATCH') {
				const body = route.request().postDataJSON();
				writes.push(body);
				Object.assign(status, {
					configured: body.bot_token !== null,
					enabled: body.enabled,
					allowed_user_ids: body.allowed_user_ids,
					status: body.enabled ? 'connected' : 'disabled'
				});
			}
			return route.fulfill({ json: status });
		}
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path.endsWith('/issues'))
			return route.fulfill({
				json: { issues: [], pagination: { page: 1, page_size: 50, total_count: 0, total_pages: 0 } }
			});
		return route.fulfill({ json: [] });
	});
	return writes;
}
test('configures token and allowed IDs, keeps stored token private and supports removal', async ({ page }) => {
	const writes = await setup(page);
	await page.goto('/test/settings/telegram');
	await expect(page.getByRole('heading', { name: 'Telegram', exact: true })).toBeVisible();
	await expect(page.getByLabel('Bot token', { exact: true })).toHaveValue('');
	await page.getByLabel('Bot token', { exact: true }).fill('fixture-token-not-a-credential');
	await page.getByLabel('Allowed Telegram user IDs').fill('12345\n67890');
	await page.getByRole('button', { name: 'Save Telegram settings' }).click();
	await expect.poll(() => writes.length).toBe(1);
	expect(writes[0]).toMatchObject({ bot_token: 'fixture-token-not-a-credential', allowed_user_ids: [12345, 67890] });
	await expect(page.getByLabel('Bot token', { exact: true })).toHaveValue('');
	await page.getByLabel('Allowed Telegram user IDs').fill('not-an-id');
	await expect(page.getByRole('button', { name: 'Save Telegram settings' })).toBeDisabled();
	await page.getByRole('button', { name: 'Remove bot token' }).click();
	await expect.poll(() => writes.length).toBe(2);
	expect(writes[1].bot_token).toBeNull();
});
test('member can link their own account without accessing bot credentials', async ({ page }) => {
	await setup(page, false);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/telegram');
	await expect(page.getByLabel('Bot token', { exact: true })).toHaveCount(0);
	await page.getByLabel('Your Telegram user ID').fill('12345');
	await page.getByRole('button', { name: 'Generate link command' }).click();
	await expect(page.getByText('/start one-time-fixture', { exact: true })).toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await page.screenshot({ path: test.info().outputPath('telegram-mobile.png'), fullPage: true });
});

test('keeps a successful unlink visible when the refresh fails', async ({ page }) => {
	await setup(page, false, 12345);
	await page.goto('/test/settings/telegram');
	await expect(page.getByText('Linked Telegram user: 12345')).toBeVisible();
	await page.route('**/telegram/link', (route) => route.fulfill({ status: 204 }));
	await page.route('**/telegram', (route) => route.fulfill({ status: 500, json: { error: { code: 'FAILED' } } }));
	await page.getByRole('button', { name: 'Unlink account' }).click();
	await expect(page.getByText('Linked Telegram user: 12345')).toHaveCount(0);
	await expect(page.getByLabel('Your Telegram user ID')).toBeVisible();
});
