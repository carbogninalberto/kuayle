import { expect, test, type BrowserContext, type Page, type Route } from '@playwright/test';
import { generateKeyPairSync } from 'node:crypto';

// The API is mocked; the WebAuthn side is real: Chromium's virtual
// authenticator creates and uses credentials for the relying party
// "localhost", so the tests exercise the actual navigator.credentials calls
// and the JSON the UI sends to the server.

const user = {
	id: '0b8f3c7e-58a4-4c1f-9d57-1f0c2b9f6a11',
	name: 'Ada Lovelace',
	display_name: 'Ada',
	email: 'ada@example.com',
	avatar_url: null,
	is_sysadmin: false
};
const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
const challenge = 'dGVzdC1jaGFsbGVuZ2UtMDEyMzQ1Njc4OWFiY2RlZg';
const userHandle = 'C488flikTB-dVx8MK59qEQ';

const creationOptions = (excludeCredentials: unknown[] = []) => ({
	publicKey: {
		rp: { id: 'localhost', name: 'Kuayle' },
		user: { id: userHandle, name: user.email, displayName: user.display_name },
		challenge,
		pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
		timeout: 300000,
		authenticatorSelection: { residentKey: 'required', requireResidentKey: true, userVerification: 'required' },
		attestation: 'none',
		excludeCredentials
	}
});
const requestOptions = { publicKey: { challenge, rpId: 'localhost', timeout: 300000, userVerification: 'required' } };

type Api = {
	signedIn: boolean;
	passkeysEnabled: boolean;
	passkeys: Record<string, unknown>[];
	workspaces: unknown[];
	/** Requests the page made to the passkey endpoints, in order. */
	calls: { path: string; method: string; body: any }[];
	/** Overrides for single endpoints. */
	handlers: Record<string, (route: Route, body: any) => Promise<void> | void>;
};

async function mockApi(page: Page, overrides: Partial<Api> = {}): Promise<Api> {
	const api: Api = { signedIn: true, passkeysEnabled: true, passkeys: [], workspaces: [workspace], calls: [], handlers: {}, ...overrides };
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		let body: any;
		try {
			body = request.postDataJSON();
		} catch {
			body = undefined;
		}
		if (path.startsWith('/api/auth/passkey')) api.calls.push({ path, method, body });
		const handler = api.handlers[`${method} ${path}`];
		if (handler) return handler(route, body);

		if (path === '/api/config')
			return route.fulfill({ json: { registration_enabled: true, passkeys_enabled: api.passkeysEnabled } });
		if (path === '/api/auth/me')
			return api.signedIn
				? route.fulfill({ json: user })
				: route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED', message: 'Unauthorized' } } });
		if (path === '/api/auth/refresh')
			return route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED', message: 'Unauthorized' } } });
		if (path === '/api/auth/login') {
			api.signedIn = true;
			return route.fulfill({ json: user });
		}
		if (path === '/api/auth/passkey/login/begin') return route.fulfill({ json: requestOptions });
		if (path === '/api/auth/passkey/login/complete') {
			api.signedIn = true;
			return route.fulfill({ json: user });
		}
		if (path === '/api/auth/passkey/register/begin') return route.fulfill({ json: creationOptions() });
		if (path === '/api/auth/passkey/register/complete') {
			const created = {
				id: `passkey-${api.passkeys.length + 1}`,
				name: body.name,
				transports: body.credential.response.transports,
				aaguid: '00000000-0000-0000-0000-000000000000',
				backed_up: false,
				created_at: '2026-11-10T09:00:00Z',
				last_used_at: null,
				current: false
			};
			api.passkeys.push(created);
			return route.fulfill({ status: 201, json: created });
		}
		if (path === '/api/auth/passkey/credentials') return route.fulfill({ json: api.passkeys });
		if (path.startsWith('/api/auth/passkey/credentials/')) {
			const id = path.split('/').pop();
			if (method === 'PATCH') {
				const passkey = api.passkeys.find((p) => p.id === id)!;
				passkey.name = body.name;
				return route.fulfill({ json: passkey });
			}
			api.passkeys = api.passkeys.filter((p) => p.id !== id);
			return route.fulfill({ json: { status: 'deleted' } });
		}
		if (path === '/api/workspaces') return route.fulfill({ json: api.workspaces });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
	return api;
}

