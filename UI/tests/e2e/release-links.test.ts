import { expect, test } from '@playwright/test';

test('retries and restricts release manifest links to the Kuayle GitHub repository', async ({ page }) => {
	const tagName = 'v99.0.0';
	const releaseUrl = `https://github.com/carbogninalberto/kuayle/releases/tag/${tagName}`;
	let requireUpgrade = false;
	let releaseRequests = 0;
	const transientFailures = ['http', 'json', 'http'];

	await page.addInitScript((tag) => {
		localStorage.setItem('kuayle_release_notice_dismissed', tag);
	}, tagName);

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => {
		releaseRequests += 1;
		const failure = transientFailures.shift();
		if (failure === 'http') return route.fulfill({ status: 503, body: 'temporarily unavailable' });
		if (failure === 'json') return route.fulfill({ status: 200, contentType: 'application/json', body: '{' });
		return route.fulfill({
			json: [
				{
					tag_name: tagName,
					html_url: 'https://attacker.example/releases/latest',
					body: '[Trusted pull request](https://github.com/carbogninalberto/kuayle/pull/46)\n\n[Untrusted repository](https://github.com/attacker/kuayle/pull/46)',
					published_at: '2026-07-23T00:00:00Z',
					prerelease: false,
					force_upgrade: requireUpgrade,
					minimum_supported_version: requireUpgrade ? tagName : null,
					upgrade_url: 'https://attacker.example/install',
					upgrade_message: null
				}
			]
		});
	});

	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'test@example.com',
					name: 'Test User',
					display_name: 'Test User',
					avatar_url: null,
					is_sysadmin: false
				}
			});
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		}
		if (path === '/api/workspaces') {
			return route.fulfill({
				json: [{ id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test' }]
			});
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000002',
					name: 'Test Workspace',
					slug: 'test',
					current_user_role: 'owner'
				}
			});
		}
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});

	await page.goto('/test/settings/version');
	await expect(page.getByText('No releases were found.')).toBeVisible();
	await expect.poll(() => releaseRequests).toBe(3);
	await page.getByRole('button', { name: 'Check releases' }).click();
	await expect(page.getByRole('link', { name: 'Open', exact: true })).toHaveAttribute('href', releaseUrl);
	await expect.poll(() => releaseRequests).toBe(4);
	await expect(page.getByRole('link', { name: 'Trusted pull request' })).toHaveAttribute(
		'href',
		'https://github.com/carbogninalberto/kuayle/pull/46'
	);
	await expect(page.getByText('Untrusted repository', { exact: true })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Untrusted repository' })).toHaveCount(0);

	requireUpgrade = true;
	await page.reload();
	const requiredDialog = page.getByRole('alertdialog');
	await expect(requiredDialog).toBeVisible();
	await expect(requiredDialog.getByRole('link', { name: 'Open release' })).toHaveAttribute('href', releaseUrl);
});
