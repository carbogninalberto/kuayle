import { expect, test } from '@playwright/test';

const teamId = '00000000-0000-0000-0000-000000000010';
const projectId = '00000000-0000-0000-0000-000000000020';
const now = '2026-01-01T00:00:00Z';

test('browser tab title shows the project name and team', async ({ page }) => {
	await page.routeWebSocket('**/ws', () => {});
	await page.route('**/api/**', async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user', name: 'Owner', email: 'owner@example.com' } });
		if (path === '/api/preferences') return route.fulfill({ json: { issues_group_by: 'status', theme_mode: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'workspace', slug: 'test', name: 'Test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: 'workspace', slug: 'test', name: 'Test', current_user_role: 'owner' } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({
				json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', created_at: now }]
			});
		if (path === `/api/workspaces/test/projects/${projectId}`)
			return route.fulfill({
				json: {
					id: projectId,
					name: 'Launch',
					team_id: teamId,
					status: 'planned',
					created_at: now,
					progress: { total: 0, completed: 0, cancelled: 0 }
				}
			});
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.endsWith('/github/status')) return route.fulfill({ json: { configured: false, repos: [] } });
		if (path.endsWith('/dev-machine-scope-setting')) return route.fulfill({ json: {} });
		return route.fulfill({ json: [] });
	});
	await page.goto(`/test/projects/${projectId}`);
	await expect(page).toHaveTitle('Kuayle - Engineering - Launch');
});
