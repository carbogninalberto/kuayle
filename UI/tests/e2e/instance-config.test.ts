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
	restart_editable?: boolean;
	rule?: { format?: string; choices?: string[]; min?: string; max?: string; allow_empty?: boolean };
};

type Run = {
	id: string;
	status: 'restarting' | 'applying' | 'confirmed' | 'rolled_back';
	requested_at: string;
	failure?: string;
	changes: { name: string; from: string; to: string }[];
};

// What the API does with an apply: it comes back healthy with the new values,
// or it does not and the previous ones return. `supported` is false when
// nothing would start the API again.
type Restart = { supported: boolean; outcome: 'confirmed' | 'rolled_back' };

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

// Variables read at startup: three editable ones and the read-only ones above.
function restartVariables(): Variable[] {
	return [
		{ name: 'SMTP_PORT', group: 'email', description: 'SMTP server port.', kind: 'int', secret: false, set: true, value: '587',
			default: '587', source: 'environment', tunable: false, restart_editable: true, base_value: '587', base_source: 'environment',
			rule: { min: '1', max: '65535' } },
		{ name: 'PASSKEYS_ENABLED', group: 'sign_in', description: 'Offers passkey sign-in.', kind: 'bool', secret: false, set: true,
			value: 'true', default: 'true', source: 'default', tunable: false, restart_editable: true, base_value: 'true',
			base_source: 'default', rule: {} },
		{ name: 'SSO_OIDC_DEFAULT_ROLE', group: 'sign_in', description: 'Role of a person who joins through instance OIDC.',
			kind: 'string', secret: false, set: true, value: 'member', default: 'member', source: 'default', tunable: false,
			restart_editable: true, base_value: 'member', base_source: 'default', rule: { choices: ['member', 'guest'] } }
	];
}

async function setup(page: Page, admin: boolean, restart?: Restart) {
	const state = restart ? [...variables(), ...restartVariables()] : variables();
	let run: Run | undefined;
	let pollsSinceApply = 0;
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
		if (path.startsWith('/api/system/config/restart/')) {
			const body = req.postDataJSON() as { changes: Record<string, string | null> };
			requests.push({ method: req.method(), name: path.split('/')[5], body });
			const issues = Object.entries(body.changes)
				.filter(([name, value]) => name === 'SMTP_PORT' && (Number(value) < 1 || Number(value) > 65535))
				.map(([name]) => ({ name, message: 'SMTP_PORT must be between 1 and 65535' }));
			const changes = Object.entries(body.changes).map(([name, value]) => {
				const variable = state.find((candidate) => candidate.name === name)!;
				return { name, from: variable.value ?? '', to: value ?? variable.base_value ?? '' };
			});
			if (path.endsWith('/validate')) return route.fulfill({ json: { valid: issues.length === 0, changes, issues } });
			if (issues.length > 0)
				return route.fulfill({
					status: 400,
					json: { error: { code: 'INVALID_CONFIG_VALUE', message: issues[0].message, details: issues.map((i) => ({ field: i.name, message: i.message })) } }
				});
			run = { id: 'run-1', status: 'restarting', requested_at: '2026-11-25T09:00:00Z', changes };
			pollsSinceApply = 0;
			return route.fulfill({ status: 202, json: run });
		}
		if (path.startsWith('/api/system/config')) {
			const name = decodeURIComponent(path.split('/')[4] ?? '');
			requests.push({ method: req.method(), name: name || undefined, body: req.postDataJSON() ?? undefined });
			if (!admin) return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'Forbidden' } } });
			if (req.method() === 'GET') {
				// The API is down for the first poll after an apply, then decided.
				if (run && (run.status === 'restarting' || run.status === 'applying')) {
					if (pollsSinceApply++ === 0) return route.abort('connectionrefused');
					run = { ...run, status: restart!.outcome, failure: restart!.outcome === 'rolled_back' ? 'the API was not healthy within 1m30s of restarting with the new settings' : undefined };
					if (run.status === 'confirmed')
						for (const change of run.changes) Object.assign(state.find((v) => v.name === change.name)!, { value: change.to, source: 'override' });
				}
				return route.fulfill({ json: { groups, variables: state, restart: { supported: restart?.supported ?? true, run } } });
			}
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

test('restart-only settings are marked, and read-only ones are told apart from them', async ({ page }) => {
	await setup(page, true, { supported: true, outcome: 'confirmed' });
	await page.goto('/alpha/settings/instance-config');
	const port = page.getByTestId('instance-config-SMTP_PORT');
	await expect(port.getByText('Needs restart', { exact: true })).toBeVisible();
	await expect(port.getByRole('textbox', { name: 'Value of SMTP_PORT' })).toHaveValue('587');
	await expect(port).toContainText('Between 1 and 65535');
	await expect(port).toContainText('Applies after “Apply and restart”');
	await expect(page.getByTestId('instance-config-PASSKEYS_ENABLED').getByRole('switch')).toBeChecked();
	for (const name of ['JWT_SECRET', 'FRONTEND_URL', 'SMTP_PASSWORD']) {
		const row = page.getByTestId(`instance-config-${name}`);
		await expect(row.getByText('Read-only', { exact: true })).toBeVisible();
		await expect(row.getByRole('textbox')).toHaveCount(0);
		await expect(row.getByRole('switch')).toHaveCount(0);
	}
	await expect(page.getByTestId('instance-config-WORKFLOW_PIPELINES_ENABLED').getByText('Read-only')).toHaveCount(0);
	await expect(page.getByTestId('instance-restart-panel')).toHaveCount(0);
});

