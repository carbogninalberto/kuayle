import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const teamId = '00000000-0000-0000-0000-000000000010';
const labelId = '00000000-0000-0000-0000-000000000020';

async function mockAutomationsApi(page: Page) {
	const state = {
		rules: [] as any[],
		created: [] as any[],
		patches: [] as any[],
		approvals: 0,
		runs: [
			{
				id: 'run-1',
				automation_id: 'rule-0',
				automation_name: 'Nightly fixer',
				issue_id: 'issue-1',
				issue_identifier: 'ENG-7',
				issue_title: 'Flaky login test',
				status: 'pending_approval',
				created_at: '2026-10-27T10:00:00Z'
			}
		] as any[]
	};
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
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({ json: [{ id: teamId, name: 'Engineering', key: 'ENG' }] });
		if (path === '/api/workspaces/test/labels')
			return route.fulfill({ json: [{ id: labelId, name: 'agent', color: '#000000' }] });
		if (path === `/api/workspaces/test/teams/${teamId}/statuses`) return route.fulfill({ json: [] });
		if (['projects', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/agent-automations' && request.method() === 'GET')
			return route.fulfill({ json: state.rules });
		if (path === '/api/workspaces/test/agent-automations' && request.method() === 'POST') {
			const input = request.postDataJSON();
			state.created.push(input);
			const rule = { id: 'rule-1', owner_id: 'u', enabled: true, created_at: '2026-10-27T10:00:00Z', ...input };
			state.rules.push(rule);
			return route.fulfill({ status: 201, json: rule });
		}
		if (path === '/api/workspaces/test/agent-automations/rule-1' && request.method() === 'PATCH') {
			const input = request.postDataJSON();
			state.patches.push(input);
			return route.fulfill({ json: { ...state.rules[0], ...input } });
		}
		if (path === '/api/workspaces/test/agent-automation-runs') return route.fulfill({ json: state.runs });
		if (path === '/api/workspaces/test/agent-automation-runs/run-1/approve') {
			state.approvals++;
			return route.fulfill({ json: { ...state.runs[0], status: 'started', task_id: 'task-1' } });
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
					allow_unattended_automations: false,
					disk_quota_mode: 'hard'
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
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('admins create, pause and approve agent automations', async ({ page }) => {
	const state = await mockAutomationsApi(page);
	await page.goto('/test/settings/dev-machines?tab=policy');
	// Automations have their own tab, not a section under the policy.
	await expect(page.getByRole('heading', { name: 'Workspace defaults' })).toBeVisible();
	await expect(page.getByRole('region', { name: 'Automations' })).toHaveCount(0);
	await page.getByRole('tab', { name: 'Automations' }).click();
	await expect(page).toHaveURL(/tab=automations/);
	const section = page.getByRole('region', { name: 'Automations' });
	await expect(section.getByText('No automations yet.')).toBeVisible();
	await section.getByRole('button', { name: 'New automation' }).click();
	const create = section.getByRole('button', { name: 'Create automation' });
	await section.getByLabel('Name').fill('Agent label');
	await section.getByRole('combobox', { name: 'Team' }).click();
	await page.getByRole('option', { name: 'Engineering' }).click();
	await section.getByLabel('Instructions for the agent').fill('Fix the issue and add tests');
	await expect(section.getByText('Choose a label, a status, or both.')).toBeVisible();
	await expect(create).toBeDisabled();
	await section.getByRole('combobox', { name: 'Label' }).click();
	await page.getByRole('option', { name: 'agent' }).click();
	await expect(section.getByRole('switch', { name: 'Start without approval' })).toBeDisabled();
	await expect(section.getByText('Allow unattended automations in the workspace policy')).toBeVisible();
	await create.click();
	await expect.poll(() => state.created.length).toBe(1);
	expect(state.created[0]).toEqual({
		team_id: teamId,
		name: 'Agent label',
		label_id: labelId,
		instructions: 'Fix the issue and add tests',
		unattended: false,
		max_active: 1
	});
	await expect(section.getByText('Engineering · label agent · Any time')).toBeVisible();

	await section.getByRole('switch', { name: 'Enable Agent label' }).click();
	await expect.poll(() => state.patches).toEqual([{ enabled: false }]);

	const pending = section.getByRole('region', { name: 'Waiting for approval' });
	await expect(pending.getByText('ENG-7 · Flaky login test')).toBeVisible();
	await pending.getByRole('button', { name: 'Approve' }).click();
	await expect.poll(() => state.approvals).toBe(1);
	await expect(section.getByText('ENG-7 · Nightly fixer · Started')).toBeVisible();
	await expect(pending).toHaveCount(0);
});