/** A platform authenticator that verifies its user and approves every request. */
async function virtualAuthenticator(context: BrowserContext, page: Page) {
	const cdp = await context.newCDPSession(page);
	await cdp.send('WebAuthn.enable');
	const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
		options: {
			protocol: 'ctap2',
			transport: 'internal',
			hasResidentKey: true,
			hasUserVerification: true,
			isUserVerified: true,
			automaticPresenceSimulation: true
		}
	});
	return {
		credentials: async () => (await cdp.send('WebAuthn.getCredentials', { authenticatorId })).credentials,
		/** Stores a discoverable credential for localhost, as if registered earlier. */
		addCredential: async () => {
			const { privateKey } = generateKeyPairSync('ec', { namedCurve: 'P-256' });
			const credentialId = Buffer.from('existing-passkey-credential-id').toString('base64');
			await cdp.send('WebAuthn.addCredential', {
				authenticatorId,
				credential: {
					credentialId,
					isResidentCredential: true,
					rpId: 'localhost',
					privateKey: privateKey.export({ type: 'pkcs8', format: 'der' }).toString('base64'),
					userHandle: Buffer.from(userHandle, 'base64url').toString('base64'),
					signCount: 0
				}
			});
			return Buffer.from('existing-passkey-credential-id').toString('base64url');
		}
	};
}

const passkeyButton = (page: Page) => page.getByRole('button', { name: 'Sign in with a passkey' });

/**
 * Turns off passkey autofill. The virtual authenticator answers an autofill
 * request by itself as soon as the page loads, which would sign in before a
 * test can press the button.
 */
const withoutAutofill = (page: Page) =>
	page.addInitScript(() => {
		PublicKeyCredential.isConditionalMediationAvailable = async () => false;
	});

