import { expect, test, type Locator, type Page } from '@playwright/test';

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

type Template = {
	id: string;
	workspace_id: string;
	team_id: null;
	title: string;
	description: string | null;
	status: string | null;
	priority: number | null;
	label_ids: string[];
	assignee_id: null;
	recurrence_rule: null;
	next_run_at: null;
	is_active: boolean;
	created_by: string;
	created_at: string;
	updated_at: string;
};

function template(overrides: Partial<Template> = {}): Template {
	return {
		id: 'template-existing',
		workspace_id: workspace.id,
		team_id: null,
		title: 'Existing bug report',
		description: '<p>Existing <strong>details</strong></p>',
		status: 'todo',
		priority: 2,
		label_ids: [],
		assignee_id: null,
		recurrence_rule: null,
		next_run_at: null,
		is_active: true,
		created_by: user.id,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z',
		...overrides
	};
}

async function mockTemplateApi(
	page: Page,
	{
		failCreates = 0,
		failUpdates = 0,
		createGate
	}: { failCreates?: number; failUpdates?: number; createGate?: Promise<void> } = {}
) {
	const templates = [template()];
	const creates: Record<string, unknown>[] = [];
	const updates: Array<{ id: string; data: Record<string, unknown> }> = [];
	let createAttempts = 0;
	let updateAttempts = 0;

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
		if (path === '/api/workspaces/test/issue-templates' && method === 'GET') {
			return route.fulfill({ json: templates });
		}
		if (path === '/api/workspaces/test/issue-templates' && method === 'POST') {
			const data = request.postDataJSON() as Record<string, unknown>;
			createAttempts++;
			await createGate;
			if (createAttempts <= failCreates) {
				return route.fulfill({ status: 500, json: { error: { message: 'Create failed' } } });
			}
			creates.push(data);
			const created = template({
				id: 'template-created',
				title: String(data.title),
				description: data.description ? String(data.description) : null,
				status: data.status ? String(data.status) : null,
				priority: typeof data.priority === 'number' ? data.priority : null
			});
			templates.unshift(created);
			return route.fulfill({ status: 201, json: created });
		}
		const match = path.match(/^\/api\/workspaces\/test\/issue-templates\/([^/]+)$/);
		if (match && method === 'PATCH') {
			const id = match[1];
			const data = request.postDataJSON() as Record<string, unknown>;
			updateAttempts++;
			if (updateAttempts <= failUpdates) {
				return route.fulfill({ status: 500, json: { error: { message: 'Update failed' } } });
			}
			updates.push({ id, data });
			const index = templates.findIndex((item) => item.id === id);
			templates[index] = { ...templates[index], ...data, updated_at: '2026-01-02T00:00:00Z' };
			return route.fulfill({ json: templates[index] });
		}
		if (
			path === '/api/workspaces/test/teams' ||
			path === '/api/workspaces/test/projects' ||
			path === '/api/workspaces/test/labels' ||
			path === '/api/workspaces/test/members' ||
			path === '/api/workspaces/test/views'
		) {
			return route.fulfill({ json: [] });
		}
		return route.fulfill({
			status: 404,
			json: { error: { code: 'UNHANDLED_TEST_ROUTE', message: `${method} ${path}` } }
		});
	});

	return {
		templates,
		creates,
		updates,
		get createAttempts() {
			return createAttempts;
		},
		get updateAttempts() {
			return updateAttempts;
		}
	};
}

async function selectEditorText(editor: Locator, matcher: string | RegExp = '') {
	await editor
		.locator('p', { hasText: matcher })
		.first()
		.evaluate((element) => {
			(element.closest('[contenteditable]') as HTMLElement).focus();
			const range = document.createRange();
			range.selectNodeContents(element);
			const selection = window.getSelection()!;
			selection.removeAllRanges();
			selection.addRange(range);
		});
}

