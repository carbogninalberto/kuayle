import { expect, test, type Page } from '@playwright/test';

const redirectURI = 'https://kuayle.test/api/auth/sso/test/callback';

function ssoConfig(overrides: Record<string, unknown> = {}) {
	return {
		issuer_url: 'https://idp.example.com/realms/test',
		client_id: 'kuayle-test',
		has_client_secret: true,
		allowed_domains: ['example.com'],
		default_role: 'member',
		require_sso: false,
		enabled: true,
		provider_name: 'Example IdP',
		managed_by_instance: false,
		redirect_uri: redirectURI,
		...overrides
	};
}

async function mockApi(page: Page, role: 'owner' | 'admin' | 'member', saves: unknown[], initial: Record<string, unknown> = {}) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	let config = ssoConfig(initial);
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/sso') {
			if (request.method() === 'PUT') {
				const update = request.postDataJSON();
				saves.push(update);
				config = ssoConfig({
					issuer_url: update.issuer_url,
					client_id: update.client_id,
					allowed_domains: update.allowed_domains,
					default_role: update.default_role,
					require_sso: update.require_sso,
					enabled: update.enabled,
					provider_name: update.provider_name
				});
			}
			return route.fulfill({ json: config });
		}
		return route.fulfill({ json: [] });
	});
}

test('an owner edits the OIDC configuration and the secret stays write-only', async ({ page }) => {
	const saves: unknown[] = [];
	await mockApi(page, 'owner', saves);
	await page.goto('/test/settings/sso');

	await expect(page.getByRole('heading', { name: 'Single sign-on (OIDC)' })).toBeVisible();
	await expect(page.getByLabel('Issuer URL')).toHaveValue('https://idp.example.com/realms/test');
	await expect(page.getByLabel('Redirect URI', { exact: true })).toHaveValue(redirectURI);

	const secret = page.getByLabel('Client secret');
	await expect(secret).toHaveValue('');
	await expect(secret).toHaveAttribute('placeholder', 'Stored — leave empty to keep it');

	await page.getByLabel('Allowed email domains').fill('example.com, example.org');
	await secret.fill('new-client-secret');
	await page.getByRole('button', { name: 'Save configuration' }).click();

	await expect.poll(() => saves.length).toBe(1);
	expect(saves[0]).toMatchObject({
		client_id: 'kuayle-test',
		client_secret: 'new-client-secret',
		allowed_domains: ['example.com', 'example.org'],
		default_role: 'member'
	});
	// The saved response is applied again: the secret field clears and reports
	// the stored flag instead of echoing the value.
	await expect(secret).toHaveValue('');
	await expect(secret).toHaveAttribute('placeholder', 'Stored — leave empty to keep it');
});

test('disabling SSO clears require-SSO in the request', async ({ page }) => {
	const saves: unknown[] = [];
	await mockApi(page, 'owner', saves, { require_sso: true });
	await page.goto('/test/settings/sso');

	await expect(page.getByRole('switch', { name: 'Enable SSO sign-in' })).toBeChecked();
	await page.getByRole('switch', { name: 'Enable SSO sign-in' }).click();
	await page.getByRole('button', { name: 'Save configuration' }).click();

	await expect.poll(() => saves.length).toBe(1);
	expect(saves[0]).toMatchObject({ enabled: false, require_sso: false });
});

test('an admin can reach the page and a member sees the admin-only note', async ({ page }) => {
	await mockApi(page, 'admin', []);
	await page.goto('/test/settings/sso');
	await expect(page.getByRole('button', { name: 'Save configuration' })).toBeVisible();

	await page.unrouteAll();
	await mockApi(page, 'member', []);
	await page.goto('/test/settings/sso');
	await expect(page.getByText('Only workspace owners and admins can configure single sign-on.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Save configuration' })).toHaveCount(0);
});

test('an instance-managed configuration is read-only', async ({ page }) => {
	await mockApi(page, 'owner', []);
	// Override the config response after the shared mock is installed.
	await page.route('**/api/workspaces/test/sso', (route) =>
		route.fulfill({ json: ssoConfig({ managed_by_instance: true }) })
	);
	await page.goto('/test/settings/sso');

	await expect(page.getByText('This configuration is managed by the instance environment variables and is read-only here.')).toBeVisible();
	await expect(page.getByLabel('Issuer URL')).toBeDisabled();
	await expect(page.getByRole('button', { name: 'Save configuration' })).toHaveCount(0);
});