test('a sysadmin checks, applies and restarts, and sees the settings take effect', async ({ page }) => {
	const requests = await setup(page, true, { supported: true, outcome: 'confirmed' });
	await page.goto('/alpha/settings/instance-config');
	const port = page.getByTestId('instance-config-SMTP_PORT');
	const field = port.getByRole('textbox', { name: 'Value of SMTP_PORT' });

	await field.fill('0');
	const panel = page.getByTestId('instance-restart-panel');
	await expect(panel).toContainText('Unapplied changes: 1');
	await panel.getByRole('button', { name: 'Check changes' }).click();
	await expect(port).toContainText('SMTP_PORT must be between 1 and 65535');
	await expect(field).toHaveAttribute('aria-invalid', 'true');
	await panel.getByRole('button', { name: 'Apply and restart' }).click();
	await expect(page.getByRole('alertdialog')).toHaveCount(0, { timeout: 1000 });

	await field.fill('2525');
	await expect(port).not.toContainText('SMTP_PORT must be between');
	await panel.getByRole('button', { name: 'Check changes' }).click();
	await expect(panel).toContainText('The changes are valid.');
	await panel.getByRole('button', { name: 'Apply and restart' }).click();
	const dialog = page.getByRole('alertdialog', { name: 'Apply and restart the API?' });
	await expect(dialog.getByTestId('instance-restart-changes')).toContainText('SMTP_PORT: 587 → 2525');
	await dialog.getByRole('button', { name: 'Apply and restart' }).click();

	const progress = page.getByTestId('instance-restart-progress');
	await expect(progress).toContainText('Restarting the API…');
	await expect(port.getByRole('textbox')).toBeDisabled();
	await expect(page.getByText('The new settings are applied.').first()).toBeVisible({ timeout: 15_000 });
	await expect(progress).toHaveCount(0);
	await expect(page.getByTestId('instance-restart-panel')).toHaveCount(0);
	await expect(port.getByRole('textbox', { name: 'Value of SMTP_PORT' })).toHaveValue('2525');
	await expect(port.getByText('Changed here', { exact: true })).toBeVisible();
	await expect(port.getByRole('button', { name: 'Return to .env value' })).toBeVisible();

	// Check, apply (checked again), check, apply (checked again), then the apply itself.
	expect(requests.filter((request) => request.method !== 'GET').map((request) => `${request.method} ${request.name}`)).toEqual([
		...Array(4).fill('POST validate'),
		'POST apply'
	]);
	expect(requests.filter((request) => request.method !== 'GET').at(-1)?.body).toEqual({ changes: { SMTP_PORT: '2525' } });
});

test('a restart that does not come back healthy is rolled back and reported', async ({ page }) => {
	await setup(page, true, { supported: true, outcome: 'rolled_back' });
	await page.goto('/alpha/settings/instance-config');
	const passkeys = page.getByTestId('instance-config-PASSKEYS_ENABLED');
	await passkeys.getByRole('switch', { name: 'Value of PASSKEYS_ENABLED' }).click();
	await page.getByTestId('instance-config-SSO_OIDC_DEFAULT_ROLE').getByRole('combobox').click();
	await page.getByRole('option', { name: 'guest' }).click();
	const panel = page.getByTestId('instance-restart-panel');
	await expect(panel).toContainText('Unapplied changes: 2');
	await panel.getByRole('button', { name: 'Apply and restart' }).click();
	const dialog = page.getByRole('alertdialog');
	await expect(dialog.getByTestId('instance-restart-changes')).toContainText('PASSKEYS_ENABLED: true → false');
	await expect(dialog.getByTestId('instance-restart-changes')).toContainText('SSO_OIDC_DEFAULT_ROLE: member → guest');
	await dialog.getByRole('button', { name: 'Apply and restart' }).click();

	const rolledBack = page.getByTestId('instance-restart-rolled-back');
	await expect(rolledBack).toContainText('The previous settings were restored', { timeout: 15_000 });
	await expect(rolledBack).toContainText('not healthy within 1m30s');
	await expect(passkeys.getByRole('switch')).toBeChecked();
	await expect(passkeys.getByText('Changed here')).toHaveCount(0);
	await rolledBack.getByRole('button', { name: 'Dismiss' }).click();
	await expect(rolledBack).toHaveCount(0);
});

test('without a supervisor the changes can be checked but not applied', async ({ page }) => {
	const requests = await setup(page, true, { supported: false, outcome: 'confirmed' });
	await page.goto('/alpha/settings/instance-config');
	await page.getByTestId('instance-config-PASSKEYS_ENABLED').getByRole('switch').click();
	const panel = page.getByTestId('instance-restart-panel');
	await expect(panel.getByTestId('instance-restart-unsupported')).toContainText('RESTART_SUPERVISED=true');
	await expect(panel.getByRole('button', { name: 'Apply and restart' })).toBeDisabled();
	await panel.getByRole('button', { name: 'Check changes' }).click();
	await expect(panel).toContainText('The changes are valid.');
	await panel.getByRole('button', { name: 'Discard all' }).click();
	await expect(page.getByTestId('instance-restart-panel')).toHaveCount(0);
	await expect(page.getByTestId('instance-config-PASSKEYS_ENABLED').getByRole('switch')).toBeChecked();
	expect(requests.filter((request) => request.name === 'apply')).toEqual([]);
});
