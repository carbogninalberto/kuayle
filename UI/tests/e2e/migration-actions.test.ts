import { test, expect, type Page } from '@playwright/test';

const workspaceId = '00000000-0000-0000-0000-000000000002';
const machineId = '00000000-0000-0000-0000-000000000073';
const userId = '00000000-0000-0000-0000-000000000001';
const time = '2026-11-18T00:00:00Z';

type Action = {
	id: string;
	workspace_id: string;
	machine_id: string;
	name: string;
	description: string;
	command: string;
	created_by_user_id: string | null;
	created_at: string;
	updated_at: string;
};

// Mocks the migration actions contract of the machine API: list, create,
// update and delete. The stored connection secret is write-only: it arrives
// with create and never travels back with an action.
async function fixture(
	page: Page,
	options: { creator?: string; role?: string; actions?: Action[] } = {}
) {
	const state = {
		actions: (options.actions ?? []).map((action) => ({ ...action })),
		created: [] as Record<string, unknown>[],
		updated: [] as Record<string, unknown>[],
		removed: [] as string[],
		lists: 0,
		unhandled: [] as string[]
	};
	const machine = {
		id: machineId,
		workspace_id: workspaceId,
		routing_key: 'migrationsmachine0001',
		name: 'Migrations machine',
		status: 'running',
		desired_status: 'running',
		generation: 1,
		repo_url: '',
		repo_owner: '',
		repo_name: '',
		base_branch: '',
		working_branch: '',
		machine_size: 'medium',
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 50,
		pids_limit: 1024,
		created_by_user_id: options.creator ?? userId,
		created_at: time,
		updated_at: time
	};
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const method = request.method();
		const path = new URL(request.url()).pathname;
		const machinePath = `/api/workspaces/test/dev-machines/${machineId}`;
		if (path === '/api/auth/me')
			return route.fulfill({
				json: { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null }
			});
		if (path === '/api/preferences')
			return route.fulfill({
				json: {
					font_size: 'default',
					pointer_cursors: true,
					theme_mode: 'dark',
					dark_theme: 'dark',
					light_theme: 'light',
					workflow_sort_mode: 'default',
					workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
					team_workflow_sort_overrides: {},
					issues_group_by: 'status'
				}
			});
		if (path === '/api/workspaces')
			return route.fulfill({ json: [{ id: workspaceId, name: 'Test Workspace', slug: 'test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({
				json: { id: workspaceId, name: 'Test Workspace', slug: 'test', current_user_role: options.role ?? 'member' }
			});
		if (['teams', 'projects', 'labels', 'members', 'views'].some((part) => path === `/api/workspaces/test/${part}`))
			return route.fulfill({ json: [] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/dev-machine-policy')
			return route.fulfill({ json: { workspace_id: workspaceId, enabled: true, disk_quota_mode: 'hard' } });
		if (path === '/api/workspaces/test/agent-model-servers') return route.fulfill({ json: [] });
		if (path === machinePath) return route.fulfill({ json: machine });
		if (
			['checkouts', 'events', 'logs', 'tasks', 'resource-usage', 'services'].some((p) => path === `${machinePath}/${p}`)
		)
			return route.fulfill({ json: [] });
		if (path === `${machinePath}/ports`)
			return route.fulfill({ json: { ports: [], listening: [], scanned_at: time } });
		// The machine page's environment status card reads a cached, healthy check on load.
		if (path === `${machinePath}/environment-status` && method === 'GET')
			return route.fulfill({
				json: {
					package_index: {
						state: 'present',
						index_mtime: time,
						sudo_available: true,
						checked_at: time,
						refresh: null
					}
				}
			});
		if (path === `${machinePath}/migration-actions` && method === 'GET') {
			state.lists++;
			return route.fulfill({ json: { actions: state.actions } });
		}
		if (path === `${machinePath}/migration-actions` && method === 'POST') {
			const input = request.postDataJSON();
			state.created.push(input);
			const action: Action = {
				id: `ma-${state.actions.length + 1}`,
				workspace_id: workspaceId,
				machine_id: machineId,
				name: input.name,
				description: input.description ?? '',
				command: input.command,
				created_by_user_id: machine.created_by_user_id,
				created_at: time,
				updated_at: time
			};
			state.actions = [...state.actions, action];
			return route.fulfill({ status: 201, json: action });
		}
		const actionMatch = path.match(new RegExp(`^${machinePath}/migration-actions/([^/]+)$`));
		if (actionMatch) {
			const [, id] = actionMatch;
			const target = state.actions.find((action) => action.id === id);
			if (!target) return route.fulfill({ status: 404, json: { error: { message: 'not found' } } });
			if (method === 'PUT') {
				const input = request.postDataJSON();
				state.updated.push(input);
				Object.assign(target, {
					name: input.name,
					description: input.description ?? '',
					command: input.command,
					updated_at: time
				});
				return route.fulfill({ json: target });
			}
			if (method === 'DELETE') {
				state.removed.push(id);
				state.actions = state.actions.filter((action) => action.id !== id);
				return route.fulfill({ status: 204 });
			}
		}
		state.unhandled.push(`${method} ${path}`);
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return state;
}

const card = (page: Page) => page.getByRole('region', { name: 'Migrations' });

async function openApps(page: Page) {
	await page.goto(`/test/machines/${machineId}`);
	await page.getByRole('tab', { name: 'Apps & services' }).click();
}

test('configures a named migration, keeps its secret write-only, and removes it after confirmation', async ({ page }) => {
	const state = await fixture(page);
	await openApps(page);
	const migrations = card(page);
	await expect(migrations).toContainText('No migrations configured');

	await migrations.getByRole('button', { name: 'Add migration' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('heading', { name: 'Add migration' })).toBeVisible();
	// The form tells the admin where the command reads the connection secret from.
	await expect(dialog).toContainText('$KUAYLE_MIGRATION_SECRET');
	await dialog.getByLabel('Name').fill('migrate-db');
	await dialog.getByLabel('Command').fill('migrate apply --dsn "$KUAYLE_MIGRATION_SECRET"');
	await dialog.getByLabel('Connection secret').fill('postgres://migration:donotprint@db.internal:5432/app');
	// The API requires a description, so the form does too.
	await expect(dialog.getByRole('button', { name: 'Add migration' })).toBeDisabled();
	await dialog.getByLabel('Description').fill('Production schema');
	await dialog.getByRole('button', { name: 'Add migration' }).click();
	await expect(dialog).toHaveCount(0);

	// The card lists name, description and the exact command — never the secret.
	const listed = migrations.getByRole('listitem').filter({ hasText: 'migrate-db' });
	await expect(listed).toContainText('Production schema');
	await expect(listed).toContainText('migrate apply --dsn "$KUAYLE_MIGRATION_SECRET"');
	await expect(page.getByText('donotprint')).toHaveCount(0);
	expect(state.created).toEqual([
		{
			name: 'migrate-db',
			description: 'Production schema',
			command: 'migrate apply --dsn "$KUAYLE_MIGRATION_SECRET"',
			secret: 'postgres://migration:donotprint@db.internal:5432/app'
		}
	]);

	// Editing keeps the stored secret: the update sends no secret field.
	await listed.getByRole('button', { name: 'Edit migrate-db' }).click();
	await expect(dialog.getByRole('heading', { name: 'Edit migration migrate-db' })).toBeVisible();
	await expect(dialog.getByLabel('Connection secret')).toHaveAttribute('placeholder', 'Leave blank to keep the stored secret');
	await dialog.getByLabel('Command').fill('migrate apply --steps all --dsn "$KUAYLE_MIGRATION_SECRET"');
	await dialog.getByRole('button', { name: 'Save' }).click();
	await expect(dialog).toHaveCount(0);
	await expect(migrations.getByRole('listitem').filter({ hasText: 'migrate-db' })).toContainText('--steps all');
	expect(state.updated).toEqual([
		{
			name: 'migrate-db',
			description: 'Production schema',
			command: 'migrate apply --steps all --dsn "$KUAYLE_MIGRATION_SECRET"'
		}
	]);
	expect(state.updated[0]).not.toHaveProperty('secret');

	// Removal asks for confirmation first.
	await listed.getByRole('button', { name: 'Remove migrate-db' }).click();
	const confirm = page.getByRole('alertdialog');
	await expect(confirm.getByRole('heading', { name: 'Remove migrate-db?' })).toBeVisible();
	await confirm.getByRole('button', { name: 'Cancel' }).click();
	expect(state.removed).toEqual([]);
	await listed.getByRole('button', { name: 'Remove migrate-db' }).click();
	await page.getByRole('alertdialog').getByRole('button', { name: 'Remove' }).click();
	await expect(migrations).toContainText('No migrations configured');
	expect(state.removed).toEqual(['ma-1']);
	expect(state.unhandled).toEqual([]);
});

test('hides migrations from a member who did not create the machine', async ({ page }) => {
	// Reading a machine's migrations is for its creator and workspace owners and
	// admins, as configuring them is: the API refuses anyone else.
	const state = await fixture(page, { creator: '00000000-0000-0000-0000-000000000099', role: 'member' });
	await openApps(page);
	await expect(page.getByRole('heading', { name: 'Services' })).toBeVisible();
	await expect(card(page)).toHaveCount(0);
	expect(state.lists).toBe(0);
});

test('lets a workspace admin configure migrations on a machine they did not create', async ({ page }) => {
	await fixture(page, {
		creator: '00000000-0000-0000-0000-000000000099',
		role: 'admin',
		actions: [
			{
				id: 'ma-1',
				workspace_id: workspaceId,
				machine_id: machineId,
				name: 'migrate-db',
				description: '',
				command: 'migrate apply --dsn "$KUAYLE_MIGRATION_SECRET"',
				created_by_user_id: '00000000-0000-0000-0000-000000000099',
				created_at: time,
				updated_at: time
			}
		]
	});
	await openApps(page);
	const migrations = card(page);
	await expect(migrations.getByRole('button', { name: 'Add migration' })).toBeVisible();
	await expect(migrations.getByRole('button', { name: 'Edit migrate-db' })).toBeVisible();
	await expect(migrations.getByRole('button', { name: 'Remove migrate-db' })).toBeVisible();
});
