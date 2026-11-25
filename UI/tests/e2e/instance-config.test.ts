import { expect, test, type Page } from '@playwright/test';

type Variable = {
	name: string;
	group: string;
	description: string;
	kind: string;
	secret: boolean;
	set: boolean;
	value?: string;
	withheld?: boolean;
	default?: string;
	source: 'environment' | 'default' | 'override';
	tunable: boolean;
	base_value?: string;
	base_source?: 'environment' | 'default';
	min?: string;
	max?: string;
	updated_at?: string;
};

const groups = ['server', 'data', 'security', 'sign_in', 'workflows', 'storage', 'email', 'push', 'backups', 'network',
	'integrations', 'assistant', 'updates', 'dev_machines'];

// What GET /api/system/config sends: a secret carries no value, only `set`.
function variables(): Variable[] {
	return [
		{ name: 'FRONTEND_URL', group: 'server', description: 'Public origin of the web app.', kind: 'string', secret: false,
			set: true, value: 'https://kuayle.example.com', default: 'http://localhost:5173', source: 'environment', tunable: false },
		{ name: 'JWT_SECRET', group: 'security', description: 'Signs sessions.', kind: 'string', secret: true, set: true,
			source: 'environment', tunable: false },
		{ name: 'WORKFLOW_PIPELINES_ENABLED', group: 'workflows', description: 'Allows saving pipeline workflows.', kind: 'bool',
			secret: false, set: true, value: 'true', default: 'true', source: 'default', tunable: true, base_value: 'true',
			base_source: 'default' },
		{ name: 'SMTP_PASSWORD', group: 'email', description: 'SMTP password.', kind: 'string', secret: true, set: false,
			source: 'default', tunable: false },
		{ name: 'BACKUP_TIMEOUT', group: 'backups', description: 'Longest a backup run may take.', kind: 'duration',
			secret: false, set: true, value: '2h', default: '2h', source: 'environment', tunable: true, base_value: '2h',
			base_source: 'environment', min: '5m', max: '72h' },
		{ name: 'VOICE_URL', group: 'assistant', description: 'Internal address of the voice service.', kind: 'string',
			secret: false, set: true, withheld: true, source: 'environment', tunable: false }
	];
}

async function setup(page: Page, admin: boolean) {
	const state = variables();
	const requests: { method: string; name?: string; body?: unknown }[] = [];
	await page.routeWebSocket('**/api/workspaces/*/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const req = route.request();
		const path = new URL(req.url()).pathname;
		const workspace = { id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'owner' };
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user', name: 'Owner', is_sysadmin: admin } });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/alpha') return route.fulfill({ json: workspace });
		if (path.startsWith('/api/system/config')) {
			const name = decodeURIComponent(path.split('/')[4] ?? '');
			requests.push({ method: req.method(), name: name || undefined, body: req.postDataJSON() ?? undefined });
			if (!admin) return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
			if (req.method() === 'GET') return route.fulfill({ json: { groups, variables: state } });
			const variable = state.find((candidate) => candidate.name === name)!;
			if (req.method() === 'PUT') {
				const value = String(req.postDataJSON().value);
				if (value === '1s')
					return route.fulfill({
						status: 400,
						json: { error: { code: 'INVALID_CONFIG_VALUE', message: 'BACKUP_TIMEOUT must be between 5m and 72h' } }
					});
				Object.assign(variable, { value: value === '90m' ? '1h30m' : value, source: 'override', updated_at: '2026-11-25T09:00:00Z' });
			} else {
				Object.assign(variable, { value: variable.base_value, source: variable.base_source, updated_at: undefined });
			}
			return route.fulfill({ json: variable });
		}
		if (path === '/api/preferences') return route.fulfill({ json: {} });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		return route.fulfill({ json: [] });
	});
	return requests;
}

