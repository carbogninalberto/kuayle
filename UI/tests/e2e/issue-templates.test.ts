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
const team = {
	id: 'team-1',
	name: 'Engineering',
	key: 'ENG',
	description: null,
	color: null,
	icon: null,
	triage_enabled: false,
	parent_auto_close_enabled: false,
	sub_issue_auto_close_enabled: false,
	issue_copy_prompt: null,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};
const assignee = {
	user_id: 'user-2',
	name: 'Alice Chen',
	email: 'alice@example.com',
	role: 'member',
	created_at: '2026-01-01T00:00:00Z'
};
const label = {
	id: 'label-1',
	name: 'Bug',
	color: '#ef4444',
	description: null,
	parent_id: null,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

type Template = {
	id: string;
	workspace_id: string;
	team_id: string | null;
	title: string;
	default_issue_title: string | null;
	description: string | null;
	status: string | null;
	priority: number | null;
	label_ids: string[];
	assignee_id: string | null;
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
		default_issue_title: null,
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
		createGate,
		initialTemplate = template()
	}: {
		failCreates?: number;
		failUpdates?: number;
		createGate?: Promise<void>;
		initialTemplate?: Template;
	} = {}
) {
	const templates = [initialTemplate];
	const creates: Record<string, unknown>[] = [];
	const updates: Array<{ id: string; data: Record<string, unknown> }> = [];
	const uploads: Record<string, unknown>[] = [];
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
		if (path === '/api/workspaces/test/upload' && method === 'POST') {
			uploads.push({ body: await request.postDataBuffer() });
			return route.fulfill({
				json: {
					url: '/api/workspaces/test/assets/pasted.png',
					filename: 'pasted.png',
					content_type: 'image/png'
				}
			});
		}
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
				default_issue_title: data.default_issue_title ? String(data.default_issue_title) : null,
				description: data.description ? String(data.description) : null,
				status: data.status ? String(data.status) : null,
				priority: typeof data.priority === 'number' ? data.priority : null,
				team_id: data.team_id ? String(data.team_id) : null,
				assignee_id: data.assignee_id ? String(data.assignee_id) : null,
				label_ids: Array.isArray(data.label_ids) ? data.label_ids.map(String) : []
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
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/workspaces/test/labels') return route.fulfill({ json: [label] });
		if (path === '/api/workspaces/test/members') return route.fulfill({ json: [assignee] });
		if (path === '/api/workspaces/test/projects' || path === '/api/workspaces/test/views') {
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
		uploads,
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
	await createDialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Created template');
	await createDialog.getByRole('textbox', { name: 'Issue title', exact: true }).fill('[Bug] Created issue');
	const createEditor = createDialog.getByRole('textbox', { name: 'Description', exact: true });
	await createEditor.fill('Rich description');
	await selectEditorText(createEditor);
	await page.getByTitle('Bold', { exact: true }).click();

	await createDialog.getByRole('combobox', { name: 'Team', exact: true }).click();
	await page.getByRole('option', { name: 'Engineering', exact: true }).click();
	await createEditor.evaluate((element) => {
		const data = new DataTransfer();
		data.items.add(new File(['image'], 'pasted.png', { type: 'image/png' }));
		element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
	});
	await expect.poll(() => calls.uploads).toHaveLength(1);
	await expect(createEditor.getByRole('img', { name: 'pasted.png', exact: true })).toHaveAttribute(
		'src',
		'/api/workspaces/test/assets/pasted.png'
	);
	await createDialog.getByRole('combobox', { name: 'Default status', exact: true }).click();
	await page.getByRole('option', { name: 'Todo', exact: true }).click();
	await createDialog.getByRole('combobox', { name: 'Default priority', exact: true }).click();
	await page.getByRole('option', { name: 'High', exact: true }).click();
	await createDialog.getByRole('combobox', { name: 'Assignee', exact: true }).click();
	await page.getByRole('option', { name: 'Alice Chen', exact: true }).click();
	await createDialog.getByRole('button', { name: 'Labels', exact: true }).click();
	await page.getByRole('option', { name: 'Bug', exact: true }).click();
	await page.keyboard.press('Escape');
	await createDialog.getByRole('button', { name: 'Create Issue Template', exact: true }).click();

	await expect(createDialog).toHaveCount(0);
	await expect(page.getByText('Created template', { exact: true })).toBeVisible();
	expect(calls.creates).toHaveLength(1);
	expect(calls.creates[0].title).toBe('Created template');
	expect(calls.creates[0].default_issue_title).toBe('[Bug] Created issue');
	expect(calls.creates[0].description).toContain('<strong>Rich description</strong>');
	expect(calls.creates[0].team_id).toBe(team.id);
	expect(calls.creates[0].status).toBe('todo');
	expect(calls.creates[0].priority).toBe(2);
	expect(calls.creates[0].assignee_id).toBe(assignee.user_id);
	expect(calls.creates[0].label_ids).toEqual([label.id]);

	await page.getByRole('button', { name: 'Edit Created template', exact: true }).click();
	const editDialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await expect(editDialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Created template');
	await expect(editDialog.getByRole('textbox', { name: 'Issue title', exact: true })).toHaveValue(
		'[Bug] Created issue'
	);
	await expect(editDialog.getByRole('textbox', { name: 'Description', exact: true })).toContainText('Rich description');
	await expect(editDialog.getByRole('combobox', { name: 'Default status', exact: true })).toContainText('Todo');
	await expect(editDialog.getByRole('combobox', { name: 'Default priority', exact: true })).toContainText('High');

	await editDialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Updated template');
	await editDialog.getByRole('textbox', { name: 'Issue title', exact: true }).fill('[Bug] Updated issue');
	await editDialog.getByRole('textbox', { name: 'Description', exact: true }).fill('Updated description');
	await editDialog.getByRole('button', { name: 'Save changes', exact: true }).click();

	await expect(editDialog).toHaveCount(0);
	await expect(page.getByText('Updated template', { exact: true })).toBeVisible();
	await expect(page.getByText('ENG', { exact: true })).toBeVisible();
	expect(calls.updates).toHaveLength(1);
	expect(calls.updates[0].id).toBe('template-created');
	expect(calls.updates[0].data.title).toBe('Updated template');
	expect(calls.updates[0].data.default_issue_title).toBe('[Bug] Updated issue');
	expect(calls.updates[0].data.description).toContain('Updated description');
});

test('validates edits, discards cancelled values, and does not leak stale editor content', async ({ page }) => {
	const calls = await mockTemplateApi(page);
	await page.goto('/test/settings/templates');

	const editButton = page.getByRole('button', { name: 'Edit Existing bug report', exact: true });
	await editButton.click();
	let dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await dialog.getByRole('textbox', { name: 'Title', exact: true }).fill('   ');
	await expect(dialog.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
	await expect(dialog).toBeVisible();
	expect(calls.updates).toHaveLength(0);

	await dialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Unsaved title');
	await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('Unsaved description');
	await page.keyboard.press('Escape');
	await expect(dialog).toHaveCount(0);
	await expect(page.getByText('Existing bug report', { exact: true })).toBeVisible();
	await expect(page.getByText('Unsaved title', { exact: true })).toHaveCount(0);

	await editButton.click();
	dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await expect(dialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Existing bug report');
	await expect(dialog.getByRole('textbox', { name: 'Description', exact: true })).toContainText('Existing details');
	await expect(dialog.getByRole('combobox', { name: 'Default status', exact: true })).toContainText('Todo');
	await expect(dialog.getByRole('combobox', { name: 'Default priority', exact: true })).toContainText('High');
	await expect(dialog.getByRole('textbox', { name: 'Description', exact: true })).not.toContainText(
		'Unsaved description'
	);

	await page.keyboard.press('Escape');
	await page.getByRole('button', { name: 'New Template', exact: true }).first().click();
	const createDialog = page.getByRole('dialog', { name: 'Create Issue Template', exact: true });
	await expect(createDialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('');
	await expect(createDialog.getByRole('textbox', { name: 'Description', exact: true })).not.toContainText(
		'Existing details'
	);
	await expect(createDialog.getByRole('textbox', { name: 'Description', exact: true })).not.toContainText(
		'Unsaved description'
	);
});

test('clears a description and preserves form values after a failed update', async ({ page }) => {
	const calls = await mockTemplateApi(page, { failUpdates: 1 });
	await page.goto('/test/settings/templates');

	await page.getByRole('button', { name: 'Edit Existing bug report', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await dialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Retry title');
	await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('Retry description');
	await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();

	await expect(page.getByText('Update failed', { exact: true })).toBeVisible();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Retry title');
	await expect(dialog.getByRole('textbox', { name: 'Description', exact: true })).toContainText('Retry description');
	expect(calls.updateAttempts).toBe(1);

	await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('');
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
	await dialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Retry create');
	await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('Keep this description');
	await dialog.getByRole('button', { name: 'Create Issue Template', exact: true }).click();

	await expect(dialog.getByRole('button', { name: 'Saving...', exact: true })).toBeDisabled();
	await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
	await page.keyboard.press('Escape');
	await expect(dialog).toBeVisible();
	expect(calls.createAttempts).toBe(1);
	releaseCreate();

	await expect(page.getByText('Create failed', { exact: true })).toBeVisible();
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Retry create');
	await expect(dialog.getByRole('textbox', { name: 'Description', exact: true })).toContainText(
		'Keep this description'
	);
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

test('preserves unset status and priority when editing unrelated fields', async ({ page }) => {
	const calls = await mockTemplateApi(page, {
		initialTemplate: template({ status: null, priority: null })
	});
	await page.goto('/test/settings/templates');

	await page.getByRole('button', { name: 'Edit Existing bug report', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	await expect(
		dialog.getByRole('button', { name: 'Select a team to upload attachments', exact: true }).first()
	).toBeDisabled();
	await dialog.getByRole('textbox', { name: 'Title', exact: true }).fill('Renamed bug report');
	await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();

	await expect(page.getByText('Renamed bug report', { exact: true })).toBeVisible();
	expect(calls.updates).toHaveLength(1);
	expect(calls.updates[0].data).not.toHaveProperty('status');
	expect(calls.updates[0].data).not.toHaveProperty('priority');
});

test('supports a keyboard-only edit and returns focus to the edit action', async ({ page }) => {
	const calls = await mockTemplateApi(page);
	await page.goto('/test/settings/templates');

	const editButton = page.getByRole('button', { name: 'Edit Existing bug report', exact: true });
	await editButton.focus();
	await expect(editButton).toBeFocused();
	await page.keyboard.press('Enter');

	const dialog = page.getByRole('dialog', { name: 'Edit Issue Template', exact: true });
	const titleInput = dialog.getByRole('textbox', { name: 'Title', exact: true });
	await expect(titleInput).toBeFocused();
	await titleInput.fill('Keyboard edit');
	await titleInput.press('Control+Enter');

	await expect(dialog).toHaveCount(0);
	await expect(page.getByText('Keyboard edit', { exact: true })).toBeVisible();
	expect(calls.updates).toHaveLength(1);
	await expect(page.getByRole('button', { name: 'Edit Keyboard edit', exact: true })).toBeFocused();
});
