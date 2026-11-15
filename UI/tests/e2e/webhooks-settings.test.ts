import { expect, test, type Page } from '@playwright/test';

const now = '2026-11-15T09:00:00Z';
const activeId = '11111111-1111-4111-8111-111111111111';
const inactiveId = '22222222-2222-4222-8222-222222222222';

function webhook(id: string, isActive: boolean, url: string) {
	return { id, url, events: ['issue.updated', 'comment.updated'], is_active: isActive, created_at: now, updated_at: now };
}

const deliveries = [
	{
		id: 'd2',
		event: 'issue.updated',
		status: 'pending',
		attempts: 2,
		last_status_code: 503,
		last_error: 'bad_status',
		next_attempt_at: '2026-11-15T09:12:00Z',
		created_at: '2026-11-15T09:01:00Z',
		updated_at: '2026-11-15T09:02:00Z',
		completed_at: null
	},
	{
		id: 'd1',
		event: 'comment.updated',
		status: 'delivered',
		attempts: 1,
		last_status_code: 204,
		last_error: null,
		next_attempt_at: null,
		created_at: '2026-11-15T09:00:00Z',
		updated_at: '2026-11-15T09:00:01Z',
		completed_at: '2026-11-15T09:00:01Z'
	}
];

async function mockApi(page: Page, privacyEnabled: boolean, calls: string[]) {
	const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner', privacy_enabled: privacyEnabled };
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
		if (path === '/api/workspaces/test/webhooks' && request.method() === 'GET')
			return route.fulfill({
				json: [webhook(activeId, true, 'https://hooks.example.com/active'), webhook(inactiveId, false, 'https://hooks.example.com/inactive')]
			});
		if (path === `/api/workspaces/test/webhooks/${activeId}/deliveries`) return route.fulfill({ json: deliveries });
		return route.fulfill({ json: [] });
	});
}

test('a private workspace explains that webhooks are off and offers no way to turn one on', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, true, calls);
	await page.goto('/test/settings/webhooks');

	await expect(page.getByText('Webhooks are off because this workspace has a private team')).toBeVisible();
	await expect(page.getByRole('button', { name: 'New Webhook' })).toBeDisabled();
	const switches = page.getByRole('switch');
	await expect(switches).toHaveCount(2);
	await expect(switches.nth(0)).toBeEnabled(); // the active one can still be turned off
	await expect(switches.nth(1)).toBeDisabled(); // the inactive one cannot be turned on
	expect(calls.filter((call) => call.startsWith('POST ') || call.startsWith('PATCH '))).toEqual([]);
});

test('a public workspace offers the comment events and shows a webhook delivery log', async ({ page }) => {
	const calls: string[] = [];
	await mockApi(page, false, calls);
	await page.goto('/test/settings/webhooks');

	await expect(page.getByText('https://hooks.example.com/active')).toBeVisible();
	await expect(page.getByText('Webhooks are off because this workspace has a private team')).toHaveCount(0);
	await expect(page.getByRole('switch').nth(1)).toBeEnabled();

	await page.getByRole('button', { name: 'New Webhook' }).click();
	const create = page.getByRole('dialog');
	for (const event of ['comment.created', 'comment.updated']) {
		await expect(create.getByRole('button', { name: event })).toBeVisible();
	}
	await expect(create.getByRole('button', { name: 'comment.deleted' })).toHaveCount(0);
	await create.getByRole('button', { name: 'Cancel' }).click();

	await page.getByRole('button', { name: 'Deliveries' }).first().click();
	const log = page.getByRole('dialog');
	await expect(log.getByText('https://hooks.example.com/active')).toBeVisible();
	await expect(log.getByText('Retrying')).toBeVisible();
	await expect(log.getByText('Last error: HTTP 503')).toBeVisible();
	await expect(log.getByText('Attempts: 2')).toBeVisible();
	await expect(log.getByText(/Next attempt/)).toBeVisible();
	await expect(log.getByText('Delivered')).toBeVisible();
	await expect(log.getByText('Attempts: 1')).toBeVisible();
	expect(calls).toContain(`GET /api/workspaces/test/webhooks/${activeId}/deliveries`);
});
