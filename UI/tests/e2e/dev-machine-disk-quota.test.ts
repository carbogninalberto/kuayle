import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const machineId = '00000000-0000-0000-0000-000000000050';
const diskGb = 20;

type DiskQuotaMode = 'hard' | 'monitored';

async function expectWarningColor(page: Page, locatorText: string) {
	const warningColor = await page.evaluate(() => {
		const probe = document.createElement('span');
		probe.style.color = 'var(--color-warning)';
		document.body.append(probe);
		const color = getComputedStyle(probe).color;
		probe.remove();
		return color;
	});
	await expect(page.getByText(locatorText, { exact: true })).toHaveCSS('color', warningColor);
}

async function mockDevMachineApi(page: Page, mode: DiskQuotaMode, usagePercent: number) {
	const machine = {
		id: machineId,
		workspace_id: workspaceId,
		routing_key: '0123456789abcdef0123',
		name: 'Quota test machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: '',
		repo_owner: '',
		repo_name: '',
		base_branch: '',
		working_branch: '',
		machine_size: 'small',
		cpu_millis: 2000,
		memory_mb: 4096,
		disk_gb: diskGb,
		pids_limit: 512,
		environment_builder: false,
		created_at: '2026-07-13T00:00:00Z',
		updated_at: '2026-07-13T00:00:00Z'
	};
	const diskBytes = Math.floor((diskGb * 1024 ** 3 * usagePercent) / 100);

	// Keep the live channel offline, as other suites do; an unstubbed upgrade reaches the preview proxy.
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'owner@example.com',
					name: 'Owner',
					display_name: 'Owner',
					avatar_url: null
				}
			});
		}
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		}
		if (path === '/api/workspaces') {
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		}
		if (path === '/api/workspaces/test') {
			return route.fulfill({
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' }
			});
		}
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/dev-machine-policy') {
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_disk_gb: 100,
					allowed_providers: ['opencode'],
					allowed_repositories: [],
					allow_custom_providers: false,
					disk_quota_mode: mode
				}
			});
		}
		if (path === '/api/workspaces/test/github/status') {
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [] } });
		}
		if (path === '/api/workspaces/test/dev-machine-scope-setting') {
			return route.fulfill({ json: { workspace_id: workspaceId } });
		}
		if (path === '/api/workspaces/test/dev-machine-environments') {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) {
			return route.fulfill({ json: machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/services`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/checkouts`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) {
			return route.fulfill({
				json: [
					{
						id: 1,
						cpu_percent: 12.5,
						memory_bytes: 1024 ** 3,
						disk_bytes: diskBytes,
						pids: 4,
						network_rx_bytes: 0,
						network_tx_bytes: 0,
						created_at: '2026-07-13T00:00:00Z'
					}
				]
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/tasks`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/events`) {
			return route.fulfill({ json: [] });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/logs`) {
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
}

for (const mode of ['hard', 'monitored'] as const) {
	test(`shows the disk-quota banner only in ${mode} mode`, async ({ page }) => {
		await mockDevMachineApi(page, mode, 50);
		await page.goto('/test/settings/dev-machines?tab=policy');
		await expect(page.getByRole('heading', { name: 'Dev Machines', exact: true })).toBeVisible();

		const banner = page.getByRole('alert').filter({ hasText: 'Disk limits are monitored' });
		if (mode === 'monitored') {
			await expect(banner).toBeVisible();
			await expect(banner).toContainText(
				'This host does not enforce hard disk quotas. Machines receive a warning at 90% of their limit and are stopped at 100%. All machines are stopped when host free space falls below the configured floor.'
			);
		} else {
			await expect(banner).toHaveCount(0);
		}
	});
}

for (const mode of ['hard', 'monitored'] as const) {
	test(`labels the workspace disk limit in ${mode} mode`, async ({ page }) => {
		await mockDevMachineApi(page, mode, 50);
		await page.goto(`/test/machines/${machineId}`);
		await expect(page.getByText('Quota test machine', { exact: true })).toBeVisible();
		await expect(
			page.getByText(mode === 'hard' ? 'Hard limit 20 GB' : 'Monitored limit 20 GB', { exact: true })
		).toBeVisible();
	});

	test(`shows the near-limit note at 92 percent in ${mode} mode`, async ({ page }) => {
		await mockDevMachineApi(page, mode, 92);
		await page.goto(`/test/machines/${machineId}`);
		await expect(page.getByText('Disk usage is near the limit.', { exact: true })).toBeVisible();
		await expectWarningColor(page, '18.4 GB');
	});

	test(`hides the near-limit note at 50 percent in ${mode} mode`, async ({ page }) => {
		await mockDevMachineApi(page, mode, 50);
		await page.goto(`/test/machines/${machineId}`);
		await expect(page.getByText('Quota test machine', { exact: true })).toBeVisible();
		await expect(page.getByText('Disk usage is near the limit.', { exact: true })).toHaveCount(0);
	});
}
