import { expect, test, type Page } from '@playwright/test';

const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };

async function mockApi(page: Page, calls: string[]) {
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (path.startsWith('/api/workspaces/test/github/')) calls.push(path + url.search);
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/github/callback') return route.fulfill({ json: { id: 'inst-1', account_login: 'acme' } });
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({ json: { mode: 'global', app_configured: true, connected: false } });
		return route.fulfill({ json: [] });
	});
}

// GitHub returns the installing user's OAuth code with the installation. The
// API needs that code to verify the installation belongs to the user, and it
// must not be mistaken for a self-hosted app-manifest code.
test('forwards the installation OAuth code to the installation callback', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, calls);

	await page.goto('/test/settings/github?installation_id=4242&setup_action=install&code=oauth-code');

	await expect.poll(() => calls.find((call) => call.startsWith('/api/workspaces/test/github/callback'))).toBe(
		'/api/workspaces/test/github/callback?installation_id=4242&code=oauth-code'
	);
	expect(calls.some((call) => call.startsWith('/api/workspaces/test/github/setup/callback'))).toBe(false);
	await expect(page).toHaveURL(/\/test\/settings\/github$/);
});