test.describe('sign-in page', () => {
	test('offers passkey sign-in only when the instance and the browser support it', async ({ page }) => {
		await mockApi(page, { signedIn: false });
		await page.goto('/login');
		await expect(passkeyButton(page)).toBeVisible();
		await expect(page.getByLabel('Email')).toHaveAttribute('autocomplete', 'username webauthn');
		// Creating an account is a password flow.
		await page.getByRole('button', { name: 'Sign up' }).click();
		await expect(passkeyButton(page)).toBeHidden();
	});

	test('hides passkey sign-in when the instance has passkeys off', async ({ page }) => {
		const api = await mockApi(page, { signedIn: false, passkeysEnabled: false });
		await page.goto('/login');
		await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
		await expect(passkeyButton(page)).toBeHidden();
		expect(api.calls).toEqual([]);
	});

	test('hides passkey sign-in in a browser without WebAuthn', async ({ page }) => {
		await page.addInitScript(() => {
			// @ts-expect-error simulate a browser that never had the API
			delete window.PublicKeyCredential;
		});
		const api = await mockApi(page, { signedIn: false });
		await page.goto('/login');
		await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
		await expect(passkeyButton(page)).toBeHidden();
		expect(api.calls).toEqual([]);
	});

	test('signs in with a passkey alone and does not offer to add one', async ({ page, context }) => {
		const api = await mockApi(page, { signedIn: false, workspaces: [] });
		const authenticator = await virtualAuthenticator(context, page);
		const credentialId = await authenticator.addCredential();
		await withoutAutofill(page);
		await page.goto('/login');
		await passkeyButton(page).click();
		await page.waitForURL('**/workspace-setup');
		expect(api.calls.map((call) => call.path)).toEqual(['/api/auth/passkey/login/begin', '/api/auth/passkey/login/complete']);

		const complete = api.calls.find((call) => call.path === '/api/auth/passkey/login/complete')!;
		expect(complete.body.credential.id).toBe(credentialId);
		expect(complete.body.credential.type).toBe('public-key');
		expect(complete.body.credential.response.userHandle).toBe(userHandle);
		expect(complete.body.credential.response.signature).toMatch(/^[\w-]+$/);
		const clientData = JSON.parse(Buffer.from(complete.body.credential.response.clientDataJSON, 'base64url').toString());
		expect(clientData).toMatchObject({ type: 'webauthn.get', challenge });
		// No email or password went anywhere.
		await expect(page.getByRole('dialog')).toBeHidden();
	});

	test('explains a passkey the server no longer knows and stays on the page', async ({ page, context }) => {
		const api = await mockApi(page, { signedIn: false });
		api.handlers['POST /api/auth/passkey/login/complete'] = (route) =>
			route.fulfill({ status: 401, json: { error: { code: 'PASSKEY_UNKNOWN', message: 'This passkey is not registered with any account' } } });
		const authenticator = await virtualAuthenticator(context, page);
		await authenticator.addCredential();
		await withoutAutofill(page);
		await page.goto('/login');
		await passkeyButton(page).click();
		await expect(page.getByText('That passkey is not registered here. It may have been removed.')).toBeVisible();
		await expect(page).toHaveURL(/\/login$/);
		await expect(passkeyButton(page)).toBeEnabled();
	});

	test('signs in from the email field autofill without pressing anything', async ({ page, context }) => {
		const api = await mockApi(page, { signedIn: false, workspaces: [] });
		const authenticator = await virtualAuthenticator(context, page);
		const credentialId = await authenticator.addCredential();
		// The request is made with conditional mediation, so the browser offers
		// the passkey in the field instead of opening a prompt.
		await page.addInitScript(() => {
			const get = navigator.credentials.get.bind(navigator.credentials);
			navigator.credentials.get = (options) => {
				(window as any).passkeyMediation = options?.mediation;
				return get(options);
			};
		});
		await page.goto('/login');
		await page.waitForURL('**/workspace-setup');
		expect(await page.evaluate(() => (window as any).passkeyMediation)).toBe('conditional');
		const complete = api.calls.find((call) => call.path === '/api/auth/passkey/login/complete')!;
		expect(complete.body.credential.id).toBe(credentialId);
	});

	test('renews the autofill request before its challenge expires on a page left open', async ({ page }) => {
		const api = await mockApi(page, { signedIn: false });
		await page.clock.install();
		// A browser waiting for the user to pick a passkey from the field.
		await page.addInitScript(() => {
			PublicKeyCredential.isConditionalMediationAvailable = async () => true;
			const state = ((window as any).autofill = { requests: 0, aborted: 0 });
			navigator.credentials.get = (options) =>
				new Promise((_, reject) => {
					state.requests++;
					options?.signal?.addEventListener('abort', () => {
						state.aborted++;
						reject(new DOMException('aborted', 'AbortError'));
					});
				});
		});
		await page.goto('/login');
		const begins = () => api.calls.filter((call) => call.path === '/api/auth/passkey/login/begin').length;
		await expect.poll(begins).toBe(1);
		await expect.poll(() => page.evaluate(() => (window as any).autofill)).toEqual({ requests: 1, aborted: 0 });

		// Challenges live five minutes; shortly before that nothing has changed yet.
		await page.clock.fastForward('03:50');
		expect(begins()).toBe(1);
		await page.clock.fastForward('00:15');
		await expect.poll(begins).toBe(2);
		await expect.poll(() => page.evaluate(() => (window as any).autofill)).toEqual({ requests: 2, aborted: 1 });
		await page.clock.fastForward('04:05');
		await expect.poll(begins).toBe(3);
		// Replacing the request is silent.
		await expect(page.getByText(/passkey request expired|Could not sign in/)).toBeHidden();
	});

	test('a failed start of autofill stays silent and the page keeps working', async ({ page }) => {
		const api = await mockApi(page, { signedIn: false });
		api.handlers['POST /api/auth/passkey/login/begin'] = (route) =>
			route.fulfill({ status: 400, json: { error: { code: 'PASSKEY_ORIGIN_MISMATCH', message: 'Passkeys are not set up for the address you are using to reach this instance' } } });
		await page.addInitScript(() => {
			PublicKeyCredential.isConditionalMediationAvailable = async () => true;
		});
		await page.goto('/login');
		await expect(passkeyButton(page)).toBeVisible();
		await expect.poll(() => api.calls.length).toBe(1);
		await expect(page.getByText(/Passkeys are not set up/)).toBeHidden();
		// Asking for it explicitly does say why.
		await passkeyButton(page).click();
		await expect(page.getByText(/Passkeys are not set up/)).toBeVisible();
		expect(api.calls.length).toBeLessThanOrEqual(3);
	});

	test('a dismissed browser prompt is not reported as an error', async ({ page, context }) => {
		await mockApi(page, { signedIn: false });
		await virtualAuthenticator(context, page);
		await withoutAutofill(page);
		await page.addInitScript(() => {
			navigator.credentials.get = () => Promise.reject(new DOMException('cancelled', 'NotAllowedError'));
		});
		await page.goto('/login');
		await passkeyButton(page).click();
		await expect(passkeyButton(page)).toBeEnabled();
		await expect(page.getByText('Could not sign in with that passkey')).toBeHidden();
	});
});

