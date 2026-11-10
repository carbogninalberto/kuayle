import { expect, test, type Page } from '@playwright/test';

const appSettings = 'https://github.com/organizations/acme/settings/apps/kuayle-acme/permissions';
const installation = 'https://github.com/organizations/acme/settings/installations/77';

const steps = {
	app_missing: {
		state: 'app_missing',
		missing_events: ['pull_request_review', 'check_suite'],
		missing_permissions: [{ name: 'checks', level: 'read' }]
	},
	installation_pending: {
		state: 'installation_pending',
		missing_events: ['pull_request_review', 'check_suite'],
		missing_permissions: [{ name: 'checks', level: 'read' }]
	},
	ok: { state: 'ok', missing_events: [], missing_permissions: [] },
	installation_suspended: { state: 'installation_suspended', missing_events: [], missing_permissions: [] }
} as const;

async function mockApi(page: Page, role: string, setup: () => keyof typeof steps, calls: string[]) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({
				json: {
					configured: true,
					installed: true,
					global_app: false,
					app_slug: 'kuayle-acme',
					installation: {
						id: 'inst-1',
						installation_id: 77,
						account_login: 'acme',
						account_type: 'Organization',
						created_at: '2026-01-01T00:00:00Z'
					},
					repos: []
				}
			});
		if (path === '/api/workspaces/test/github/app-setup') {
			calls.push(path);
			return route.fulfill({
				json: {
					managed: false,
					app_slug: 'kuayle-acme',
					account_login: 'acme',
					app_settings_url: appSettings,
					installation_url: installation,
					...steps[setup()]
				}
			});
		}
		return route.fulfill({ json: [] });
	});
}

// GitHub has no API for an app to change its own events, so the owner fixes
// the app, confirms GitHub's dialog, then accepts on the installation; the
// card follows each step via "Check again" and window focus.
test('guides the owner from a missing app setting to accepted permissions', async ({ page }) => {
	let step: keyof typeof steps = 'app_missing';
	const calls: string[] = [];
	await mockApi(page, 'owner', () => step, calls);
	await page.goto('/test/settings/github');

	const card = page.getByTestId('github-app-setup');
	await expect(card).toContainText("Kuayle can't see CI results and review requests");
	await expect(card.getByRole('listitem').filter({ hasText: 'Checks: Read-only' })).toBeVisible();
	await expect(card.getByRole('listitem').filter({ hasText: 'Pull request review' })).toBeVisible();
	await expect(card.getByRole('listitem').filter({ hasText: 'Check suite' })).toBeVisible();
	await expect(card).toContainText('confirm "Update permissions"');
	const fix = card.getByRole('link', { name: /Fix on GitHub/ });
	await expect(fix).toHaveAttribute('href', appSettings);
	await expect(fix).toHaveAttribute('target', '_blank');

	step = 'installation_pending';
	await card.getByRole('button', { name: 'Check again' }).click();
	await expect(card).toContainText('GitHub is waiting for you to accept the new permissions for acme.');
	await expect(card).toContainText('Review request, then Accept new permissions');
	await expect(card.getByRole('link', { name: /Accept on GitHub/ })).toHaveAttribute('href', installation);

	step = 'ok';
	const before = calls.length;
	await page.evaluate(() => window.dispatchEvent(new Event('focus')));
	await expect(card).toContainText('Agents now receive CI results and review requests.');
	expect(calls.length).toBeGreaterThan(before);
	// Nothing is left to fix: the only links are the logo ones, since GitHub has
	// no API for an app's logo and the owner uploads it by hand.
	const logo = card.getByTestId('github-app-logo');
	await expect(logo.getByRole('link', { name: 'Download Kuayle logo' })).toHaveAttribute('href', '/icons/icon-512.png');
	const updateLogo = logo.getByRole('link', { name: /Update logo on GitHub/ });
	await expect(updateLogo).toHaveAttribute('href', appSettings.replace(/\/permissions$/, ''));
	await expect(updateLogo).toHaveAttribute('target', '_blank');
	await expect(card.getByRole('link')).toHaveCount(2);

	// Once OK, regaining focus does not keep asking GitHub.
	const settled = calls.length;
	await page.evaluate(() => window.dispatchEvent(new Event('focus')));
	await page.waitForTimeout(200);
	expect(calls.length).toBe(settled);
});

test('explains a suspended installation', async ({ page }) => {
	await mockApi(page, 'owner', () => 'installation_suspended', []);
	await page.goto('/test/settings/github');
	const card = page.getByTestId('github-app-setup');
	await expect(card).toContainText('installation for acme is suspended');
	await expect(card.getByRole('link', { name: /Open installation on GitHub/ })).toHaveAttribute('href', installation);
});

test('is not shown to members who cannot manage the workspace', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, 'admin', () => 'app_missing', calls);
	await page.goto('/test/settings/github');
	await expect(page.getByText('GitHub App connected')).toBeVisible();
	await expect(page.getByTestId('github-app-setup')).toHaveCount(0);
	expect(calls).toEqual([]);
});
