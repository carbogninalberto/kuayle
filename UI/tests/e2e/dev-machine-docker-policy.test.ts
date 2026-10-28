import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';

async function mockPolicyApi(page: Page, allowDocker: boolean | undefined) {
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
		disk_quota_mode: 'hard'
	};
	if (allowDocker !== undefined) policy.allow_docker = allowDocker;
	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null } });
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
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
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
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('admins opt machines into a Docker daemon in the policy', async ({ page }) => {
	const state = await mockPolicyApi(page, undefined);
	await page.goto('/test/settings/dev-machines?tab=policy');
	const docker = page.getByRole('switch', { name: 'Allow Docker on new machines' });
	await expect(docker).toBeVisible();
	await expect(docker).not.toBeChecked();
	await expect(page.getByText(/egress proxy/)).toBeVisible();
	await docker.click();
	await expect(docker).toBeChecked();
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({ allow_docker: true, allow_unattended_automations: false });
});

test('saving the policy keeps Docker on unless it is turned off', async ({ page }) => {
	const state = await mockPolicyApi(page, true);
	await page.goto('/test/settings/dev-machines?tab=policy');
	const docker = page.getByRole('switch', { name: 'Allow Docker on new machines' });
	await expect(docker).toBeChecked();
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({ allow_docker: true });
	await docker.click();
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(2);
	expect(state.saved[1]).toMatchObject({ allow_docker: false });
});
