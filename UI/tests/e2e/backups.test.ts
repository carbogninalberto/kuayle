import { expect, test, type Page } from '@playwright/test';

const settings = {
	enabled: true,
	schedule_kind: 'daily',
	schedule_time: '02:30',
	schedule_weekday: 0,
	schedule_cron: '',
	retention_kind: 'count',
	retention_value: 7,
	s3_enabled: true,
	s3_endpoint: 'https://s3.example.com',
	s3_bucket: 'kuayle-backups',
	s3_region: 'eu-west-1',
	s3_prefix: 'prod',
	has_access_key: true,
	has_secret_key: true,
	encryption_enabled: false,
	has_passphrase: false,
	next_run_at: '2026-11-16T02:30:00Z',
	updated_at: '2026-11-15T10:00:00Z',
	backup_dir: '/var/backups/kuayle'
};

const run = (over: Record<string, unknown>) => ({
	id: 'r1',
	kind: 'manual',
	status: 'succeeded',
	upload_status: 'uploaded',
	started_at: '2026-11-15T09:00:00Z',
	finished_at: '2026-11-15T09:00:42Z',
	duration_ms: 42_000,
	size_bytes: 50 * 1024 * 1024,
	checksum_sha256: 'abcdef0123456789'.repeat(4),
	file_name: 'kuayle.dump',
	s3_key: 'prod/kuayle.dump',
	s3_bucket: 'kuayle-backups',
	destination: 'local:/var/backups/kuayle + s3://kuayle-backups/prod',
	encrypted: false,
	error: null,
	verified_at: null,
	verify_error: null,
	deleted_at: null,
	...over
});

async function setup(page: Page, isSysadmin: boolean) {
	const state = { puts: [] as Record<string, unknown>[], runRequests: 0, tests: [] as Record<string, unknown>[] };
	await page.routeWebSocket('**/api/workspaces/*/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'u1', email: 'admin@example.com', name: 'Admin', display_name: 'Admin', avatar_url: null, is_sysadmin: isSysadmin }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'owner' }] });
		if (path === '/api/workspaces/alpha')
			return route.fulfill({ json: { id: 'alpha', slug: 'alpha', name: 'Alpha', current_user_role: 'owner' } });
		if (path.startsWith('/api/system/backups')) {
			if (!isSysadmin) return route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message: 'forbidden' } } });
			if (path === '/api/system/backups/settings' && request.method() === 'PUT') {
				const body = request.postDataJSON();
				state.puts.push(body);
				return route.fulfill({ json: { ...settings, ...body, has_passphrase: false } });
			}
			if (path === '/api/system/backups/settings') return route.fulfill({ json: settings });
			if (path === '/api/system/backups/test-connection') {
				state.tests.push(request.postDataJSON());
				return route.fulfill({ json: { ok: true } });
			}
			if (path === '/api/system/backups/runs')
				return route.fulfill({
					json: {
						runs: [
							run({}),
							run({ id: 'r2', kind: 'pre_upgrade', upload_status: 'none', destination: 'host' }),
							run({ id: 'r3', kind: 'scheduled', deleted_at: '2026-11-10T00:00:00Z' })
						]
					}
				});
			if (path === '/api/system/backups/run') {
				state.runRequests++;
				return route.fulfill({ status: 409, json: { error: { code: 'BACKUP_RUNNING', message: 'a backup is already running' } } });
			}
		}
		if (path === '/api/preferences') return route.fulfill({ json: {} });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (/\/workspaces\/alpha\/(teams|projects|labels|members|views)$/.test(path)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}

test('a sysadmin configures backups without resending stored secrets', async ({ page }) => {
	const state = await setup(page, true);
	await page.goto('/alpha/settings/version');
	await page.getByRole('link', { name: 'Backups' }).click();
	await expect(page).toHaveURL(/\/alpha\/settings\/backups$/);
	await expect(page.getByRole('heading', { name: 'Backups', level: 1 })).toBeVisible();

	// Stored secrets are reported, never shown.
	const accessKey = page.getByLabel('Access key');
	await expect(accessKey).toHaveValue('');
	await expect(accessKey).toHaveAttribute('placeholder', 'Saved — leave blank to keep');

	// History shows the shortened checksum and blocks verification where it cannot run.
	const rows = page.getByTestId('backup-run');
	await expect(rows).toHaveCount(3);
	await expect(rows.nth(0).getByText('abcdef012345', { exact: true })).toBeVisible();
	await expect(rows.nth(0).getByText('50 MB')).toBeVisible();
	await expect(rows.nth(1).getByRole('button', { name: 'Verify' })).toHaveAttribute('aria-disabled', 'true');
	await expect(rows.nth(2).getByText('Removed by retention', { exact: true })).toBeVisible();

	await page.getByLabel('Bucket').fill('other-bucket');
	await page.getByLabel('Time (UTC)').fill('03:15');
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect.poll(() => state.puts.length).toBe(1);
	const body = state.puts[0];
	expect(body).toMatchObject({ s3_bucket: 'other-bucket', schedule_time: '03:15', retention_value: 7 });
	for (const secret of ['s3_access_key', 's3_secret_key', 'passphrase', 'clear_s3_credentials', 'clear_passphrase']) {
		expect(body).not.toHaveProperty(secret);
	}

	// Invalid input is caught before a request.
	await page.getByLabel('Time (UTC)').fill('25:00');
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByText('Enter the time as HH:MM (24-hour, UTC).')).toBeVisible();
	expect(state.puts.length).toBe(1);

	// A concurrent backup is reported, not treated as a failure.
	await page.getByRole('button', { name: 'Back up now' }).click();
	await expect(page.getByText('A backup is already running.')).toBeVisible();
	expect(state.runRequests).toBe(1);
});

test('a non-sysadmin sees no Backups nav item and a forbidden notice', async ({ page }) => {
	await setup(page, false);
	await page.goto('/alpha/settings/version');
	await expect(page.getByRole('link', { name: 'Version' })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Backups' })).toHaveCount(0);

	await page.goto('/alpha/settings/backups');
	await expect(page.getByText('Only instance sysadmins can manage backups.')).toBeVisible();
	await expect(page.getByLabel('Access key')).toHaveCount(0);
});

test('testing a different endpoint asks for both keys instead of reusing the stored ones', async ({ page }) => {
	const state = await setup(page, true);
	await page.goto('/alpha/settings/backups');
	const testButton = page.getByRole('button', { name: 'Test connection' });
	const hint = page.getByText('The saved keys are only used with the saved endpoint.', { exact: false });
	await expect(testButton).toBeVisible();
	await expect(hint).toHaveCount(0);

	await page.getByLabel('Endpoint').fill('https://other.example.org');
	await expect(hint).toBeVisible();
	await expect(testButton).toHaveAttribute('aria-describedby', 'backup-endpoint-needs-keys');

	await page.getByRole('textbox', { name: 'Access key' }).fill('new-access');
	await page.getByRole('textbox', { name: 'Secret key' }).fill('new-secret');
	await expect(hint).toHaveCount(0);
	await testButton.click();
	await expect.poll(() => state.tests.length).toBe(1);
	expect(state.tests[0]).toMatchObject({ s3_endpoint: 'https://other.example.org', s3_access_key: 'new-access', s3_secret_key: 'new-secret' });
});
