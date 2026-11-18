import { test, expect, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const machineId = '00000000-0000-0000-0000-000000000071';
const userId = '00000000-0000-0000-0000-000000000001';
const time = '2026-11-15T00:00:00Z';

type Refresh = {
	state: 'queued' | 'running' | 'succeeded' | 'failed';
	exit_code: number | null;
	output: string | null;
	started_at: string | null;
	finished_at: string | null;
};
type PackageIndex = {
	state: 'absent' | 'present' | 'unknown';
	index_mtime: string | null;
	sudo_available: boolean;
	checked_at: string | null;
	refresh: Refresh | null;
};

// Mocks the machine API around the environment status contract: GET returns
// the cached check, POST queues a refresh that the next reads finish.
async function fixture(page: Page, index: Partial<PackageIndex> = {}, machineStatus = 'running') {
	const state = {
		index: {
			state: 'absent',
			index_mtime: null,
			sudo_available: true,
			checked_at: time,
			refresh: null,
			...index
		} as PackageIndex,
		// Reads of a queued refresh before it finishes.
		pendingReads: 1,
		refreshes: 0,
		unhandled: [] as string[]
	};
	const machine = {
		id: machineId,
		workspace_id: workspaceId,
		routing_key: 'envmachine00000000001',
		name: 'Env machine',
		status: machineStatus,
		desired_status: machineStatus,
		generation: 1,
		repo_url: '',
		repo_owner: '',
		repo_name: '',
		base_branch: '',
		working_branch: '',
		machine_size: 'medium',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 50,
		pids_limit: 1024,
		environment_builder: true,
		created_by_user_id: userId,
		created_at: time,
		updated_at: time
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const method = request.method();
		const path = new URL(request.url()).pathname;
		const machinePath = `/api/workspaces/test/dev-machines/${machineId}`;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null }
			});
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
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		if (path === '/api/workspaces')
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' }
			});
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({ json: { workspace_id: workspaceId, enabled: true, disk_quota_mode: 'hard' } });
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path === machinePath) return route.fulfill({ json: machine });
		if (
			['checkouts', 'events', 'logs', 'tasks', 'resource-usage', 'services'].some((p) => path === `${machinePath}/${p}`)
		)
			return route.fulfill({ json: [] });
		if (path === `${machinePath}/ports`) return route.fulfill({ json: { ports: [], listening: [], scanned_at: time } });
		// The machine page's migrations card lists the named migration actions on load.
		if (path === `${machinePath}/migration-actions`) return route.fulfill({ json: { actions: [] } });
		if (path === `${machinePath}/environment-status` && method === 'GET') {
			const refresh = state.index.refresh;
			if (refresh && (refresh.state === 'queued' || refresh.state === 'running')) {
				if (state.pendingReads > 0) {
					state.pendingReads--;
					refresh.state = 'running';
				} else {
					// The manager ran apt-get update and probed the index again.
					state.index = {
						...state.index,
						state: 'present',
						index_mtime: new Date().toISOString(),
						refresh: {
							...refresh,
							state: 'succeeded',
							exit_code: 0,
							output: 'Hit:1 http://deb.debian.org/debian bookworm InRelease\nReading package lists... Done',
							finished_at: new Date().toISOString()
						}
					};
				}
			}
			return route.fulfill({ json: { package_index: state.index } });
		}
		if (path === `${machinePath}/environment-status/package-index/refresh` && method === 'POST') {
			state.refreshes++;
			if (!state.index.sudo_available)
				return route.fulfill({
					status: 403,
					json: { error: { code: 'SUDO_DISABLED', message: 'Sudo is not enabled for this machine.' } }
				});
			state.index.refresh = { state: 'queued', exit_code: null, output: null, started_at: time, finished_at: null };
			return route.fulfill({ status: 202, json: { package_index: state.index } });
		}
		state.unhandled.push(`${method} ${path}`);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}

const card = (page: Page) => page.getByRole('region', { name: 'Environment status' });
const row = (page: Page) => card(page).getByTestId('env-check-package-index');

test('a missing package index is yellow and explained as expected, never a machine failure', async ({ page }) => {
	const state = await fixture(page, { sudo_available: false });
	await page.goto(`/test/machines/${machineId}`);
	const badge = row(page).getByTestId('env-package-index-badge');
	await expect(badge).toHaveText(/Not fetched/);
	await expect(badge).toHaveClass(/text-amber-400/);
	await expect(page.getByText(/^running$/i).first()).toBeVisible();
	// Without sudo there is no button; the popover carries the command instead.
	await expect(card(page).getByRole('button', { name: 'Refresh package index' })).toHaveCount(0);

	await row(page).getByRole('button', { name: 'About the package index' }).click();
	const popover = page.getByTestId('env-package-index-popover');
	await expect(popover).toContainText("Expected: the machine images delete apt's package index to keep them small");
	await expect(popover).toContainText('sudo apt-get update');
	await expect(popover.getByRole('button', { name: 'Copy' })).toBeVisible();
	await expect(popover).toContainText('Enable sudo in Settings → Dev Machines for new machines');
	expect(state.refreshes).toBe(0);
	expect(state.unhandled).toEqual([]);
});

test('refreshing the index shows the exit code and output and turns the row neutral', async ({ page }) => {
	const state = await fixture(page);
	await page.goto(`/test/machines/${machineId}`);
	await expect(row(page).getByTestId('env-package-index-badge')).toBeVisible();
	await row(page).getByRole('button', { name: 'About the package index' }).click();
	const popover = page.getByTestId('env-package-index-popover');
	await expect(popover).toContainText('Expected:');
	await expect(popover).not.toContainText('sudo apt-get update');
	await page.keyboard.press('Escape');

	await card(page).getByRole('button', { name: 'Refresh package index' }).click();
	await expect(card(page).getByRole('button', { name: 'Refreshing package index…' })).toBeDisabled();
	await expect(row(page).getByTestId('env-package-index-fetched')).toHaveText('Index fetched just now', {
		timeout: 15_000
	});
	await expect(row(page).getByTestId('env-package-index-badge')).toHaveCount(0);
	const result = card(page).getByTestId('env-refresh-result');
	await expect(result).toContainText('Exit code 0');
	await expect(result).toContainText('Reading package lists... Done');
	expect(state.refreshes).toBe(1);
	expect(state.unhandled).toEqual([]);
});

test('an unknown check reads as a dash', async ({ page }) => {
	await fixture(page, { state: 'unknown', checked_at: null });
	await page.goto(`/test/machines/${machineId}`);
	await expect(row(page).getByTestId('env-package-index-unknown')).toContainText('—');
	await expect(row(page).getByTestId('env-package-index-badge')).toHaveCount(0);
});

test('a stopped machine is never probed and reads unknown', async ({ page }) => {
	await fixture(page, {}, 'stopped');
	await page.goto(`/test/machines/${machineId}`);
	await expect(row(page).getByTestId('env-package-index-unknown')).toContainText('—');
	await expect(card(page).getByRole('button', { name: 'Refresh package index' })).toHaveCount(0);
});
