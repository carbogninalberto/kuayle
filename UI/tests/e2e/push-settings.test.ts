import { expect, test, type Page } from '@playwright/test';

async function setup(page: Page, admin: boolean, environment = false) {
	const changes: unknown[] = [];
	let status = { source: environment ? 'environment' : 'off', read_only: environment,
		public_key: environment ? 'environment-public' : '', subject: environment ? 'https://example.org' : '',
		enabled: environment, subscribed_devices: 2 };
	await page.routeWebSocket('**/api/workspaces/*/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', route => route.fulfill({ json: [] }));
	await page.route('**/api/**', async route => {
		const req = route.request();
		const path = new URL(req.url()).pathname;
		const workspace = { id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'member' };
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user', name: 'Member', is_sysadmin: admin } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/alpha') return route.fulfill({ json: workspace });
		if (path.startsWith('/api/system/push')) {
			if (!admin) return route.fulfill({ status: 403, json: {} });
			if (path.endsWith('/test')) return route.fulfill({ json: { sent: 1, failed: 1 } });
			if (path.endsWith('/keys')) {
				changes.push(req.postDataJSON());
				status = { ...status, public_key: 'generated-public', source: 'app' };
				return route.fulfill({ json: { ...status, requires_reenrollment: req.postDataJSON().confirm } });
			}
			if (req.method() === 'PUT') {
				changes.push(req.postDataJSON());
				status = { ...status, ...req.postDataJSON() };
			}
			return route.fulfill({ json: status });
		}
		if (path === '/api/preferences') return route.fulfill({ json: {} });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
	return changes;
}

test('sysadmin configures and tests push, then confirms rotation', async ({ page }) => {
	const changes = await setup(page, true);
	await page.goto('/alpha/settings/version');
	await page.getByRole('link', { name: 'Push notifications', exact: true }).click();
	await page.getByRole('button', { name: 'Generate key pair', exact: true }).click();
	await expect(page.getByText('generated-public')).toBeVisible();
	await page.getByLabel('Contact').fill('mailto:admin@example.org');
	await page.getByRole('switch').click();
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await page.getByRole('button', { name: 'Send test notification' }).click();
	await expect(page.getByText('Sent: 1. Failed: 1.')).toBeVisible();
	await page.getByRole('button', { name: 'Regenerate key pair' }).click();
	await expect(page.getByRole('alertdialog')).toContainText('Every device must enable push again');
	await page.getByRole('button', { name: 'Cancel' }).click();
	expect(changes).toEqual([{ confirm: false }, { subject: 'mailto:admin@example.org', enabled: true }]);
	await page.getByRole('button', { name: 'Regenerate key pair' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Regenerate key pair' }).click();
	await expect.poll(() => changes.at(-1)).toEqual({ confirm: true });
});

test('members see no push settings link or controls on direct navigation', async ({ page }) => {
	await setup(page, false);
	await page.goto('/alpha/settings/version');
	await expect(page.getByRole('link', { name: 'Version' })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Push notifications', exact: true })).toHaveCount(0);
	await page.goto('/alpha/settings/push');
	await expect(page.getByText('Only instance sysadmins can manage push notifications.')).toBeVisible();
	await expect(page.getByRole('switch')).toHaveCount(0);
});

test('environment configuration is read-only and can be tested', async ({ page }) => {
	await setup(page, true, true);
	await page.goto('/alpha/settings/push');
	await expect(page.getByLabel('Contact')).toBeDisabled();
	await expect(page.getByRole('switch')).toBeDisabled();
	await expect(page.getByRole('button', { name: 'Regenerate key pair' })).toBeDisabled();
	await page.getByRole('button', { name: 'Send test notification' }).click();
	await expect(page.getByText('Sent: 1. Failed: 1.')).toBeVisible();
});
