import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const userId = '00000000-0000-0000-0000-000000000001';
const savedAt = '2026-10-01T10:00:00Z';
const token = 'sk-ant-oat01-settings-fixture-token-0001';

const policy = {
	workspace_id: workspaceId,
	enabled: true,
	max_concurrent_machines: 5,
	max_machines_per_user: 2,
	max_daily_agent_runs: 25,
	max_disk_gb: 100,
	allowed_providers: ['codex', 'claude-code'],
	allowed_repositories: [],
	allow_custom_providers: false,
	allow_unattended_automations: false,
	disk_quota_mode: 'hard'
};

type SignIn = {
	provider: string;
	supports_token: boolean;
	token: { saved_at: string; expires_at: string | null; expired: boolean } | null;
	terminal: { saved_at: string } | null;
};

async function mockSettings(page: Page, signIns: SignIn[] | 'error') {
	const requests: Array<{ method: string; path: string; search: string; body?: unknown }> = [];
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: userId, email: 'member@example.com', name: 'Member', display_name: 'Member' } });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'member' } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machine-policy') return route.fulfill({ json: policy });
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path.startsWith('/api/workspaces/test/agent-sign-ins')) {
			const body = request.method() === 'PUT' ? request.postDataJSON() : undefined;
			requests.push({ method: request.method(), path, search: url.search, body });
			if (signIns === 'error') return route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } });
			if (request.method() === 'GET') return route.fulfill({ json: signIns });
			if (request.method() === 'PUT') {
				const provider = path.split('/').at(-2) ?? '';
				// Only a Claude Code token has an expiry estimate.
				const saved = {
					provider,
					supports_token: true,
					token: {
						saved_at: '2026-10-28T10:00:00Z',
						expires_at: provider === 'claude-code' ? '2027-10-28T10:00:00Z' : null,
						expired: false
					},
					terminal: null
				};
				signIns = signIns.map((item) => (item.provider === provider ? saved : item));
				return route.fulfill({ json: saved });
			}
			if (request.method() === 'DELETE') {
				const provider = path.split('/').pop();
				const kind = url.searchParams.get('kind');
				signIns = signIns.map((item) =>
					item.provider !== provider
						? item
						: { ...item, token: kind === 'credentials' ? item.token : null, terminal: kind === 'token' ? item.terminal : null }
				);
				return route.fulfill({ status: 204, body: '' });
			}
		}
		if (path.startsWith('/api/workspaces/test/')) return route.fulfill({ json: [] });
		return route.fallback();
	});
	return requests;
}