test('a sysadmin sees every variable, secrets only as set or not set', async ({ page }) => {
	await setup(page, true);
	await page.goto('/alpha/settings/version');
	await page.getByRole('link', { name: 'Instance configuration', exact: true }).click();
	await expect(page.getByRole('heading', { name: 'Instance configuration', level: 1 })).toBeVisible();
	await expect(page.getByText('6 of 6 variables')).toBeVisible();
	for (const group of ['Server', 'Security', 'Workflows', 'Email', 'Backups', 'Assistant'])
		await expect(page.getByRole('heading', { name: group, level: 2 })).toBeVisible();

	const jwt = page.getByTestId('instance-config-JWT_SECRET');
	await expect(jwt.getByText('Secret', { exact: true })).toBeVisible();
	await expect(jwt.getByText('Environment', { exact: true })).toBeVisible();
	await expect(jwt.getByText('Set', { exact: true })).toBeVisible();
	await expect(jwt.getByText('Set in .env; needs a restart', { exact: true })).toBeVisible();
	await expect(jwt.getByRole('switch')).toHaveCount(0);
	await expect(jwt.getByRole('textbox')).toHaveCount(0);
	const smtp = page.getByTestId('instance-config-SMTP_PASSWORD');
	await expect(smtp.getByText('Not set', { exact: true })).toBeVisible();
	await expect(smtp.getByText('Default', { exact: true })).toBeVisible();
	await expect(page.getByTestId('instance-config-VOICE_URL').getByText('Set, hidden: it contains credentials')).toBeVisible();
	const frontend = page.getByTestId('instance-config-FRONTEND_URL');
	await expect(frontend).toContainText('https://kuayle.example.com');
	await expect(frontend).toContainText('Set in .env; needs a restart');

	await page.getByRole('searchbox', { name: 'Search variables' }).fill('smtp');
	await expect(page.getByText('1 of 6 variables')).toBeVisible();
	await expect(page.getByTestId('instance-config-SMTP_PASSWORD')).toBeVisible();
	await expect(page.getByTestId('instance-config-JWT_SECRET')).toHaveCount(0);
	await page.getByRole('searchbox', { name: 'Search variables' }).fill('no such variable');
	await expect(page.getByText('No variable matches your search.')).toBeVisible();
	await page.getByRole('searchbox', { name: 'Search variables' }).fill('backups');
	await expect(page.getByTestId('instance-config-BACKUP_TIMEOUT')).toBeVisible();
	await expect(page.getByText('1 of 6 variables')).toBeVisible();
});

test('a sysadmin changes runtime values, sees a refusal, and resets them', async ({ page }) => {
	const requests = await setup(page, true);
	await page.goto('/alpha/settings/instance-config');
	const pipelines = page.getByTestId('instance-config-WORKFLOW_PIPELINES_ENABLED');
	await expect(pipelines).toContainText('Applies without a restart');
	await pipelines.getByRole('switch', { name: 'Value of WORKFLOW_PIPELINES_ENABLED' }).click();
	await expect(pipelines).toContainText('Changed here');
	await expect(pipelines.getByRole('switch')).not.toBeChecked();
	await pipelines.getByRole('button', { name: 'Reset to true' }).click();
	await expect(pipelines.getByText('Default', { exact: true })).toBeVisible();
	await expect(pipelines.getByRole('switch')).toBeChecked();
	await expect(pipelines.getByRole('button', { name: /Reset/ })).toHaveCount(0);

	const timeout = page.getByTestId('instance-config-BACKUP_TIMEOUT');
	const field = timeout.getByRole('textbox', { name: 'Value of BACKUP_TIMEOUT' });
	await expect(timeout).toContainText('Between 5m and 72h');
	await expect(timeout.getByRole('button', { name: 'Save' })).toBeDisabled();
	await field.fill('1s');
	await timeout.getByRole('button', { name: 'Save' }).click();
	await expect(page.getByText('BACKUP_TIMEOUT must be between 5m and 72h')).toBeVisible();
	await expect(timeout.getByText('Environment', { exact: true })).toBeVisible();
	await field.fill('90m');
	await field.press('Enter');
	await expect(field).toHaveValue('1h30m');
	await expect(timeout).toContainText('Changed here');
	await timeout.getByRole('button', { name: 'Reset to 2h' }).click();
	await expect(field).toHaveValue('2h');
	await expect(timeout.getByText('Environment', { exact: true })).toBeVisible();

	expect(requests.filter((request) => request.method !== 'GET')).toEqual([
		{ method: 'PUT', name: 'WORKFLOW_PIPELINES_ENABLED', body: { value: 'false' } },
		{ method: 'DELETE', name: 'WORKFLOW_PIPELINES_ENABLED', body: undefined },
		{ method: 'PUT', name: 'BACKUP_TIMEOUT', body: { value: '1s' } },
		{ method: 'PUT', name: 'BACKUP_TIMEOUT', body: { value: '90m' } },
		{ method: 'DELETE', name: 'BACKUP_TIMEOUT', body: undefined }
	]);
});

test('members see no link, no configuration and send no request on direct navigation', async ({ page }) => {
	const requests = await setup(page, false);
	await page.goto('/alpha/settings/version');
	await expect(page.getByRole('link', { name: 'Version' })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Instance configuration', exact: true })).toHaveCount(0);
	await page.goto('/alpha/settings/instance-config');
	await expect(page.getByText('Only instance sysadmins can view the instance configuration.')).toBeVisible();
	await expect(page.getByRole('searchbox')).toHaveCount(0);
	await expect(page.getByRole('switch')).toHaveCount(0);
	expect(requests).toEqual([]);
});
