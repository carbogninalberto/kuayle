import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64');

async function mockWorkspaceLogo(page: Page, initialLogo: string | null = null) {
	const workspace: Record<string, unknown> = {
		privacy_enabled: false,
		id: workspaceId,
		name: 'Test Workspace',
		slug: 'test',
		logo_url: initialLogo,
		owner_id: '00000000-0000-0000-0000-000000000001',
		share_link_min_role: 'admin',
		current_user_role: 'owner',
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
	const state = { uploads: 0, patches: [] as Record<string, unknown>[], logoUrl: initialLogo };

	await page.routeWebSocket('**/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
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
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test' && request.method() === 'GET') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test' && request.method() === 'PATCH') {
			const body = request.postDataJSON();
			state.patches.push(body);
			state.logoUrl = body.logo_url ?? null;
			workspace.logo_url = state.logoUrl;
			return route.fulfill({ json: workspace });
		}
		if (path === '/api/workspaces/test/logo' && request.method() === 'POST') {
			state.uploads += 1;
			state.logoUrl = `/api/workspace-logos/${workspaceId}/${state.uploads}.png`;
			workspace.logo_url = state.logoUrl;
			return route.fulfill({ json: workspace });
		}
		if (path.startsWith('/api/workspace-logos/')) return route.fulfill({ body: png, contentType: 'image/png' });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (
			path === '/api/workspaces/test/teams' ||
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === '/api/workspaces/test/views' ||
			path === '/api/workspaces/test/github/status' ||
			path === '/api/workspaces/test/dev-machine-environments'
		) {
			return route.fulfill({ json: [] });
		}
		if (path === '/api/workspaces/test/issues') {
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return state;
}

test('shows the configured logo in the switcher and settings', async ({ page }) => {
	const logo = `/api/workspace-logos/${workspaceId}/configured.png`;
	await mockWorkspaceLogo(page, logo);
	await page.goto('/test/settings');

	await expect(page.getByText('Workspace logo')).toBeVisible();
	await expect(page.locator(`img[src="${logo}"]:visible`).first()).toBeVisible();
});

test('shows initials when no logo is configured', async ({ page }) => {
	await mockWorkspaceLogo(page);
	await page.goto('/test/settings');

	await expect(page.locator('img[src^="/api/workspace-logos/"]')).toHaveCount(0);
	await expect(page.locator('[data-slot="avatar-fallback"]').first()).toHaveText('T');
});

test('uploads, renders without reload, survives reload and replaces the logo', async ({ page }) => {
	const state = await mockWorkspaceLogo(page);
	await page.goto('/test/settings');
	await expect(page.getByRole('button', { name: 'Upload image' })).toBeVisible();
	await expect(page.locator('img[src^="/api/workspace-logos/"]')).toHaveCount(0);

	const input = page.locator('input[type="file"][accept*="image/png"]');
	await input.setInputFiles({ name: 'logo.png', mimeType: 'image/png', buffer: png });
	await expect.poll(() => state.uploads).toBe(1);
	const first = state.logoUrl!;
	await expect(page.locator(`img[src="${first}"]`).first()).toBeVisible();

	await page.reload();
	await expect(page.locator(`img[src="${first}"]`).first()).toBeVisible();

	await input.setInputFiles({ name: 'logo-2.png', mimeType: 'image/png', buffer: png });
	await expect.poll(() => state.uploads).toBe(2);
	const second = state.logoUrl!;
	expect(second).not.toBe(first);
	await expect(page.locator(`img[src="${second}"]`).first()).toBeVisible();
	await expect(page.locator(`img[src="${first}"]`)).toHaveCount(0);
});

test('renders the logo in the mobile header', async ({ page }) => {
	const state = await mockWorkspaceLogo(page);
	await page.goto('/test/settings');
	await page.locator('input[type="file"][accept*="image/png"]').setInputFiles({ name: 'logo.png', mimeType: 'image/png', buffer: png });
	await expect.poll(() => state.uploads).toBe(1);
	const logo = state.logoUrl!;

	await page.setViewportSize({ width: 500, height: 800 });
	await page.goto('/test/my-issues');
	await expect(page.locator(`img[src="${logo}"]:visible`).first()).toBeVisible();
});

test('rejects the wrong type and an oversized file before uploading', async ({ page }) => {
	const state = await mockWorkspaceLogo(page);
	await page.goto('/test/settings');
	await expect(page.getByRole('button', { name: 'Upload image' })).toBeVisible();
	const input = page.locator('input[type="file"][accept*="image/png"]');

	await input.setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') });
	await expect(page.getByText('Use a PNG, JPEG, WebP or GIF image.')).toBeVisible();
	expect(state.uploads).toBe(0);

	await input.setInputFiles({ name: 'huge.png', mimeType: 'image/png', buffer: Buffer.alloc(2 * 1024 * 1024 + 1) });
	await expect(page.getByText('The image must be 2 MB or smaller.')).toBeVisible();
	expect(state.uploads).toBe(0);
});

test('removes the logo', async ({ page }) => {
	const state = await mockWorkspaceLogo(page);
	await page.goto('/test/settings');
	await page.locator('input[type="file"][accept*="image/png"]').setInputFiles({ name: 'logo.png', mimeType: 'image/png', buffer: png });
	await expect.poll(() => state.uploads).toBe(1);
	await expect(page.locator('img[src^="/api/workspace-logos/"]').first()).toBeVisible();

	await page.getByRole('button', { name: 'Remove logo' }).click();
	await expect.poll(() => state.patches.length).toBe(1);
	expect(state.patches[0]).toEqual({ logo_url: null });
	await expect(page.locator('img[src^="/api/workspace-logos/"]')).toHaveCount(0);
});
