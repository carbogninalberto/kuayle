import { expect, test, type Page } from '@playwright/test';

const now = '2026-01-01T00:00:00Z';
const connectionId = '11111111-1111-4111-8111-111111111111';
const repoId = '22222222-2222-4222-8222-222222222222';

function connection(overrides: Record<string, unknown> = {}) {
	return {
		id: connectionId,
		provider: 'gitlab',
		name: 'GitLab',
		base_url: 'https://gitlab.example.com',
		has_access_token: true,
		has_webhook_secret: true,
		webhook_url: `https://kuayle.test/api/forges/webhook/${connectionId}`,
		created_at: now,
		updated_at: now,
		...overrides
	};
}

const repo = {
	id: repoId,
	connection_id: connectionId,
	external_repo_id: 42,
	full_name: 'team/app',
	web_url: 'https://gitlab.example.com/team/app',
	default_branch: 'main',
	is_active: true
};

async function mockApi(page: Page, role: 'owner' | 'member', calls: string[]) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: role };
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		calls.push(`${request.method()} ${path}`);
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: 'user-1', name: 'Owner', display_name: 'Owner', email: 'owner@example.test', avatar_url: null }
			});
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
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
					team_workflow_sort_overrides: {}
				}
			});
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/forges/connections' && request.method() === 'GET')
			return route.fulfill({ json: [connection()] });
		if (path === '/api/workspaces/test/forges/connections' && request.method() === 'POST') {
			calls.push(JSON.stringify(request.postDataJSON()));
			return route.fulfill({ status: 201, json: connection({ id: '33333333-3333-4333-8333-333333333333', name: 'Gitea', provider: 'gitea', base_url: 'https://gitea.example.com' }) });
		}
		if (path === `/api/workspaces/test/forges/connections/${connectionId}` && request.method() === 'DELETE')
			return route.fulfill({ json: { status: 'deleted' } });
		if (path === `/api/workspaces/test/forges/connections/${connectionId}/repos`)
			return route.fulfill({ json: [repo] });
		return route.fulfill({ json: [] });
	});
}

test('an owner sees the webhook URL, links repositories and can remove a connection', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, 'owner', calls);
	await page.goto('/test/settings/forges');

	await expect(page.getByRole('heading', { name: 'Forge integrations' })).toBeVisible();
	await expect(page.getByText(`https://kuayle.test/api/forges/webhook/${connectionId}`)).toBeVisible();
	await expect(page.getByText('team/app')).toBeVisible();
	await expect(page.getByText('#42')).toBeVisible();
	await expect(page.getByText('token set')).toBeVisible();

	await page.getByRole('button', { name: 'Add connection' }).click();
	await page.getByLabel('Base URL').fill('https://gitea.example.com');
	await page.getByLabel('Webhook secret').fill('s3cret');
	await page.getByRole('button', { name: 'Create connection' }).click();
	await expect(page.getByText('Connection created')).toBeVisible();
	await expect(page.getByText('https://gitea.example.com')).toBeVisible();
	expect(calls.find((call) => call.startsWith('{'))).toContain('"provider":"gitlab"');

	const gitlab = page.locator('section').filter({ hasText: 'gitlab.example.com' });
	page.once('dialog', (dialog) => dialog.accept());
	await gitlab.getByRole('button', { name: 'Delete' }).click();
	await expect(gitlab).toHaveCount(0);
	expect(calls).toContain(`DELETE /api/workspaces/test/forges/connections/${connectionId}`);
});

test('a member sees the admin-only note and cannot load or change forge connections', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, 'member', calls);
	await page.goto('/test/settings/forges');

	await expect(page.getByText('Only workspace owners can configure forge connections.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Add connection' })).toHaveCount(0);
	expect(calls.filter((call) => call.includes('/forges/'))).toEqual([]);
});
