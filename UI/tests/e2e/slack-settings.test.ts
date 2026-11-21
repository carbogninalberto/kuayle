import { expect, test, type Page } from '@playwright/test';

async function workspace(page: Page, role: string) {
	const ws = { id: 'workspace-1', slug: 'test', name: 'Product', current_user_role: role };
	await page.route('https://raw.githubusercontent.com/**', (r) => r.fulfill({ json: [] }));
	await page.route('**/api/**', (route) => {
		const path = new URL(route.request().url()).pathname;
		const fixtures: Record<string, unknown> = {
			'/api/auth/me': { id: 'user-1', name: 'Test user', email: 'user@example.test' },
			'/api/preferences': { font_size: 'default', theme_mode: 'dark', dark_theme: 'dark', light_theme: 'light', pointer_cursors: true },
			'/api/workspaces': [ws], '/api/workspaces/test': ws,
			'/api/workspaces/test/teams': [],
			'/api/workspaces/test/projects': [{ id: 'p1', name: 'Release Engineering' }],
			'/api/notifications': { notifications: [], unread_count: 0 }
		};
		return route.fulfill({ json: fixtures[path] ?? [] });
	});
}

test('an admin selects and clears a project channel', async ({ page }) => {
	await workspace(page, 'admin');
	await page.route('**/test/slack', (r) => r.fulfill({ json: { configured: true, install: { name: 'Product Engineering' }, bindings: [] } }));
	await page.route('**/slack/channels', (r) => r.fulfill({ json: [{ id: 'C1', name: 'releases' }] }));
	const writes: unknown[] = [];
	await page.route('**/slack/bindings', (r) => {
		writes.push(r.request().postDataJSON());
		return r.fulfill({ status: 204 });
	});
	await page.goto('/test/settings/slack');
	const selector = page.getByRole('combobox', { name: 'Channel' });
	await selector.focus();
	await page.keyboard.press('Enter');
	await page.getByRole('option', { name: '#releases' }).click();
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	expect(writes).toEqual([{ project_id: 'p1', channel_id: 'C1' }]);
	await selector.click();
	await page.getByRole('option', { name: 'None', exact: true }).click();
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect.poll(() => writes.length).toBe(2);
	expect(writes[1]).toEqual({ project_id: 'p1', channel_id: '' });
});

test('member cannot open Slack settings or request installation details', async ({ page }) => {
	await workspace(page, 'member');
	let calls = 0;
	await page.route('**/test/slack**', (r) => { calls++; return r.fulfill({ status: 403 }); });
	await page.goto('/test/settings/slack');
	await expect(page.getByText('No access', { exact: true })).toBeVisible();
	expect(calls).toBe(0);
});

test('a callback from another tab cannot submit an installation code', async ({ page }) => {
	await workspace(page, 'admin');
	let exchanges = 0;
	await page.route('**/api/**/slack/callback', (r) => { exchanges++; return r.fulfill({ status: 204 }); });
	await page.goto('/integrations/slack/callback?state=unmatched&code=test');
	await expect(page.getByRole('alert').filter({ hasText: 'Connection failed' })).toBeVisible();
	await expect(page).toHaveURL(/\/integrations\/slack\/callback$/);
	expect(exchanges).toBe(0);
});

test('the install returns to the initiating workspace after consent', async ({ page }) => {
	await workspace(page, 'admin');
	let connected = false;
	await page.route('**/test/slack', (r) => r.fulfill({ json: {
		configured: true, install: connected ? { name: 'Product Engineering' } : null, bindings: []
	} }));
	await page.route('**/slack/install', (r) => r.fulfill({ json: { url: 'https://slack.com/oauth/v2/authorize?state=one-use' } }));
	await page.route('https://slack.com/oauth/v2/authorize?**', (r) => r.fulfill({ contentType: 'text/html', body: 'Consent' }));
	await page.route('**/api/**/slack/callback', (r) => {
		expect(r.request().postDataJSON()).toEqual({ state: 'one-use', code: 'test' });
		connected = true;
		return r.fulfill({ status: 204 });
	});
	await page.goto('/test/settings/slack');
	await page.getByRole('button', { name: 'Connect', exact: true }).click();
	await expect(page).toHaveURL(/^https:\/\/slack.com\//);
	await page.goto('/integrations/slack/callback?state=one-use&code=test');
	await expect(page).toHaveURL(/\/test\/settings\/slack$/);
	await expect(page.getByText(/Product Engineering/)).toBeVisible();
});
