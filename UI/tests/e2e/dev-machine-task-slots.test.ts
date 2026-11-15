import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const ownerId = '00000000-0000-0000-0000-000000000001';
const machineId = '00000000-0000-0000-0000-000000000061';
const workspaceTaskLimit = 3;

type Machine = Record<string, unknown>;

function baseMachine(overrides: Machine = {}): Machine {
	return {
		id: machineId,
		workspace_id: workspaceId,
		created_by_user_id: ownerId,
		routing_key: '0123456789abcdef0124',
		name: 'Task slots machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		machine_size: 'small',
		profile: 'headless',
		cpu_millis: 2000,
		memory_mb: 4096,
		disk_gb: 20,
		pids_limit: 512,
		environment_builder: false,
		max_tasks: null,
		created_at: '2026-10-29T00:00:00Z',
		updated_at: '2026-10-29T00:00:00Z',
		...overrides
	};
}

async function mockMachineApi(page: Page, initial: Machine, role = 'owner') {
	const state = { machine: initial, patches: [] as Record<string, unknown>[] };
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: ownerId, email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null }
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
		if (path === '/api/workspaces/test/dev-machine-policy')
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
					disk_quota_mode: 'hard',
					task_machine_max_tasks: workspaceTaskLimit,
					size_options: {
						cpu_millis: [1000, 2000, 4000],
						memory_mb: [2048, 4096, 8192],
						disk_gb: [20, 30, 40]
					}
				}
			});
		if (path === `/api/workspaces/test/dev-machines/${machineId}`) {
			if (request.method() === 'PATCH') {
				const body = request.postDataJSON() as Record<string, unknown>;
				state.patches.push(body);
				state.machine = {
					...state.machine,
					max_tasks:
						body.clear_max_tasks === true ? null : typeof body.max_tasks === 'number' ? body.max_tasks : state.machine.max_tasks
				};
			}
			return route.fulfill({ json: state.machine });
		}
		if (path === `/api/workspaces/test/dev-machines/${machineId}/resource-usage`) return route.fulfill({ json: [] });
		if (
			['services', 'checkouts', 'tasks', 'events', 'logs', 'ports'].some(
				(part) => path === `/api/workspaces/test/dev-machines/${machineId}/${part}`
			)
		)
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

async function openTaskSlots(page: Page) {
	await page.goto(`/test/machines/${machineId}`);
	await page.getByRole('tab', { name: /Worktrees & tasks/ }).click();
	return page.getByTestId('machine-task-slots');
}

test('the machine creator changes its task limit while it runs', async ({ page }) => {
	const state = await mockMachineApi(page, baseMachine());
	const slots = await openTaskSlots(page);
	await expect(slots).toContainText(`Up to ${workspaceTaskLimit} agent tasks (workspace default)`);
	const slider = slots.getByRole('slider', { name: 'Task slots' });
	await expect(slider).toHaveAttribute('aria-valuemin', '1');
	await expect(slider).toHaveAttribute('aria-valuemax', '10');
	await expect(slider).toHaveAttribute('aria-valuenow', String(workspaceTaskLimit));

	await slider.focus();
	await page.keyboard.press('ArrowRight');
	await expect(slider).toHaveAttribute('aria-valuenow', '4');
	await slots.getByRole('button', { name: 'Save task limit' }).click();
	await expect.poll(() => state.patches.length).toBe(1);
	expect(state.patches[0]).toEqual({ max_tasks: 4 });
	await expect(slots).toContainText('Up to 4 agent tasks (set for this machine)');

	// The override can be cleared back to the workspace policy.
	await slots.getByRole('button', { name: 'Use workspace default' }).click();
	await expect.poll(() => state.patches.length).toBe(2);
	expect(state.patches[1]).toEqual({ clear_max_tasks: true });
	await expect(slots).toContainText(`Up to ${workspaceTaskLimit} agent tasks (workspace default)`);
});

test('only the machine creator sees the task limit edit control', async ({ page }) => {
	await mockMachineApi(page, baseMachine({ created_by_user_id: '00000000-0000-0000-0000-000000000099' }), 'member');
	const slots = await openTaskSlots(page);
	await expect(slots).toContainText(`Up to ${workspaceTaskLimit} agent tasks (workspace default)`);
	await expect(slots.getByRole('slider', { name: 'Task slots' })).toHaveCount(0);
	await expect(slots.getByRole('button', { name: 'Save task limit' })).toHaveCount(0);
});
