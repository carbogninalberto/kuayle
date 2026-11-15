import { expect, test } from '@playwright/test';

const idpURL = 'https://idp.example.test/authorize?state=link-state';

test('a link_required callback signs in with the password and then starts the link flow', async ({ page }) => {
	const calls: string[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('https://idp.example.test/**', (route) =>
		route.fulfill({ body: '<html><body>Identity provider</body></html>', contentType: 'text/html' })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		calls.push(`${request.method()} ${path}`);
		if (path === '/api/config')
			return route.fulfill({
				json: { registration_enabled: true, passkeys_enabled: false, sso_enabled_instance: false }
			});
		if (path === '/api/auth/login')
			return route.fulfill({
				json: {
					id: 'user-1',
					email: 'person@example.com',
					name: 'Person',
					display_name: 'Person',
					avatar_url: null,
					is_sysadmin: false
				}
			});
		if (path === '/api/auth/sso/test/link') return route.fulfill({ json: { auth_url: idpURL } });
		return route.fulfill({ json: [] });
	});

	await page.goto('/login?sso_error=link_required&workspace=test');
	await expect(
		page.getByText('An account with this email already exists. Sign in with your password to link SSO.')
	).toBeVisible();

	await page.getByLabel('Email').fill('person@example.com');
	await page.getByLabel('Password', { exact: true }).fill('correct-horse-battery-1');
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();

	await page.waitForURL('https://idp.example.test/**');
	expect(calls).toContain('POST /api/auth/login');
	expect(calls).toContain('POST /api/auth/sso/test/link');
});

const workspaceUser = {
	id: 'a0000000-0000-0000-0000-000000000001',
	email: 'person@example.com',
	name: 'Person',
	display_name: 'Person',
	avatar_url: null,
	is_sysadmin: false
};

test('an SSO-required workspace offers the link flow instead of a silent redirect', async ({ page }) => {
	const calls: string[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('https://idp.example.test/**', (route) =>
		route.fulfill({ body: '<html><body>Identity provider</body></html>', contentType: 'text/html' })
	);
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		calls.push(`${request.method()} ${path}`);
		if (path === '/api/auth/me') return route.fulfill({ json: workspaceUser });
		if (path === '/api/workspaces/test')
			return route.fulfill({
				status: 403,
				json: {
					error: {
						code: 'SSO_REQUIRED',
						message: 'This workspace requires single sign-on',
						workspace_slug: 'test'
					}
				}
			});
		if (path === '/api/auth/sso/test/link') return route.fulfill({ json: { auth_url: idpURL } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});

	await page.goto('/test/inbox');

	await expect(page.getByText('Single sign-on required')).toBeVisible();
	await expect(page).toHaveURL(/\/test\/inbox$/);
	await page.getByRole('button', { name: 'Continue with SSO' }).click();

	await page.waitForURL('https://idp.example.test/**');
	expect(calls).toContain('POST /api/auth/sso/test/link');
});

test('an SSO-scoped session explains the workspace limit and offers sign out', async ({ page }) => {
	const calls: string[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/other/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		calls.push(`${request.method()} ${path}`);
		if (path === '/api/auth/me')
			return route.fulfill({ json: { ...workspaceUser, sso_workspace_slug: 'test' } });
		if (path === '/api/workspaces/other')
			return route.fulfill({
				status: 403,
				json: {
					error: {
						code: 'SSO_SESSION_SCOPED',
						message: 'This session is limited to the workspace it signed in through'
					}
				}
			});
		if (path === '/api/auth/logout') return route.fulfill({ status: 204 });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});

	await page.goto('/other/inbox');

	await expect(page.getByText('SSO-scoped session')).toBeVisible();
	await expect(page).toHaveURL(/\/other\/inbox$/);
	await expect(page.getByRole('button', { name: 'Back to test' })).toBeVisible();

	await page.getByRole('button', { name: 'Log out' }).click();
	await page.waitForURL('**/login');
	expect(calls).toContain('POST /api/auth/logout');
});
