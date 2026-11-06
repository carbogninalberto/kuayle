import { expect, test, type Page } from '@playwright/test';

const machineId = '00000000-0000-0000-0000-000000000070';
const workspaceId = '00000000-0000-0000-0000-000000000002';
const GIB = 1024 ** 3;

const machine = {
	id: machineId,
	workspace_id: workspaceId,
	routing_key: '0123456789abcdef0170',
	name: 'Overview machine',
	status: 'running',
	desired_status: 'running',
	generation: 1,
	repo_url: 'https://github.com/kuayle/kuayle',
	repo_provider: 'github',
	repo_owner: 'kuayle',
	repo_name: 'kuayle',
	machine_size: 'custom',
	profile: 'headless',
	cpu_millis: 16000,
	memory_mb: 16384,
	disk_gb: 80,
	pids_limit: 1024,
	environment_builder: false,
	last_activity_at: '2026-11-06T19:55:00Z',
	created_at: '2026-11-01T08:00:00Z',
	updated_at: '2026-11-06T19:55:00Z'
};

const checkouts = Array.from({ length: 9 }, (_, index) => ({
	id: `00000000-0000-0000-0000-0000000001${String(index).padStart(2, '0')}`,
	workspace_id: workspaceId,
	machine_id: machineId,
	issue_id: `00000000-0000-0000-0000-0000000002${String(index).padStart(2, '0')}`,
	github_repo_id: '00000000-0000-0000-0000-000000000055',
	repository_full_name: 'kuayle/kuayle',
	base_branch: 'main',
	working_branch: `kuayle/eng-${index + 1}`,
	workspace_path: `/workspace/tasks/eng-${index + 1}`,
	status: 'ready',
	created_at: '2026-11-06T00:00:00Z',
	updated_at: '2026-11-06T00:00:00Z'
}));

// 60 one-minute buckets ending now: CPU rises to 12 cores, memory to 9 GiB.
function samples(count: number) {
	const end = Date.now();
	return Array.from({ length: count }, (_, index) => ({
		id: index + 1,
		cpu_percent: 200 + (index / count) * 1000,
		memory_bytes: Math.round((2 + (index / count) * 7) * GIB),
		disk_bytes: Math.round((10 + index / 10) * GIB),
		pids: 120,
		network_rx_bytes: 0,
		network_tx_bytes: 0,
		created_at: new Date(end - (count - index) * 60_000).toISOString()
	}));
}

async function mockMachine(page: Page, options: { samples: unknown[]; theme?: 'dark' | 'light'; windows?: string[] }) {
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		const base = `/api/workspaces/test/dev-machines/${machineId}`;
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null } });
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: options.theme ?? 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'admin' } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machine-policy') return route.fulfill({ json: { disk_quota_mode: 'monitored' } });
		if (path === base && request.method() === 'GET') return route.fulfill({ json: machine });
		if (path === `${base}/services`) return route.fulfill({ json: [] });
		if (path === `${base}/checkouts`) return route.fulfill({ json: checkouts });
		if (path === `${base}/resource-usage`) {
			options.windows?.push(url.searchParams.get('window') ?? '');
			return route.fulfill({ json: options.samples });
		}
		if (path === `${base}/tasks`) return route.fulfill({ json: [] });
		if (path === `${base}/events` || path === `${base}/logs`) return route.fulfill({ json: [] });
		if (path === `${base}/ports`) return route.fulfill({ json: { ports: [], listening: [], scanned_at: null } });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
}

test('shows the machine summary and charts its resource history for the chosen window', async ({ page }) => {
	const windows: string[] = [];
	await mockMachine(page, { samples: samples(60), windows });
	await page.goto(`/test/machines/${machineId}`);

	const summary = page.getByTestId('machine-summary');
	await expect(summary).toContainText('16 vCPU');
	await expect(summary).toContainText('kuayle/kuayle');
	await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true');

	const usage = page.getByTestId('machine-usage');
	// The newest sample is the current usage, against the machine's size.
	await expect(usage.getByTestId('machine-usage-cpu')).toContainText('11.8 vCPU');
	await expect(usage.getByTestId('machine-usage-cpu-limit')).toHaveText('Limit 16 vCPU');
	await expect(usage.getByTestId('machine-usage-memory')).toContainText('8.9 GB');
	await expect(usage.getByTestId('machine-usage-memory-limit')).toHaveText('Limit 16 GB');
	await expect(usage.getByTestId('machine-usage-disk-limit')).toHaveText('Monitored limit 80 GB');
	for (const key of ['cpu', 'memory', 'disk']) await expect(usage.getByTestId(`machine-usage-${key}`).locator('svg').first()).toBeVisible();
	await expect.poll(() => windows).toEqual(['1h']);

	await usage.getByRole('radio', { name: '24h' }).click();
	await expect.poll(() => windows).toEqual(['1h', '24h']);
	await expect(usage.getByRole('radio', { name: '24h' })).toHaveAttribute('aria-checked', 'true');
});

test('explains an empty history', async ({ page }) => {
	await mockMachine(page, { samples: [] });
	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByTestId('machine-usage-empty')).toHaveCount(3);
	await expect(page.getByTestId('machine-usage-empty').first()).toHaveText('No samples in this period yet.');
});

test('opens the worktrees tab from #tasks and lists every worktree in a dialog', async ({ page }) => {
	await mockMachine(page, { samples: [] });
	await page.goto(`/test/machines/${machineId}#tasks`);
	await expect(page.getByRole('tab', { name: /Worktrees & tasks/ })).toHaveAttribute('aria-selected', 'true');
	await expect(page.locator('#tasks')).toBeVisible();

	const panel = page.getByRole('tabpanel');
	await expect(panel.getByText('kuayle/eng-5 · ready')).toBeVisible();
	await expect(panel.getByText('kuayle/eng-6 · ready')).toHaveCount(0);
	await panel.getByRole('button', { name: 'View all (9)' }).click();
	const dialog = page.getByRole('dialog', { name: 'Issue worktrees' });
	await expect(dialog.getByText('kuayle/eng-9 · ready')).toBeAttached();
	await expect(dialog.getByRole('button', { name: 'Terminal' })).toHaveCount(9);
	await page.keyboard.press('Escape');
	await expect(dialog).toHaveCount(0);
	await expect(panel.getByRole('button', { name: 'View all (9)' })).toBeFocused();
});

for (const theme of ['dark', 'light'] as const) {
	for (const viewport of [
		{ name: 'desktop', width: 1440, height: 1000 },
		{ name: 'mobile', width: 390, height: 900 }
	]) {
		test(`renders the overview in ${theme} on ${viewport.name}`, async ({ page }) => {
			await page.setViewportSize({ width: viewport.width, height: viewport.height });
			await mockMachine(page, { samples: samples(60), theme });
			await page.goto(`/test/machines/${machineId}`);
			await expect(page.getByTestId('machine-usage-cpu').locator('svg').first()).toBeVisible();
			const directory = process.env.KUAYLE_SCREENSHOT_DIR;
			if (directory) await page.screenshot({ path: `${directory}/machine-overview-${theme}-${viewport.name}.png`, fullPage: true });
		});
	}
}
