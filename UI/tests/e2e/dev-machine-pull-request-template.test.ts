import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const defaultTemplate =
	'## Description\n{{description}}\n\n## Changes\n{{changes}}\n\n## Verification\n{{verification}}\n\n## Screenshots\n{{screenshots}}\n\n## Notes\n{{notes}}\n';

async function mockApi(page: Page, scope: Record<string, unknown>) {
	const state = { saved: [] as Record<string, unknown>[], deleted: 0 };
	const policy: Record<string, unknown> = {
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
	};
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
			if (request.method() === 'PATCH') Object.assign(policy, request.postDataJSON());
			return route.fulfill({ json: policy });
		}
		if (path === '/api/workspaces/test/github/status')
			return route.fulfill({ json: { configured: true, installed: true, global_app: false, repos: [] } });
		if (path === '/api/workspaces/test/dev-machine-scope-setting') {
			if (request.method() === 'DELETE') {
				state.deleted += 1;
				return route.fulfill({ status: 204, body: '' });
			}
			if (request.method() === 'PUT') {
				state.saved.push(request.postDataJSON());
				return route.fulfill({ json: { workspace_id: workspaceId, ...request.postDataJSON() } });
			}
			return route.fulfill({ json: { workspace_id: workspaceId, ...scope } });
		}
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('admins edit the pull request template, see invalid placeholders and reset it to the default', async ({ page }) => {
	const state = await mockApi(page, {});
	await page.goto('/test/settings/dev-machines?tab=policy');
	const field = page.getByRole('textbox', { name: 'Pull request description template' });
	const reset = page.getByRole('button', { name: 'Reset to default' });
	await expect(field).toHaveValue(defaultTemplate);
	await expect(reset).toBeDisabled();
	await expect(page.getByText(/\{\{description\}\}, \{\{changes\}\}, \{\{verification\}\}, \{\{screenshots\}\}, \{\{notes\}\}, \{\{issue\}\}/)).toBeVisible();

	await field.fill('## Summary\n{{summary}}');
	await expect(page.getByRole('alert').filter({ hasText: '{{summary}} is not a placeholder.' })).toBeVisible();
	await expect(field).toHaveAttribute('aria-invalid', 'true');

	await field.fill('  ## Why\n{{description}}\n\nFor {{issue}}\n');
	await expect(page.getByRole('alert').filter({ hasText: 'is not a placeholder' })).toHaveCount(0);
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({ scope_type: 'workspace', pull_request_template: '## Why\n{{description}}\n\nFor {{issue}}' });

	await expect(reset).toBeEnabled();
	await reset.click();
	await expect(field).toHaveValue(defaultTemplate);
	await page.getByRole('button', { name: 'Save policy' }).click();
	// The default is not stored: an otherwise empty workspace default is removed.
	await expect.poll(() => state.deleted).toBe(1);
	expect(state.saved).toHaveLength(1);
});

test('a stored template is shown and kept when other defaults are saved', async ({ page }) => {
	const state = await mockApi(page, { test_command: ['npm', 'test'], pull_request_template: '## What\n{{description}}' });
	await page.goto('/test/settings/dev-machines?tab=policy');
	await expect(page.getByRole('textbox', { name: 'Pull request description template' })).toHaveValue('## What\n{{description}}');
	await page.getByRole('button', { name: 'Save policy' }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0]).toMatchObject({ test_command: ['npm', 'test'], pull_request_template: '## What\n{{description}}' });
});