test.describe('offer after a password sign-in', () => {
	async function passwordSignIn(page: Page) {
		await page.goto('/login');
		await page.getByLabel('Email').fill(user.email);
		await page.getByRole('textbox', { name: 'Password', exact: true }).fill('Password123!!');
		await page.getByRole('button', { name: 'Sign in', exact: true }).click();
		await page.waitForURL('**/workspace-setup');
	}
	const offer = (page: Page) => page.getByRole('dialog', { name: 'Sign in faster next time' });

	test('adds a passkey from the offer', async ({ page, context }) => {
		const api = await mockApi(page, { signedIn: false, workspaces: [] });
		const authenticator = await virtualAuthenticator(context, page);
		await passwordSignIn(page);
		await expect(offer(page)).toBeVisible();
		await offer(page).getByLabel('Name').fill('My laptop');
		await offer(page).getByRole('button', { name: 'Add a passkey' }).click();
		await expect(page.getByText('Passkey added')).toBeVisible();
		await expect(offer(page)).toBeHidden();

		expect(api.passkeys.map((p) => p.name)).toEqual(['My laptop']);
		const stored = await authenticator.credentials();
		expect(stored).toHaveLength(1);
		expect(stored[0]).toMatchObject({ isResidentCredential: true, rpId: 'localhost' });
		const begin = api.calls.find((call) => call.path === '/api/auth/passkey/register/begin')!;
		expect(begin.body).toEqual({}); // a fresh session sends no password
		const complete = api.calls.find((call) => call.path === '/api/auth/passkey/register/complete')!;
		expect(complete.body.credential.response.attestationObject).toMatch(/^[\w-]+$/);
		expect(complete.body.credential.response.transports).toContain('internal');
	});

	test('"Not now" is remembered and the offer does not return', async ({ page, context }) => {
		const api = await mockApi(page, { signedIn: false, workspaces: [] });
		await virtualAuthenticator(context, page);
		await passwordSignIn(page);
		await offer(page).getByRole('button', { name: 'Not now' }).click();
		await expect(offer(page)).toBeHidden();

		api.signedIn = false;
		await passwordSignIn(page);
		await expect(page.getByRole('heading', { name: /set up your workspace/i })).toBeVisible();
		await expect(offer(page)).toBeHidden();
		expect(api.passkeys).toEqual([]);
	});

	test('is not shown to an account that already has a passkey or without WebAuthn', async ({ page, browser }) => {
		await mockApi(page, { signedIn: false, workspaces: [], passkeys: [{ id: 'passkey-1', name: 'Phone' }] });
		await passwordSignIn(page);
		await expect(page.getByRole('heading', { name: /set up your workspace/i })).toBeVisible();
		await expect(offer(page)).toBeHidden();

		const other = await browser.newPage();
		await other.addInitScript(() => {
			// @ts-expect-error simulate a browser that never had the API
			delete window.PublicKeyCredential;
		});
		const api = await mockApi(other, { signedIn: false, workspaces: [] });
		await passwordSignIn(other);
		await expect(other.getByRole('heading', { name: /set up your workspace/i })).toBeVisible();
		await expect(offer(other)).toBeHidden();
		expect(api.calls).toEqual([]);
		await other.close();
	});
});