test('a member saves a Claude Code token and removes their sign-ins', async ({ page }) => {
	const requests = await mockSettings(page, [
		{ provider: 'claude-code', supports_token: true, token: null, terminal: null },
		{ provider: 'codex', supports_token: false, token: null, terminal: { saved_at: savedAt } }
	]);
	await page.goto('/test/settings/agents');
	const card = page.getByRole('region', { name: 'Your sign-ins' });
	await expect(card).toBeVisible();
	const claude = card.getByTestId('sign-in-claude-code');
	const codex = card.getByTestId('sign-in-codex');
	await expect(claude.getByText('Not signed in', { exact: true })).toBeVisible();
	await expect(claude.getByText('claude setup-token', { exact: true })).toBeVisible();
	await expect(codex.getByText('Signed in', { exact: true })).toBeVisible();
	await expect(codex.getByText('Sign-in saved on Oct 1, 2026.')).toBeVisible();
	await expect(codex.getByText('codex login --device-auth', { exact: false })).toBeVisible();
	await expect(codex.getByLabel('Claude Code token', { exact: true })).toHaveCount(0);

	const field = claude.getByLabel('Claude Code token', { exact: true });
	await expect(field).toHaveAttribute('type', 'password');
	await claude.getByRole('button', { name: 'Save token' }).click();
	await expect(claude.getByText('Paste the token first.')).toBeVisible();
	await field.fill('sk-ant-oat01 with spaces in it');
	await claude.getByRole('button', { name: 'Save token' }).click();
	await expect(claude.getByText('That doesn’t look like a token.', { exact: false })).toBeVisible();
	expect(requests.filter((request) => request.method === 'PUT')).toHaveLength(0);

	await field.fill(` ${token} `);
	await claude.getByRole('button', { name: 'Save token' }).click();
	await expect(claude.getByText('Token set', { exact: true })).toBeVisible();
	await expect(claude.getByText('Token saved Oct 28, 2026, expires around Oct 28, 2027.')).toBeVisible();
	await expect(field).toHaveValue('');
	await expect(field).toHaveAttribute('placeholder', 'Saved — paste a new token to replace it');
	const put = requests.find((request) => request.method === 'PUT');
	expect(put).toEqual({ method: 'PUT', path: '/api/workspaces/test/agent-sign-ins/claude-code/token', search: '', body: { token } });
	await expect(page.getByText(token)).toHaveCount(0);

	await claude.getByRole('button', { name: 'Remove the Claude Code token' }).click();
	const confirm = page.getByRole('alertdialog');
	await expect(confirm.getByText('use your claude.ai settings', { exact: false })).toBeVisible();
	await confirm.getByRole('button', { name: 'Delete', exact: true }).click();
	await expect(claude.getByText('Not signed in', { exact: true })).toBeVisible();

	await codex.getByRole('button', { name: 'Remove Codex sign-in' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Delete', exact: true }).click();
	await expect(codex.getByText('Not signed in', { exact: true })).toBeVisible();
	expect(requests.filter((request) => request.method === 'DELETE').map((request) => request.path + request.search)).toEqual([
		'/api/workspaces/test/agent-sign-ins/claude-code?kind=token',
		'/api/workspaces/test/agent-sign-ins/codex?kind=credentials'
	]);
});

test('a member saves, replaces and removes an OpenCode API key', async ({ page }) => {
	const key = 'fixture-opencode-api-key-settings-0001';
	const requests = await mockSettings(page, [
		{ provider: 'claude-code', supports_token: true, token: null, terminal: null },
		{ provider: 'codex', supports_token: false, token: null, terminal: null },
		{ provider: 'opencode', supports_token: true, token: null, terminal: null }
	]);
	await page.goto('/test/settings/agents');
	const card = page.getByRole('region', { name: 'Your sign-ins' });
	const claude = card.getByTestId('sign-in-claude-code');
	const opencode = card.getByTestId('sign-in-opencode');
	await expect(opencode).toContainText('OpenCode');
	await expect(opencode.getByText('Not signed in', { exact: true })).toBeVisible();
	await expect(opencode.getByText('OPENCODE_API_KEY', { exact: false })).toBeVisible();
	await expect(opencode.getByText('claude setup-token', { exact: false })).toHaveCount(0);
	await expect(claude.getByLabel('OpenCode API key', { exact: true })).toHaveCount(0);

	const field = opencode.getByLabel('OpenCode API key', { exact: true });
	await expect(field).toHaveAttribute('type', 'password');
	await opencode.getByRole('button', { name: 'Save key' }).click();
	await expect(opencode.getByText('Paste the API key as a single line, without spaces.')).toBeVisible();
	await field.fill('a key with spaces in it 0001');
	await opencode.getByRole('button', { name: 'Save key' }).click();
	await expect(opencode.getByText('Paste the API key as a single line, without spaces.')).toBeVisible();
	expect(requests.filter((request) => request.method === 'PUT')).toHaveLength(0);

	await field.fill(` ${key} `);
	await opencode.getByRole('button', { name: 'Save key' }).click();
	await expect(opencode.getByText('Key saved', { exact: true })).toBeVisible();
	await expect(opencode.getByText('Key saved Oct 28, 2026.', { exact: true })).toBeVisible();
	await expect(opencode.getByText('expires', { exact: false })).toHaveCount(0);
	await expect(field).toHaveValue('');
	await expect(field).toHaveAttribute('placeholder', 'Saved — paste a new key to replace it');
	await expect(page.getByText(key)).toHaveCount(0);
	await expect(claude.getByText('Not signed in', { exact: true })).toBeVisible();

	await field.fill(`${key}-replacement`);
	await opencode.getByRole('button', { name: 'Save key' }).click();
	await expect(field).toHaveValue('');
	expect(requests.filter((request) => request.method === 'PUT')).toEqual([
		{ method: 'PUT', path: '/api/workspaces/test/agent-sign-ins/opencode/token', search: '', body: { token: key } },
		{
			method: 'PUT',
			path: '/api/workspaces/test/agent-sign-ins/opencode/token',
			search: '',
			body: { token: `${key}-replacement` }
		}
	]);

	await opencode.getByRole('button', { name: 'Remove key' }).click();
	const confirm = page.getByRole('alertdialog');
	await expect(confirm.getByText('use your OpenCode account', { exact: false })).toBeVisible();
	await expect(confirm.getByText('claude.ai', { exact: false })).toHaveCount(0);
	await confirm.getByRole('button', { name: 'Delete', exact: true }).click();
	await expect(opencode.getByText('Not signed in', { exact: true })).toBeVisible();
	expect(requests.filter((request) => request.method === 'DELETE').map((request) => request.path + request.search)).toEqual([
		'/api/workspaces/test/agent-sign-ins/opencode?kind=token'
	]);
});

test('an expired token asks for a new one', async ({ page }) => {
	await mockSettings(page, [
		{
			provider: 'claude-code',
			supports_token: true,
			token: { saved_at: '2025-10-01T10:00:00Z', expires_at: '2026-10-01T10:00:00Z', expired: true },
			terminal: null
		},
		{ provider: 'codex', supports_token: false, token: null, terminal: null }
	]);
	await page.goto('/test/settings/agents');
	const claude = page.getByTestId('sign-in-claude-code');
	await expect(claude.getByText('Token expired', { exact: true })).toBeVisible();
	await expect(claude.getByText('Create a new one and paste it below.', { exact: false })).toBeVisible();
});

test('sign-ins that fail to load can be retried', async ({ page }) => {
	await mockSettings(page, 'error');
	await page.goto('/test/settings/agents');
	await expect(page.getByText('Your sign-ins could not be loaded.')).toBeVisible();
	await expect(page.getByRole('region', { name: 'Your sign-ins' }).getByRole('button', { name: 'Refresh' })).toBeVisible();
});

const devicePath = '**/api/workspaces/test/agent-sign-ins/codex/device';
const deviceCode = 'ABCD-EFGHJ';
const verifyURL = 'https://auth.openai.com/codex/device';

function deviceChallenge(expiresAt = Date.now() + 60_000) {
	return {
		attempt_id: 'device-attempt-fixture',
		verification_url: verifyURL,
		user_code: deviceCode,
		expires_at: new Date(expiresAt).toISOString()
	};
}

test('a member connects ChatGPT from Settings before creating a machine', async ({ page }) => {
	const signIn: SignIn = { provider: 'codex', supports_token: false, token: null, terminal: null };
	await mockSettings(page, [signIn]);
	const challenge = deviceChallenge();
	await page.route(`${devicePath}/start`, (route) => route.fulfill({ json: challenge }));
	let polls = 0;
	await page.route(`${devicePath}/poll`, (route) => {
		expect(route.request().method()).toBe('POST');
		expect(route.request().postDataJSON()).toEqual({ attempt_id: challenge.attempt_id });
		if (++polls === 1) return route.fulfill({ json: { status: 'pending' } });
		signIn.terminal = { saved_at: savedAt };
		return route.fulfill({ json: { status: 'connected' } });
	});
	await page.goto('/test/settings/agents');
	const codex = page.getByTestId('sign-in-codex');
	await expect(codex.getByText('Not signed in', { exact: true })).toBeVisible();
	await expect(codex.getByText('codex login --device-auth', { exact: false })).toBeVisible();
	await codex.getByRole('button', { name: 'Connect ChatGPT', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Connect ChatGPT' });
	await expect(dialog.getByText(deviceCode, { exact: true })).toBeVisible();
	await expect(dialog.getByRole('link', { name: 'Open ChatGPT' })).toHaveAttribute('href', verifyURL);
	await expect(dialog.getByText('Waiting for approval in ChatGPT…')).toBeVisible();
	await expect(codex.getByText('Signed in', { exact: true })).toBeVisible();
	await expect(dialog).toHaveCount(0);
	await expect(page.getByText(deviceCode, { exact: true })).toHaveCount(0);
	await expect(codex.getByRole('button', { name: 'Connect ChatGPT', exact: true })).toBeFocused();
	await page.reload();
	await expect(codex.getByText('Sign-in saved on Oct 1, 2026.')).toBeVisible();
});

test('Escape cancels the device attempt and preserves the saved sign-in', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 667 });
	await mockSettings(page, [{ provider: 'codex', supports_token: false, token: null, terminal: { saved_at: savedAt } }]);
	const challenge = deviceChallenge();
	await page.route(`${devicePath}/start`, (route) => route.fulfill({ json: challenge }));
	await page.route(`${devicePath}/poll`, (route) => route.fulfill({ json: { status: 'pending' } }));
	await page.route(`${devicePath}/cancel`, (route) => route.fulfill({ status: 204 }));
	await page.goto('/test/settings/agents');
	const codex = page.getByTestId('sign-in-codex');
	const button = codex.getByRole('button', { name: 'Connect ChatGPT', exact: true });
	await button.click();
	const dialog = page.getByRole('dialog', { name: 'Connect ChatGPT' });
	await expect(dialog.getByText(deviceCode, { exact: true })).toBeVisible();
	await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
	const canceled = page.waitForResponse((response) => response.url().endsWith('/codex/device/cancel'));
	await page.keyboard.press('Escape');
	expect((await canceled).request().postDataJSON()).toEqual({ attempt_id: challenge.attempt_id });
	await expect(dialog).toHaveCount(0);
	await expect(button).toBeFocused();
	await expect(codex.getByText('Sign-in saved on Oct 1, 2026.')).toBeVisible();
});

test('an expired device code is cleared and a new attempt is available', async ({ page }) => {
	const now = Date.now();
	await page.clock.install({ time: now });
	await mockSettings(page, [{ provider: 'codex', supports_token: false, token: null, terminal: null }]);
	let starts = 0;
	await page.route(`${devicePath}/start`, (route) =>
		route.fulfill({
			json: starts++ === 0 ? deviceChallenge(now + 30_000) : { ...deviceChallenge(now + 120_000), user_code: 'WXYZ-QRSTU' }
		})
	);
	await page.route(`${devicePath}/poll`, (route) => route.fulfill({ json: { status: 'pending' } }));
	await page.goto('/test/settings/agents');
	const codex = page.getByTestId('sign-in-codex');
	const button = codex.getByRole('button', { name: 'Connect ChatGPT', exact: true });
	await button.click();
	const dialog = page.getByRole('dialog', { name: 'Connect ChatGPT' });
	await expect(dialog.getByText(deviceCode, { exact: true })).toBeVisible();
	await page.clock.fastForward(30_001);
	await expect(dialog.getByRole('alert')).toHaveText('This code expired. Start again to get a new code.');
	await expect(dialog.getByText(deviceCode, { exact: true })).toHaveCount(0);
	await expect(dialog.getByText('codex login --device-auth', { exact: false })).toBeVisible();
	await dialog.getByRole('button', { name: 'Close', exact: true }).click();
	await expect(codex.getByText('Not signed in', { exact: true })).toBeVisible();
	await button.click();
	await expect(dialog.getByText('WXYZ-QRSTU', { exact: true })).toBeVisible();
});

test('reopening during a canceled start waits for that attempt to be cleaned up', async ({ page }) => {
	await mockSettings(page, [{ provider: 'codex', supports_token: false, token: null, terminal: null }]);
	const first = deviceChallenge();
	const second = { ...first, attempt_id: 'second-attempt-fixture', user_code: 'WXYZ-QRSTU' };
	const calls: string[] = [];
	let releaseStart!: () => void;
	const delayedStart = new Promise<void>((resolve) => { releaseStart = resolve; });
	await page.route(`${devicePath}/start`, async (route) => {
		if (!calls.length) {
			calls.push('first start');
			await delayedStart;
			return route.fulfill({ json: first });
		}
		calls.push('second start');
		return route.fulfill({ json: second });
	});
	await page.route(`${devicePath}/cancel`, (route) => {
		calls.push(route.request().postDataJSON().attempt_id);
		return route.fulfill({ status: 204 });
	});
	await page.route(`${devicePath}/poll`, (route) => route.fulfill({ json: { status: 'pending' } }));
	await page.goto('/test/settings/agents');
	const button = page.getByTestId('sign-in-codex').getByRole('button', { name: 'Connect ChatGPT', exact: true });
	const starts = page.waitForRequest((request) => request.url().endsWith('/codex/device/start'));
	await button.click();
	await starts;
	const dialog = page.getByRole('dialog', { name: 'Connect ChatGPT' });
	await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
	await button.click();
	releaseStart();
	await expect(dialog.getByText(second.user_code, { exact: true })).toBeVisible();
	await expect(dialog.getByText(first.user_code, { exact: true })).toHaveCount(0);
	expect(calls).toEqual(['first start', first.attempt_id, 'second start']);
});

test('a failed ChatGPT sign-in offers the terminal fallback without exposing provider details', async ({ page }) => {
	await mockSettings(page, [{ provider: 'codex', supports_token: false, token: null, terminal: null }]);
	const problem = 'private provider diagnostics fixture';
	await page.route(`${devicePath}/start`, (route) =>
		route.fulfill({ status: 502, json: { error: { code: 'UPSTREAM_ERROR', message: problem } } })
	);
	await page.goto('/test/settings/agents');
	const codex = page.getByTestId('sign-in-codex');
	await codex.getByRole('button', { name: 'Connect ChatGPT', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Connect ChatGPT' });
	await expect(dialog.getByRole('alert')).toHaveText('ChatGPT sign-in could not be completed. Try again or use the terminal.');
	await expect(dialog.getByText('codex login --device-auth', { exact: false })).toBeVisible();
	await expect(page.getByText(problem, { exact: true })).toHaveCount(0);
	await dialog.getByRole('button', { name: 'Close', exact: true }).click();
	await expect(codex.getByText('Not signed in', { exact: true })).toBeVisible();
});
