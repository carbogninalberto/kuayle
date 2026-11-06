import { expect, test, type Page } from '@playwright/test';

const workspace = {
	id: 'workspace-1',
	slug: 'test',
	name: 'Test Workspace',
	current_user_role: 'owner'
};
const user = {
	id: 'user-1',
	name: 'Test User',
	display_name: 'Test User',
	email: 'test@example.com',
	avatar_url: null
};
const project = {
	id: 'project-1',
	workspace_id: workspace.id,
	name: 'Apollo',
	description: null,
	status: 'in_progress',
	team_id: null,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

function label(id: string, name: string, project_id: string | null) {
	return {
		id,
		name,
		color: '#ef4444',
		description: null,
		parent_id: null,
		project_id,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};
}

async function mockLabelApi(page: Page) {
	const labels = [label('label-1', 'Bug', null), label('label-2', 'Launch blocker', project.id)];
	const creates: Record<string, unknown>[] = [];

	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();

		if (path === '/api/auth/me') return route.fulfill({ json: user });
		if (path === '/api/preferences') {
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					light_theme: 'light',
					dark_theme: 'dark',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {}
				}
			});
		}
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') {
			return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		}
		if (path === '/api/workspaces/test/labels' && method === 'GET') return route.fulfill({ json: labels });
		if (path === '/api/workspaces/test/labels' && method === 'POST') {
			const data = request.postDataJSON() as Record<string, unknown>;
			creates.push(data);
			const created = label('label-created', String(data.name), (data.project_id as string) ?? null);
			labels.push(created);
			return route.fulfill({ status: 201, json: created });
		}
		if (path === '/api/workspaces/test/projects') return route.fulfill({ json: [project] });
		if (path === '/api/workspaces/test/teams' || path === '/api/workspaces/test/views') {
			return route.fulfill({ json: [] });
		}
		return route.fulfill({
			status: 404,
			json: { error: { code: 'UNHANDLED_TEST_ROUTE', message: `${method} ${path}` } }
		});
	});

	return { creates };
}

test('groups labels by project and creates a project label', async ({ page }) => {
	const calls = await mockLabelApi(page);
	await page.goto('/test/settings/labels');

	await expect(page.getByRole('heading', { name: 'Labels', exact: true })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'All projects', exact: true })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Apollo', exact: true })).toBeVisible();
	await expect(page.getByText('Bug', { exact: true })).toBeVisible();
	await expect(page.getByText('Launch blocker', { exact: true })).toBeVisible();

	await page.getByRole('button', { name: 'New Label', exact: true }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Create label', exact: true });
	await dialog.getByPlaceholder('e.g. Bug, Feature, Documentation').fill('Apollo only');
	await dialog.getByRole('combobox', { name: 'Scope', exact: true }).click();
	await page.getByRole('option', { name: 'Apollo', exact: true }).click();
	await dialog.getByRole('button', { name: 'Create label', exact: true }).click();

	await expect.poll(() => calls.creates).toHaveLength(1);
	expect(calls.creates[0]).toMatchObject({ name: 'Apollo only', project_id: project.id });
	await expect(dialog).toBeHidden();
	await expect(page.getByText('Apollo only', { exact: true })).toBeVisible();
});

test('creates a workspace-wide label by default', async ({ page }) => {
	const calls = await mockLabelApi(page);
	await page.goto('/test/settings/labels');

	await page.getByRole('button', { name: 'New Label', exact: true }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Create label', exact: true });
	await dialog.getByPlaceholder('e.g. Bug, Feature, Documentation').fill('Everywhere');
	await dialog.getByRole('button', { name: 'Create label', exact: true }).click();

	await expect.poll(() => calls.creates).toHaveLength(1);
	expect(calls.creates[0].project_id).toBeUndefined();
});
