import { test, expect, type Page } from '@playwright/test';

const signUp = (page: Page) => page.getByRole('button', { name: 'Sign up', exact: true });
const ssoButton = (page: Page) =>
	page.getByRole('button', { name: 'Sign in with SSO', includeHidden: true });
const demoMode = /^(1|true|yes|on)$/i.test(process.env.PUBLIC_DEMO_MODE ?? '');

test.beforeEach(async ({ page }) => {
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
});

test('login page loads', async ({ page }) => {
	await page.goto('/login');
	await expect(page.getByAltText('Kuayle logo')).toBeVisible();
	await expect(page.getByText('Kuayle')).toBeVisible();
	await expect(page.getByText('Sign in to your account')).toBeVisible();
});

test('login form has required fields', async ({ page }) => {
	await page.goto('/login');
	await expect(page.getByLabel('Email')).toBeVisible();
	await expect(page.getByRole('textbox', { name: 'Password', exact: true })).toBeVisible();
});

test('signup waits for activation and stays disabled in demo mode', async ({ page }) => {
	const demoUsers = page.getByRole('button', { name: 'Demo users' });
	const { promise: configReady, resolve: releaseConfig } = Promise.withResolvers<void>();
	await page.route('**/api/config', async (route) => {
		await configReady;
		await route.fulfill({ json: { registration_enabled: true, passkeys_enabled: true } });
	});
	await page.goto('/login');
	try {
		await expect(signUp(page)).toBeVisible();
		await expect(signUp(page)).toBeDisabled();
	} finally {
		releaseConfig();
	}
	await expect(page.getByRole('button', { name: 'Sign in with a passkey' })).toBeVisible();

	if (demoMode) {
		await expect(demoUsers).toBeVisible();
		await expect(signUp(page)).toBeDisabled();
		await page.goto('/login?invite=demo-invite');
		await expect(signUp(page)).toBeDisabled();
	} else {
		await expect(demoUsers).toHaveCount(0);
		await expect(signUp(page)).toBeEnabled();
		await signUp(page).press('Enter');
		await expect(page.getByRole('button', { name: 'Create account' })).toBeVisible();
	}
});

test('an unavailable auth configuration leaves signup disabled and SSO hidden', async ({ page }) => {
	await page.route('**/api/config', (route) => route.fulfill({ status: 503, json: {} }));
	const configResponse = page.waitForResponse('**/api/config');
	await page.goto('/login');
	await (await configResponse).finished();
	await expect(signUp(page)).toBeVisible();
	await expect(signUp(page)).toBeDisabled();
	await expect(ssoButton(page)).toHaveCount(0);
});

test('SSO appears only when enabled and offers a short provider action', async ({ page }) => {
	let enabled = false;
	await page.route('**/api/config', (route) =>
		route.fulfill({ json: { registration_enabled: false, sso_enabled: enabled, passkeys_enabled: true } })
	);
	await page.route('**/api/auth/sso/acme', (route) => route.fulfill({ json: { enabled: true } }));
	await page.route('**/api/auth/sso/acme/start', (route) => route.fulfill({ body: 'SSO started' }));

	await page.goto('/login');
	await expect(page.getByRole('button', { name: 'Sign in with a passkey' })).toBeVisible();
	await expect.soft(ssoButton(page)).toHaveCount(0);

	enabled = true;
	await page.reload();
	await ssoButton(page).click();
	const form = page.getByRole('form', { name: 'Sign in with SSO' });
	const provider = form.getByRole('button', { name: 'Continue', exact: true });
	await expect(provider).toBeDisabled();
	await form.getByRole('textbox', { name: 'Workspace' }).fill('acme');
	await provider.click();
	await expect(page).toHaveURL('/api/auth/sso/acme/start');
});
