import { expect, test, type Page } from '@playwright/test';

const token = 'single-use-token';
const user = {
	id: '11111111-1111-4111-8111-111111111111',
	email: 'invitee@example.test',
	name: 'Invitee',
	display_name: 'Invitee',
	avatar_url: null,
	is_sysadmin: false
};
const preview = {
	workspace_name: 'Invited Workspace',
	workspace_slug: 'invited',
	workspace_logo_url: null,
	role: 'member',
	status: 'valid'
};
const unauthorized = { error: { code: 'UNAUTHORIZED', message: 'Unauthorized' } };

async function mockApi(
	page: Page,
	options: {
		authenticated?: boolean;
		status?: string;
		failRegister?: boolean;
		failPreview?: boolean;
		failLogin?: boolean;
		refreshSucceeds?: boolean;
	} = {}
) {
	let authenticated = options.authenticated ?? false;
	const requests: { path: string; method: string; body: unknown }[] = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		requests.push({ path, method: request.method(), body: request.postDataJSON() });
		if (path === '/api/config') return route.fulfill({ json: { registration_enabled: false } });
		if (path === '/api/auth/me')
			return route.fulfill(authenticated ? { json: user } : { status: 401, json: unauthorized });
		if (path === '/api/auth/refresh') {
			if (options.refreshSucceeds) return route.fulfill({ json: { status: 'refreshed' } });
			return route.fulfill({ status: 401, json: unauthorized });
		}
		if (path === `/api/invite/${token}`)
			return route.fulfill(
				options.failPreview
					? { status: 500, json: { error: { code: 'INTERNAL_ERROR' } } }
					: { json: { ...preview, status: options.status ?? 'valid' } }
			);
		if (path === '/api/auth/register') {
			if (options.failRegister)
				return route.fulfill({
					status: 403,
					json: { error: { code: 'INVALID_INVITE_TOKEN', message: 'This invite is no longer available' } }
				});
			authenticated = true;
			return route.fulfill({ json: user });
		}
		if (path === '/api/auth/login') {
			if (options.failLogin)
				return route.fulfill({
					status: 401,
					json: { error: { code: 'INVALID_CREDENTIALS', message: 'Invalid email or password' } }
				});
			authenticated = true;
			return route.fulfill({ json: user });
		}
		if (path === `/api/invite/${token}/accept`)
			return route.fulfill({
				json: {
					workspace_id: '22222222-2222-4222-8222-222222222222',
					workspace_slug: 'invited',
					workspace_name: preview.workspace_name,
					role: 'member'
				}
			});
		if (path === '/api/workspaces/invited')
			return route.fulfill({
				json: {
					id: '22222222-2222-4222-8222-222222222222',
					name: preview.workspace_name,
					slug: 'invited',
					owner_id: user.id,
					current_user_role: 'member',
					logo_url: null
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/auth/me/preferences') return route.fulfill({ json: {} });
		return route.fulfill({ json: [] });
	});
	return requests;
}

async function fillCredentials(page: Page) {
	await page.getByLabel('Email', { exact: true }).fill(user.email);
	await page.getByRole('textbox', { name: 'Password', exact: true }).fill('password123');
}

test('signed-out invitation keeps token through session and refresh failures', async ({ page }) => {
	const requests = await mockApi(page);
	await page.goto(`/invite/${token}`);
	await expect(page).toHaveURL(`/login?invite=${token}`);
	await expect(
		page.getByText('You were invited to join a workspace. Sign in or create an account to continue.')
	).toBeVisible();
	await expect.poll(() => requests.filter((r) => r.path === '/api/auth/refresh').length).toBe(1);
	await page.getByRole('button', { name: 'Sign up', exact: true }).click();
	await expect(page.getByLabel('Name', { exact: true })).toBeVisible();
});

test('invited signup submits token and reaches the invited workspace after single-use exhaustion', async ({ page }) => {
	const requests = await mockApi(page, { status: 'exhausted' });
	await page.goto(`/login?invite=${token}`);
	await page.getByRole('button', { name: 'Sign up', exact: true }).click();
	await page.getByLabel('Name', { exact: true }).fill(user.name);
	await fillCredentials(page);
	await page.getByRole('button', { name: 'Create account', exact: true }).click();
	await expect(page).toHaveURL('/invited/inbox');
	expect(requests.find((r) => r.path === '/api/auth/register')?.body).toMatchObject({ invite_token: token });
	expect(requests.some((r) => r.path === `/api/invite/${token}/accept`)).toBe(true);
	// The destination sidebar may load memberships; invited signup must never create a workspace.
	expect(requests.some((r) => r.path === '/api/workspaces' && r.method === 'POST')).toBe(false);
});

test('failed invited signup stays on the form without creating a workspace', async ({ page }) => {
	const requests = await mockApi(page, { failRegister: true });
	await page.goto(`/login?invite=${token}`);
	await page.getByRole('button', { name: 'Sign up', exact: true }).click();
	await page.getByLabel('Name', { exact: true }).fill(user.name);
	await fillCredentials(page);
	await page.getByRole('button', { name: 'Create account', exact: true }).click();
	await expect(page.getByText('This invite is no longer available')).toBeVisible();
	await expect(page).toHaveURL(`/login?invite=${token}`);
	expect(requests.some((r) => r.path === '/api/workspaces' || r.path.endsWith('/accept'))).toBe(false);
});

test('existing account signs in before confirming invitation', async ({ page }) => {
	const requests = await mockApi(page);
	await page.goto(`/login?invite=${token}`);
	await fillCredentials(page);
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();
	await expect(page.getByRole('heading', { name: "You've been invited to join Invited Workspace" })).toBeVisible();
	expect(requests.some((r) => r.path.endsWith('/accept'))).toBe(false);
	await page.getByRole('button', { name: 'Join workspace', exact: true }).click();
	await expect(page).toHaveURL('/invited/inbox');
});

for (const status of ['exhausted', 'revoked', 'expired']) {
	test(`existing member can reopen ${status} invitation after page reload`, async ({ page }) => {
		const requests = await mockApi(page, { authenticated: true, status });
		await page.goto(`/invite/${token}`);
		await expect(page).toHaveURL('/invited/inbox');
		expect(requests.some((r) => r.path === '/api/auth/me')).toBe(true);
		expect(requests.some((r) => r.path.endsWith('/accept'))).toBe(true);
	});
}

test('expired invitation remains visible to signed-out visitors', async ({ page }) => {
	await mockApi(page, { status: 'expired' });
	await page.goto(`/invite/${token}`);
	await expect(page.getByText('This invite link has expired.')).toBeVisible();
	await expect(page).toHaveURL(`/invite/${token}`);
});

test('preview service error offers retry without labeling invitation invalid', async ({ page }) => {
	await mockApi(page, { failPreview: true });
	await page.goto(`/invite/${token}`);
	await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
	await expect(page.getByText('Invite unavailable')).toHaveCount(0);
});

test('invalid credentials preserve invitation and do not trigger refresh', async ({ page }) => {
	const requests = await mockApi(page, { failLogin: true });
	await page.goto(`/login?invite=${token}`);
	await fillCredentials(page);
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();
	await expect(page.getByText('Invalid email or password')).toBeVisible();
	await expect(page).toHaveURL(`/login?invite=${token}`);
	expect(requests.some((r) => r.path === '/api/auth/refresh')).toBe(false);
});

test('session retry stops after one refresh even if refreshed credentials are rejected', async ({ page }) => {
	const requests = await mockApi(page, { refreshSucceeds: true });
	await page.goto(`/invite/${token}`);
	await expect(page).toHaveURL(`/login?invite=${token}`);
	expect(requests.filter((r) => r.path === '/api/auth/refresh')).toHaveLength(1);
	expect(requests.filter((r) => r.path === '/api/auth/me')).toHaveLength(2);
});

test('public signup is hidden when registration is disabled without an invitation', async ({ page }) => {
	await mockApi(page);
	await page.goto('/login');
	await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Sign up', exact: true })).toHaveCount(0);
});

test('invite link creation validates expiry and use limits before submitting', async ({ page }) => {
	await mockApi(page, { authenticated: true });
	await page.goto('/invited/settings/members');
	await page.getByRole('button', { name: 'Create link', exact: true }).click();
	const dialog = page.getByRole('dialog');
	const create = dialog.getByRole('button', { name: 'Create link', exact: true });
	await expect(create).toBeEnabled();
	await dialog.getByLabel('Expires in (days)').fill('0');
	await expect(create).toBeDisabled();
	await dialog.getByLabel('Expires in (days)').fill('366');
	await expect(create).toBeDisabled();
	await dialog.getByLabel('Expires in (days)').fill('7');
	await dialog.getByLabel('Max uses', { exact: true }).fill('1.5');
	await expect(create).toBeDisabled();
	await dialog.getByLabel('Max uses', { exact: true }).fill('');
	await expect(create).toBeEnabled();
});

test('clipboard failure preserves the created URL and reports manual copying', async ({ page }) => {
	await mockApi(page, { authenticated: true });
	await page.addInitScript(() => {
		Object.defineProperty(navigator, 'clipboard', {
			value: { writeText: () => Promise.reject(new Error('Denied')) },
			configurable: true
		});
	});
	const url = 'https://example.test/invite/new-token';
	await page.route('**/api/workspaces/invited/invite-links', async (route) =>
		route.fulfill({
			json:
				route.request().method() === 'POST'
					? {
							id: 'link1',
							role: 'member',
							expires_at: '2099-01-01T00:00:00Z',
							max_uses: null,
							use_count: 0,
							revoked_at: null,
							created_at: '2026-10-04T00:00:00Z',
							invite_url: url
						}
					: []
		})
	);
	await page.goto('/invited/settings/members');
	await page.getByRole('button', { name: 'Create link', exact: true }).click();
	const dialog = page.getByRole('dialog');
	await dialog.getByRole('button', { name: 'Create link', exact: true }).click();
	await expect(dialog.getByRole('textbox', { name: 'Invite link created', exact: true })).toHaveValue(url);
	await dialog.getByRole('button', { name: 'Copy', exact: true }).click();
	await expect(page.getByText('Could not copy the link. Select it and copy it manually.')).toBeVisible();
	await expect(page.getByText('Invite link copied', { exact: true })).toHaveCount(0);
	await expect(dialog.getByRole('textbox', { name: 'Invite link created', exact: true })).toHaveValue(url);
});
