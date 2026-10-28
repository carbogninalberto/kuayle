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
	max_runtime_minutes: 480,
	max_disk_gb: 100,
	idle_pause_minutes: 240,
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
				const saved = {
					provider: 'claude-code',
					supports_token: true,
					token: { saved_at: '2026-10-28T10:00:00Z', expires_at: '2027-10-28T10:00:00Z', expired: false },
					terminal: null
				};
				signIns = signIns.map((item) => (item.provider === 'claude-code' ? saved : item));
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
	await expect(codex.getByText('Signed in via terminal', { exact: true })).toBeVisible();
	await expect(codex.getByText('Signed in from a machine’s terminal on Oct 1, 2026.')).toBeVisible();
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

	await codex.getByRole('button', { name: 'Remove the Codex terminal sign-in' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Delete', exact: true }).click();
	await expect(codex.getByText('Not signed in', { exact: true })).toBeVisible();
	expect(requests.filter((request) => request.method === 'DELETE').map((request) => request.path + request.search)).toEqual([
		'/api/workspaces/test/agent-sign-ins/claude-code?kind=token',
		'/api/workspaces/test/agent-sign-ins/codex?kind=credentials'
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
