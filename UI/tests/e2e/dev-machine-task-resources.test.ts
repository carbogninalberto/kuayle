import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';

async function mockPolicyApi(page: Page, taskMachine: Record<string, number>, role = 'owner') {
	const state = { saved: [] as Record<string, unknown>[] };
	const policy: Record<string, unknown> = {
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
		allow_unattended_automations: false,
		disk_quota_mode: 'hard',
		size_options: {
			cpu_millis: [1000, 2000, 4000, 6000, 8000, 12000, 16000],
			memory_mb: [2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536],
			disk_gb: Array.from({ length: 41 }, (_, index) => 20 + index * 2)
		}
	};
	Object.assign(policy, taskMachine);
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: {
					id: '00000000-0000-0000-0000-000000000001',
					email: 'owner@example.com',
					name: 'Owner',
					display_name: 'Owner',
					avatar_url: null
				}
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
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: role }
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-policy') {
			if (request.method() === 'PATCH') {
				const input = request.postDataJSON();
				state.saved.push(input);
				Object.assign(policy, input);
			}
			return route.fulfill({ json: policy });
		}
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [] } });
		if (path === '/api/workspaces/test/dev-machine-scope-setting') {
			if (request.method() === 'DELETE') return route.fulfill({ status: 204, body: '' });
			return route.fulfill({ json: { workspace_id: workspaceId } });
		}
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/agent-automations' || path === '/api/workspaces/test/agent-automation-runs')
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

const eightGiB = { task_machine_cpu_millis: 4000, task_machine_memory_mb: 8192, task_machine_disk_gb: 20 };

test('admins size the machines Kuayle creates for agent tasks', async ({ page }) => {
	const state = await mockPolicyApi(page, eightGiB);
	await page.goto('/test/settings/dev-machines?tab=policy');
	const section = page.getByRole('region', { name: 'Task machines' });
	await expect(section).toBeVisible();
	await expect(section.getByText('4 vCPU · 8 GB RAM · 20 GB disk')).toBeVisible();

	// The disk limit accepts any 2 GB step up to 2048 GB, beyond the saved 100 GB.
	await page.getByLabel('Maximum disk, GB').fill('500');
	await section.getByRole('slider', { name: 'Memory' }).focus();
	await page.keyboard.press('ArrowRight');
	const disk = section.getByRole('spinbutton', { name: 'Disk' });
	await disk.fill('251');
	await disk.press('Tab');
	await expect(disk).toHaveValue('252');
	await expect(section.getByText('4 vCPU · 12 GB RAM · 252 GB disk')).toBeVisible();
	await disk.press('ArrowDown');
	await disk.press('Tab');
	await expect(disk).toHaveValue('250', { timeout: 5000 });
	await disk.press('ArrowUp');
	await disk.press('Tab');
	await expect(disk).toHaveValue('252');

	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({
		max_disk_gb: 500,
		task_machine_cpu_millis: 4000,
		task_machine_memory_mb: 12288,
		task_machine_disk_gb: 252
	});
});

test('more vCPU raise task machine memory to 1 GB per vCPU', async ({ page }) => {
	const state = await mockPolicyApi(page, eightGiB);
	await page.goto('/test/settings/dev-machines?tab=policy');
	const section = page.getByRole('region', { name: 'Task machines' });
	await section.getByRole('slider', { name: 'vCPU' }).focus();
	await page.keyboard.press('End');
	await expect(section.getByText('16 vCPU · 16 GB RAM · 20 GB disk')).toBeVisible();
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({ task_machine_cpu_millis: 16000, task_machine_memory_mb: 16384 });
});

test('a server without task machine settings shows and saves the 8 GB default', async ({ page }) => {
	const state = await mockPolicyApi(page, {});
	await page.goto('/test/settings/dev-machines?tab=policy');
	const section = page.getByRole('region', { name: 'Task machines' });
	await expect(section.getByText('4 vCPU · 8 GB RAM · 20 GB disk')).toBeVisible();
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject(eightGiB);
});

test('members see task machine resources read-only', async ({ page }) => {
	await mockPolicyApi(page, eightGiB, 'member');
	await page.goto('/test/settings/dev-machines?tab=policy');
	const section = page.getByRole('region', { name: 'Task machines' });
	await expect(section.getByRole('spinbutton', { name: 'Disk' })).toBeDisabled();
	await expect(section.getByRole('slider', { name: 'Memory' })).toHaveAttribute('data-disabled', '');
});

test('the task machine section fits a phone screen', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockPolicyApi(page, eightGiB);
	await page.goto('/test/settings/dev-machines?tab=policy');
	const section = page.getByRole('region', { name: 'Task machines' });
	await section.scrollIntoViewIfNeeded();
	await expect(section.getByRole('spinbutton', { name: 'Disk' })).toBeVisible();
	const overflow = await page.evaluate(
		() => document.documentElement.scrollWidth - document.documentElement.clientWidth
	);
	expect(overflow).toBeLessThanOrEqual(0);
	if (process.env.KUAYLE_SCREENSHOT_DIR)
		await section.screenshot({ path: `${process.env.KUAYLE_SCREENSHOT_DIR}/task-machines-390.png` });
});
