import { expect, test, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
type Defaults = {
	repository: { id: string; full_name: string; base_branch: string; source: string; allowed: boolean } | null;
	inherited_repository: null;
	repositories: { id: string; full_name: string; default_branch: string }[];
};
const repositories = [
	{ id: 'repo-app', full_name: 'acme/app', default_branch: 'main' },
	{ id: 'repo-other', full_name: 'acme/other', default_branch: 'trunk' }
];

async function mockWorkspace(page: Page, defaults: Defaults) {
	const created: Record<string, unknown>[] = [];
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
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'GET')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.startsWith('/api/workspaces/test/dev-machine-names/'))
			return route.fulfill({ json: { name: 'repo-machine', available: true } });
		if (path === '/api/workspaces/test/dev-machine-providers')
			return route.fulfill({
				json: [{ id: 'codex', display_name: 'Codex', default_image: 'kuayle/dev-machine-headless:dev', required_secrets: [], optional_secrets: [], supported_modes: ['autonomous'], custom: false }]
			});
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({
				json: {
					workspace_id: workspaceId,
					enabled: true,
					max_concurrent_machines: 5,
					max_machines_per_user: 2,
					max_daily_agent_runs: 25,
					max_runtime_minutes: 480,
					max_disk_gb: 100,
					idle_pause_minutes: 240,
					allowed_providers: ['codex'],
					allowed_repositories: [],
					allow_custom_providers: false
				}
			});
		if (path === '/api/workspaces/test/dev-machine-environments') return route.fulfill({ json: [] });
		if (path === '/api/workspaces/test/dev-machine-repository-defaults') return route.fulfill({ json: defaults });
		if (path === '/api/workspaces/test/dev-machines' && request.method() === 'POST') {
			const payload = request.postDataJSON();
			created.push(payload);
			return route.fulfill({ status: 201, json: { id: '00000000-0000-0000-0000-000000000064', ...payload } });
		}
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${request.method()} ${path}` } } });
	});
	return created;
}

async function openCreateDialog(page: Page) {
	await page.goto('/test/machines');
	await page.getByRole('region', { name: 'Dev Machines' }).getByRole('button', { name: 'New machine' }).first().click();
	return page.getByRole('dialog');
}

test('preselects the configured repository and only sends an explicit override', async ({ page }) => {
	const created = await mockWorkspace(page, {
		repository: { id: 'repo-app', full_name: 'acme/app', base_branch: 'develop', source: 'workspace', allowed: true },
		inherited_repository: null,
		repositories
	});
	let dialog = await openCreateDialog(page);
	const repository = dialog.getByRole('combobox', { name: 'Repository' });
	await expect(repository).toHaveText('acme/app (from workspace settings)');
	await dialog.getByRole('button', { name: 'Create machine' }).click();
	await expect.poll(() => created.length).toBe(1);
	expect(created[0].repo, 'the server applies the configured repository and its base branch').toBeUndefined();

	dialog = await openCreateDialog(page);
	await dialog.getByRole('combobox', { name: 'Repository' }).click();
	await expect(page.getByRole('option', { name: 'acme/app', exact: true }), 'the default is offered once').toHaveCount(0);
	await page.getByRole('option', { name: 'acme/other', exact: true }).click();
	await dialog.getByRole('button', { name: 'Create machine' }).click();
	await expect.poll(() => created.length).toBe(2);
	expect(created[1].repo).toEqual({ provider: 'github', owner: 'acme', name: 'other', url: 'https://github.com/acme/other' });
});

test('a default the policy refuses blocks creation until another repository is chosen', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	const created = await mockWorkspace(page, {
		repository: { id: 'repo-secret', full_name: 'acme/secret', base_branch: 'main', source: 'team', allowed: false },
		inherited_repository: null,
		repositories
	});
	const dialog = await openCreateDialog(page);
	await expect(dialog.getByText("acme/secret is not allowed by the workspace's Dev Machine policy. Choose another repository.")).toBeVisible();
	const create = dialog.getByRole('button', { name: 'Create machine' });
	await expect(create).toBeDisabled();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	await dialog.getByRole('combobox', { name: 'Repository' }).click();
	await page.getByRole('option', { name: 'acme/app', exact: true }).click();
	await expect(create).toBeEnabled();
	await create.click();
	await expect.poll(() => created.length).toBe(1);
	expect(created[0].repo).toMatchObject({ owner: 'acme', name: 'app' });
});
