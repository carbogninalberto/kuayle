import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const teamId = '00000000-0000-0000-0000-000000000010';
const iconFile = '00000000-0000-0000-0000-0000000000aa.png';
const iconUrl = `/api/workspaces/test/teams/${teamId}/icon/${iconFile}`;
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64');

async function mockTeamSettings(page: Page) {
	const now = '2026-07-23T00:00:00Z';
	const team = { id: teamId, name: 'Team Alpha', key: 'ALPHA', description: null, color: '#3b82f6', icon: 'SquareUser', triage_enabled: false, parent_auto_close_enabled: false, sub_issue_auto_close_enabled: false, issue_copy_prompt: null, created_at: now, updated_at: now };
	const state = { uploads: 0, patches: [] as Record<string, unknown>[] };

	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (path === '/api/auth/me') return route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', email: 'owner@example.com', name: 'Owner', display_name: 'Owner', avatar_url: null } });
		if (path === '/api/preferences') return route.fulfill({ json: { font_size: 'default', pointer_cursors: true, theme_mode: 'dark', light_theme: 'light', dark_theme: 'dark', workflow_sort_mode: 'default', workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'], team_workflow_sort_overrides: {}, issues_group_by: 'status' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: 'owner' } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/issues') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === `/api/workspaces/test/teams/${teamId}/cycles`) return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/github/status') return route.fulfill({ json: { configured: false, installed: false, global_app: false, repos: [] } });
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-scope-setting') return route.fulfill({ status: 404, json: { error: { message: 'Not found' } } });
		if (path === `/api/workspaces/test/teams/${teamId}/icon` && request.method() === 'POST') {
			state.uploads += 1;
			team.icon = `image:${iconFile}`;
			return route.fulfill({ json: team });
		}
		if (path === iconUrl) return route.fulfill({ body: png, contentType: 'image/png' });
		if (path === `/api/workspaces/test/teams/${teamId}` && request.method() === 'PATCH') {
			state.patches.push(request.postDataJSON());
			team.icon = request.postDataJSON().icon;
			return route.fulfill({ json: team });
		}
		if (['labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`)) return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('shows an uploaded image as the team icon and restores the built-in icon on remove', async ({ page }) => {
	const state = await mockTeamSettings(page);
	await page.goto(`/test/settings/teams/${teamId}`);
	const uploadedIcon = page.locator(`img[src="${iconUrl}"]`);
	await expect(page.getByRole('button', { name: 'Upload image' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Remove image' })).toHaveCount(0);
	await expect(uploadedIcon).toHaveCount(0);

	await page.locator('input[type="file"]').setInputFiles({ name: 'favicon.png', mimeType: 'image/png', buffer: png });
	await expect(uploadedIcon.first()).toBeVisible();
	expect(state.uploads).toBe(1);

	await page.getByRole('button', { name: 'Remove image' }).click();
	await expect(page.getByRole('button', { name: 'Remove image' })).toHaveCount(0);
	await expect(uploadedIcon).toHaveCount(0);
	expect(state.patches).toEqual([{ icon: 'SquareUser' }]);
});

test('leaves the image type to the server and rejects only oversized files', async ({ page }) => {
	const state = await mockTeamSettings(page);
	await page.goto(`/test/settings/teams/${teamId}`);
	await expect(page.getByRole('button', { name: 'Upload image' })).toBeVisible();
	const input = page.locator('input[type="file"]');

	await input.setInputFiles({ name: 'huge.png', mimeType: 'image/png', buffer: Buffer.alloc(1024 * 1024 + 1) });
	await expect(page.getByText('Use a PNG, JPEG, WebP, GIF or ICO image up to 1 MB')).toBeVisible();
	expect(state.uploads).toBe(0);

	// Browsers often report no or a generic type for .ico files.
	await input.setInputFiles({ name: 'favicon.ico', mimeType: 'application/octet-stream', buffer: Buffer.from([0, 0, 1, 0]) });
	await expect(page.locator(`img[src="${iconUrl}"]`).first()).toBeVisible();
	expect(state.uploads).toBe(1);
});
