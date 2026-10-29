import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const ownerId = '00000000-0000-0000-0000-000000000001';
const machineId = '00000000-0000-0000-0000-000000000060';
const screenshotDir = process.env.KUAYLE_SCREENSHOT_DIR;

type Machine = Record<string, unknown>;

function baseMachine(overrides: Machine = {}): Machine {
	return {
		id: machineId,
		workspace_id: workspaceId,
		created_by_user_id: ownerId,
		routing_key: '0123456789abcdef0123',
		name: 'Resize test machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: '',
		repo_owner: '',
		repo_name: '',
		base_branch: '',
		working_branch: '',
		machine_size: 'small',
		profile: 'headless',
		cpu_millis: 2000,
		memory_mb: 4096,
		disk_gb: 20,
		pids_limit: 512,
		max_runtime_minutes: 120,
		keep_running: false,
		environment_builder: false,
		created_at: '2026-10-29T00:00:00Z',
		updated_at: '2026-10-29T00:00:00Z',
		expires_at: '2099-10-29T04:00:00Z',
		...overrides
	};
}

async function mockMachineApi(page: Page, initial: Machine, mode: 'hard' | 'monitored' = 'monitored') {
	const state = { machine: initial, patches: [] as Record<string, unknown>[] };
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') {
			return route.fulfill({
				json: { id: ownerId, email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null }
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
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'member' }
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
					max_runtime_minutes: 480,
					max_disk_gb: 100,
					idle_pause_minutes: 240,
					allowed_providers: ['opencode'],
					allowed_repositories: [],
					allow_custom_providers: false,
					disk_quota_mode: mode,
					size_options: {
						cpu_millis: [1000, 2000, 4000, 6000, 8000],
						memory_mb: [2048, 4096, 8192, 12288, 16384],
						disk_gb: [20, 30, 40, 50, 75, 100]
					}
				}
			});
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) {
			if (request.method() === 'PATCH') {
				const body = request.postDataJSON() as Record<string, number>;
				state.patches.push(body);
				state.machine = {
					...state.machine,
					pending_cpu_millis: body.cpu_millis,
					pending_memory_mb: body.memory_mb,
					pending_disk_gb: body.disk_gb
				};
			}
			return route.fulfill({ json: state.machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) {
			return route.fulfill({
				json: [
					{
						id: 1,
						cpu_percent: 80,
						memory_bytes: 1.3 * 1024 ** 3,
						disk_bytes: 5 * 1024 ** 3,
						pids: 40,
						network_rx_bytes: 0,
						network_tx_bytes: 0,
						created_at: '2026-10-29T00:00:00Z'
					}
				]
			});
		}
		if (
			['services', 'checkouts', 'tasks', 'events', 'logs', 'ports'].some(
				(part) => path === `/api/workspaces/test/dev-machines/${machineId}/${part}`
			)
		) {
			return route.fulfill({ json: [] });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

const oomMessage =
	'Out of memory: the agent container reached its 1.4 GiB memory limit. Resize the machine or raise the task machine memory.';

test('shows an out-of-memory kill on the machine and resizes it live', async ({ page }) => {
	const state = await mockMachineApi(
		page,
		baseMachine({
			resource_limit_kind: 'memory',
			resource_limit_message: oomMessage,
			resource_limit_at: '2026-10-29T01:00:00Z'
		})
	);
	await page.goto(`/test/machines/${machineId}`);
	const limit = page.getByTestId('machine-resource-limit');
	await expect(limit).toContainText('Out of memory');
	await expect(limit).toContainText(oomMessage);

	await limit.getByRole('button', { name: 'Resize' }).click();
	const dialog = page.getByRole('dialog', { name: 'Resize machine' });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByText('Running and paused machines get the new CPU and memory immediately')).toBeVisible();
	const apply = dialog.getByRole('button', { name: 'Apply size' });
	await expect(apply).toBeDisabled();
	const memory = dialog.getByRole('slider', { name: 'Memory' });
	await memory.focus();
	await page.keyboard.press('ArrowRight');
	await page.keyboard.press('ArrowRight');
	await expect(memory).toHaveAttribute('aria-valuetext', '12 GB');
	await apply.click();

	await expect(dialog).toBeHidden();
	expect(state.patches).toEqual([{ cpu_millis: 2000, memory_mb: 12288, disk_gb: 20 }]);
	await expect(page.getByTestId('machine-resize-pending')).toContainText('Resizing to 2 vCPU · 12 GB RAM · 20 GB disk');
	await expect(page.getByRole('button', { name: 'Resize' }).first()).toBeDisabled();
});

test('keeps the disk fixed under hard quotas and explains a rejected resize', async ({ page }) => {
	await mockMachineApi(
		page,
		baseMachine({
			status: 'stopped',
			desired_status: 'stopped',
			resize_error:
				'The host does not have room for the new size (host memory: 24576 MB requested of 18432 MB available to machines); choose a smaller size or stop another machine'
		}),
		'hard'
	);
	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByTestId('machine-resize-error')).toContainText('The last resize was not applied');
	await expect(page.getByTestId('machine-resize-error')).toContainText('host memory: 24576 MB requested');
	await page.getByRole('button', { name: 'Resize' }).click();
	const dialog = page.getByRole('dialog', { name: 'Resize machine' });
	await expect(dialog.getByText('This server enforces hard disk quotas')).toBeVisible();
	await expect(dialog.getByRole('slider', { name: 'Disk' })).toHaveAttribute('aria-valuetext', '20 GB');
	await dialog.getByRole('slider', { name: 'Disk' }).focus();
	await page.keyboard.press('ArrowRight');
	await expect(dialog.getByRole('slider', { name: 'Disk' })).toHaveAttribute('aria-valuetext', '20 GB');
});

test('offers resize only to the machine creator', async ({ page }) => {
	await mockMachineApi(page, baseMachine({ created_by_user_id: '00000000-0000-0000-0000-000000000099' }));
	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByText('Resize test machine', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Resize' })).toHaveCount(0);
});

test('fits the limit alert and resize dialog on a phone', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockMachineApi(
		page,
		baseMachine({
			resource_limit_kind: 'memory',
			resource_limit_message: oomMessage,
			resource_limit_at: '2026-10-29T01:00:00Z'
		})
	);
	await page.goto(`/test/machines/${machineId}`);
	await expect(page.getByTestId('machine-resource-limit')).toBeVisible();
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
	expect(overflow).toBeLessThanOrEqual(0);
	if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/r8-resize-limit-390.png`, fullPage: true });
	await page.getByTestId('machine-resource-limit').getByRole('button', { name: 'Resize' }).click();
	const dialog = page.getByRole('dialog', { name: 'Resize machine' });
	await expect(dialog).toBeVisible();
	const box = await dialog.boundingBox();
	expect(box).not.toBeNull();
	expect(box!.x).toBeGreaterThanOrEqual(0);
	expect(box!.x + box!.width).toBeLessThanOrEqual(390);
	if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/r8-resize-dialog-390.png` });
});
