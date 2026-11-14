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