test('creates and edits a rich-text issue template', async ({ page }) => {
	const calls = await mockTemplateApi(page);
	await page.goto('/test/settings/templates');

	await expect(page.getByRole('heading', { name: 'Templates', exact: true })).toBeVisible();
	await expect(page.getByText('Existing bug report', { exact: true })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Edit Existing bug report', exact: true })).toHaveCSS('opacity', '1');

	await page.getByRole('button', { name: 'New Template', exact: true }).first().click();
	const createDialog = page.getByRole('dialog', { name: 'Create Issue Template', exact: true });
	await createDialog.getByLabel('Title', { exact: true }).fill('Created template');
	const createEditor = createDialog.locator('.tiptap');
	await createEditor.fill('Rich description');
	await selectEditorText(createEditor);
	await page.getByTitle('Bold', { exact: true }).click();
	await createDialog.getByRole('button', { name: 'Create Issue Template', exact: true }).click();

	await expect(createDialog).toHaveCount(0);
	await expect(page.getByText('Created template', { exact: true })).toBeVisible();
	expect(calls.creates).toHaveLength(1);
	expect(calls.creates[0].title).toBe('Created template');
	expect(calls.creates[0].description).toContain('<strong>Rich description</strong>');

	await page.getByRole('button', { name: 'Edit Created template', exact: true }).click();
	const editDialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await expect(editDialog.getByLabel('Title', { exact: true })).toHaveValue('Created template');
	await expect(editDialog.locator('.tiptap')).toContainText('Rich description');
	await expect(editDialog.getByRole('combobox', { name: 'Default status', exact: true })).toContainText('Backlog');
	await expect(editDialog.getByRole('combobox', { name: 'Default priority', exact: true })).toContainText(
		'No priority'
	);

	await editDialog.getByLabel('Title', { exact: true }).fill('Updated template');
	await editDialog.locator('.tiptap').fill('Updated description');
	await editDialog.getByRole('button', { name: 'Save changes', exact: true }).click();

	await expect(editDialog).toHaveCount(0);
	await expect(page.getByText('Updated template', { exact: true })).toBeVisible();
	expect(calls.updates).toHaveLength(1);
	expect(calls.updates[0].id).toBe('template-created');
	expect(calls.updates[0].data.title).toBe('Updated template');
	expect(calls.updates[0].data.description).toContain('Updated description');
});

test('validates edits, discards cancelled values, and does not leak stale editor content', async ({ page }) => {
	const calls = await mockTemplateApi(page);
	await page.goto('/test/settings/templates');

	const editButton = page.getByRole('button', { name: 'Edit Existing bug report', exact: true });
	await editButton.click();
	let dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await dialog.getByLabel('Title', { exact: true }).fill('   ');
	await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
	await expect(page.getByText('Title is required', { exact: true })).toBeVisible();
	await expect(dialog).toBeVisible();
	expect(calls.updates).toHaveLength(0);

	await dialog.getByLabel('Title', { exact: true }).fill('Unsaved title');
	await dialog.locator('.tiptap').fill('Unsaved description');
	await page.keyboard.press('Escape');
	await expect(dialog).toHaveCount(0);
	await expect(page.getByText('Existing bug report', { exact: true })).toBeVisible();
	await expect(page.getByText('Unsaved title', { exact: true })).toHaveCount(0);

	await editButton.click();
	dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await expect(dialog.getByLabel('Title', { exact: true })).toHaveValue('Existing bug report');
	await expect(dialog.locator('.tiptap')).toContainText('Existing details');
	await expect(dialog.getByRole('combobox', { name: 'Default status', exact: true })).toContainText('Todo');
	await expect(dialog.getByRole('combobox', { name: 'Default priority', exact: true })).toContainText('High');
	await expect(dialog.locator('.tiptap')).not.toContainText('Unsaved description');

	await page.keyboard.press('Escape');
	await page.getByRole('button', { name: 'New Template', exact: true }).first().click();
	const createDialog = page.getByRole('dialog', { name: 'Create Issue Template', exact: true });
	await expect(createDialog.getByLabel('Title', { exact: true })).toHaveValue('');
	await expect(createDialog.locator('.tiptap')).not.toContainText('Existing details');
	await expect(createDialog.locator('.tiptap')).not.toContainText('Unsaved description');
});

test('clears a description and preserves form values after a failed update', async ({ page }) => {
	const calls = await mockTemplateApi(page, { failUpdates: 1 });
	await page.goto('/test/settings/templates');

	await page.getByRole('button', { name: 'Edit Existing bug report', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await dialog.getByLabel('Title', { exact: true }).fill('Retry title');
	await dialog.locator('.tiptap').fill('Retry description');
	await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();

	await expect(page.getByText('Update failed', { exact: true })).toBeVisible();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel('Title', { exact: true })).toHaveValue('Retry title');
	await expect(dialog.locator('.tiptap')).toContainText('Retry description');
	expect(calls.updateAttempts).toBe(1);

	await dialog.locator('.tiptap').fill('');
	await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
	await expect(dialog).toHaveCount(0);
	await expect(page.getByText('Retry title', { exact: true })).toBeVisible();
	expect(calls.updateAttempts).toBe(2);
	expect(calls.updates).toHaveLength(1);
	expect(calls.updates[0].data.title).toBe('Retry title');
	expect(String(calls.updates[0].data.description)).toBe('');
});

test('keeps a failed create available for retry and disables duplicate submission while pending', async ({ page }) => {
	let releaseCreate!: () => void;
	const createGate = new Promise<void>((resolve) => {
		releaseCreate = resolve;
	});
	const calls = await mockTemplateApi(page, { failCreates: 1, createGate });
	await page.goto('/test/settings/templates');

	await page.getByRole('button', { name: 'New Template', exact: true }).first().click();
	const dialog = page.getByRole('dialog', { name: 'Create Issue Template', exact: true });
	await dialog.getByLabel('Title', { exact: true }).fill('Retry create');
	await dialog.locator('.tiptap').fill('Keep this description');
	await dialog.getByRole('button', { name: 'Create Issue Template', exact: true }).click();

	await expect(dialog.getByRole('button', { name: 'Saving...', exact: true })).toBeDisabled();
	await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
	expect(calls.createAttempts).toBe(1);
	releaseCreate();

	await expect(page.getByText('Create failed', { exact: true })).toBeVisible();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel('Title', { exact: true })).toHaveValue('Retry create');
	await expect(dialog.locator('.tiptap')).toContainText('Keep this description');
});

test('keeps the edit action available in the narrow settings layout', async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockTemplateApi(page);
	await page.goto('/test/settings/templates');

	const editButton = page.getByRole('button', { name: 'Edit Existing bug report', exact: true });
	await expect(editButton).toHaveCSS('opacity', '1');
	await editButton.click();
	await expect(page.getByRole('dialog', { name: 'Edit Issue Template', exact: true })).toBeVisible();
});