test('the reset page says that passkeys are removed', async ({ page }) => {
	await mockApi(page, { signedIn: false });
	await page.goto('/reset-password?token=example');
	await expect(page.getByText('Resetting your password signs you out everywhere and removes your passkeys.', { exact: false })).toBeVisible();
});

test.describe('passkey settings', () => {
	const section = (page: Page) => page.getByRole('region', { name: 'Passkeys' });

	test('adds a passkey after confirming the password on an old session', async ({ page, context }) => {
		const api = await mockApi(page);
		// The session is not fresh: the server wants the password once.
		api.handlers['POST /api/auth/passkey/register/begin'] = (route, body) => {
			if (!body?.password)
				return route.fulfill({ status: 403, json: { error: { code: 'REAUTH_REQUIRED', message: 'Enter your password to add a passkey' } } });
			if (body.password !== 'Password123!!')
				return route.fulfill({ status: 403, json: { error: { code: 'INVALID_PASSWORD', message: 'The password is not correct' } } });
			return route.fulfill({ json: creationOptions() });
		};
		const authenticator = await virtualAuthenticator(context, page);
		await page.goto('/test/settings/profile');
		await expect(section(page).getByText('No passkeys yet.')).toBeVisible();

		await section(page).getByRole('button', { name: 'Add a passkey' }).click();
		const dialog = page.getByRole('dialog', { name: 'Add a passkey' });
		await dialog.getByLabel('Name').fill('Work laptop');
		await expect(dialog.getByLabel('Password', { exact: true })).toBeHidden();
		await dialog.getByRole('button', { name: 'Add a passkey' }).click();

		const password = dialog.getByRole('textbox', { name: 'Password', exact: true });
		await expect(password).toBeVisible();
		await expect(dialog.getByText('Confirm your password to add a passkey.')).toBeVisible();
		expect(await authenticator.credentials()).toHaveLength(0);
		await password.fill('Wrong-password-1');
		await dialog.getByRole('button', { name: 'Add a passkey' }).click();
		await expect(page.getByText('The password is not correct')).toBeVisible();
		await expect(dialog).toBeVisible();

		await password.fill('Password123!!');
		await dialog.getByRole('button', { name: 'Add a passkey' }).click();
		await expect(dialog).toBeHidden();
		await expect(section(page).getByText('Work laptop')).toBeVisible();
		await expect(section(page).getByText('Never used')).toBeVisible();
		expect(await authenticator.credentials()).toHaveLength(1);
		expect(api.passkeys.map((p) => p.name)).toEqual(['Work laptop']);
	});

	test('lists, renames and removes passkeys', async ({ page }) => {
		const api = await mockApi(page, {
			passkeys: [
				{ id: 'passkey-1', name: 'Laptop', transports: ['internal'], aaguid: '', backed_up: false, created_at: '2026-10-01T09:00:00Z', last_used_at: new Date().toISOString(), current: true },
				{ id: 'passkey-2', name: 'Phone', transports: ['hybrid'], aaguid: '', backed_up: true, created_at: '2026-10-02T09:00:00Z', last_used_at: null, current: false }
			]
		});
		await page.goto('/test/settings/profile');
		const rows = section(page).getByRole('listitem');
		await expect(rows).toHaveCount(2);
		await expect(rows.nth(0)).toContainText('Laptop');
		await expect(rows.nth(0)).toContainText('This session');
		await expect(rows.nth(1)).toContainText('Synced');
		await expect(rows.nth(1)).toContainText('Never used');

		await section(page).getByRole('button', { name: 'Rename Phone' }).click();
		const rename = page.getByRole('dialog', { name: 'Rename passkey' });
		await expect(rename.getByLabel('Name')).toHaveValue('Phone');
		await rename.getByLabel('Name').fill('Pixel 9');
		await rename.getByRole('button', { name: 'Save' }).click();
		await expect(rename).toBeHidden();
		await expect(rows.nth(1)).toContainText('Pixel 9');
		expect(api.calls.at(-1)).toMatchObject({ method: 'PATCH', path: '/api/auth/passkey/credentials/passkey-2', body: { name: 'Pixel 9' } });

		// Removing asks first; cancelling keeps the passkey.
		await section(page).getByRole('button', { name: 'Remove Pixel 9' }).click();
		const confirm = page.getByRole('alertdialog', { name: 'Remove passkey?' });
		await expect(confirm).toContainText('Pixel 9');
		await confirm.getByRole('button', { name: 'Cancel' }).click();
		await expect(rows).toHaveCount(2);

		await section(page).getByRole('button', { name: 'Remove Pixel 9' }).click();
		await confirm.getByRole('button', { name: 'Remove' }).click();
		await expect(rows).toHaveCount(1);
		await expect(page.getByText('Passkey removed')).toBeVisible();
		expect(api.calls.at(-1)).toMatchObject({ method: 'DELETE', path: '/api/auth/passkey/credentials/passkey-2' });
	});

	test('explains why the last passkey of a passwordless account stays', async ({ page }) => {
		const api = await mockApi(page, {
			passkeys: [{ id: 'passkey-1', name: 'Only key', transports: [], aaguid: '', backed_up: false, created_at: '2026-10-01T09:00:00Z', last_used_at: null, current: true }]
		});
		api.handlers['DELETE /api/auth/passkey/credentials/passkey-1'] = (route) =>
			route.fulfill({ status: 409, json: { error: { code: 'LAST_PASSKEY', message: 'This account has no password, so its last passkey cannot be removed' } } });
		await page.goto('/test/settings/profile');
		await section(page).getByRole('button', { name: 'Remove Only key' }).click();
		await page.getByRole('alertdialog').getByRole('button', { name: 'Remove' }).click();
		await expect(page.getByText('This account has no password, so its last passkey cannot be removed.')).toBeVisible();
		await expect(section(page).getByRole('listitem')).toHaveCount(1);
	});

	test('keeps existing passkeys manageable but offers no new ones without WebAuthn or on an instance without passkeys', async ({ page, browser }) => {
		const existing = [{ id: 'passkey-1', name: 'Laptop', transports: [], aaguid: '', backed_up: false, created_at: '2026-10-01T09:00:00Z', last_used_at: null, current: false }];
		await page.addInitScript(() => {
			// @ts-expect-error simulate a browser that never had the API
			delete window.PublicKeyCredential;
		});
		await mockApi(page, { passkeys: existing });
		await page.goto('/test/settings/profile');
		await expect(section(page).getByText('This browser does not support passkeys.')).toBeVisible();
		await expect(section(page).getByRole('button', { name: 'Add a passkey' })).toBeHidden();
		await expect(section(page).getByRole('button', { name: 'Remove Laptop' })).toBeVisible();

		const other = await browser.newPage();
		await mockApi(other, { passkeys: existing, passkeysEnabled: false });
		await other.goto('/test/settings/profile');
		await expect(other.getByRole('region', { name: 'Passkeys' }).getByText('Passkeys are not available on this instance.')).toBeVisible();
		await expect(other.getByRole('region', { name: 'Passkeys' }).getByRole('button', { name: 'Add a passkey' })).toBeHidden();
		await other.close();
	});
});
