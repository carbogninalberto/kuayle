import { test, expect, type Locator, type Page } from '@playwright/test';
import type { WorkflowGraph, WorkflowSaveInput } from '../../src/lib/api/workflows';

// The workflow editor's right side panel can be hidden so the canvas gets the
// width (KUA-147). The choice is a per-user preference, so it survives a
// reload, and hiding or showing the panel refits the chart without marking
// the workflow unsaved. Served from mocked API routes.
const time = '2026-11-10T00:00:00Z';

const profile = (id: string, name: string, behaviour: string) => ({
	id,
	workspace_id: 'workspace-1',
	name,
	role: name,
	behaviour,
	harness: 'claude-code',
	model: '',
	effort: '',
	instructions: '',
	skill_ids: [],
	created_at: time,
	updated_at: time
});
const profiles = [profile('p-impl', 'Implementer', 'changes_code')];
const graph = (): WorkflowGraph => ({
	nodes: [
		{ id: 'start', type: 'start', label: 'Start', position: { x: 0, y: 0 } },
		{ id: 'plan', type: 'agent', label: 'Implementer', profile_id: 'p-impl', prompt: 'Implement the plan.', position: { x: 280, y: 0 } },
		{ id: 'done', type: 'end', label: 'Done', outcome: 'done', position: { x: 560, y: 0 } }
	],
	edges: [
		{ id: 'e1', source: 'start', target: 'plan', condition: 'else' },
		{ id: 'e2', source: 'plan', target: 'done', condition: 'agent_finished' }
	],
	budget: {}
});

async function fixture(page: Page) {
	const state = {
		patches: [] as any[],
		saves: [] as WorkflowSaveInput[],
		saveResponse: undefined as Promise<void> | undefined,
		name: 'Optimized',
		graph: graph(),
		versions: [4, 3].map((version) => ({ id: `cheap-v${version}`, workflow_id: 'cheap', version, created_at: time })),
		preferences: {
			font_size: 'default',
			pointer_cursors: true,
			theme_mode: 'dark',
			light_theme: 'light',
			dark_theme: 'dark',
			workflow_sort_mode: 'default',
			workflow_sort_order: ['backlog', 'unstarted', 'started', 'completed', 'cancelled'],
			team_workflow_sort_overrides: {},
			recent_due_dates: [],
			issues_group_by: 'status',
			browser_notifications: true,
			workflow_editor_panel_open: true,
			notification_sound: 'chime',
			notification_events: {}
		}
	};
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.routeWebSocket('**/api/workspaces/test/ws', () => {});
	await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		const summary = () => ({
			id: 'cheap',
			name: state.name,
			description: '',
			builtin: false,
			customized: false,
			latest_version: state.versions[0].version,
			version_id: state.versions[0].id,
			archived: false,
			graph: state.graph,
			updated_at: time
		});
		if (path === '/api/auth/me')
			return route.fulfill({ json: { id: 'user-1', name: 'Owner', email: 'owner@example.test' } });
		if (path === '/api/preferences') {
			if (request.method() === 'PATCH') {
				const patch = request.postDataJSON();
				state.patches.push(patch);
				Object.assign(state.preferences, patch);
			}
			return route.fulfill({ json: state.preferences });
		}
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/agent-profiles')
			return route.fulfill({
				json: {
					profiles,
					can_manage: true,
					harnesses: ['claude-code'],
					model_harnesses: ['claude-code'],
					model_formats: {},
					behaviours: ['plans', 'changes_code', 'reviews']
				}
			});
		if (path === '/api/workspaces/test/workflows/validate')
			return route.fulfill({ json: { issues: [], warnings: [] } });
		if (path === '/api/workspaces/test/workflows')
			return route.fulfill({ json: { workflows: [summary()], defaults: [], can_manage: true } });
		if (path === '/api/workspaces/test/workflows/cheap') {
			if (request.method() === 'PUT') {
				const input: WorkflowSaveInput = request.postDataJSON();
				const version = state.versions[0].version + 1;
				state.saves.push(input);
				await state.saveResponse;
				state.name = input.name;
				state.graph = input.graph ?? state.graph;
				state.versions.unshift({ ...state.versions[0], id: `cheap-v${version}`, version });
				return route.fulfill({ json: summary() });
			}
			return route.fulfill({ json: { ...summary(), versions: state.versions } });
		}
		return route.fulfill({ json: [] });
	});
	return state;
}

type Box = { x: number; y: number; width: number; height: number };
const inside = (node: Box, canvas: Box) =>
	node.x >= canvas.x - 1 &&
	node.y >= canvas.y - 1 &&
	node.x + node.width <= canvas.x + canvas.width + 1 &&
	node.y + node.height <= canvas.y + canvas.height + 1;

async function expectStepsInsideCanvas(editor: Locator, canvas: Locator) {
	await expect(async () => {
		const canvasBox = (await canvas.boundingBox())!;
		for (const id of ['start', 'plan', 'done']) {
			const box = (await editor.getByTestId(`workflow-node-${id}`).boundingBox())!;
			expect(inside(box, canvasBox), `${id} is inside the canvas`).toBe(true);
		}
	}).toPass({ timeout: 5000 });
}

test('desktop: the side panel collapses to give the canvas the width, survives a reload and keeps the selection', async ({
	page
}) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/settings/workflows/cheap');
	const editor = page.getByTestId('workflow-editor');
	await expect(editor).toBeVisible();

	// Selecting a step shows its inspector in the open panel.
	const card = editor.getByTestId('workflow-node-plan');
	await card.click();
	const inspector = editor.getByTestId('workflow-node-inspector');
	await expect(inspector).toBeVisible();
	await expect(inspector.getByLabel('Name', { exact: true })).toHaveValue('Implementer');

	const canvas = editor.getByTestId('workflow-canvas-area');
	const openWidth = (await canvas.boundingBox())!.width;

	// The toggle is keyboard reachable and reports its expanded state.
	const toggle = editor.getByTestId('workflow-panel-toggle');
	await expect(toggle).toHaveAccessibleName('Hide side panel');
	await expect(toggle).toHaveAttribute('aria-expanded', 'true');
	await toggle.focus();
	await page.keyboard.press('Enter');

	// The panel is gone from the DOM, so its controls cannot take focus.
	await expect(editor.getByTestId('workflow-inspector-panel')).toHaveCount(0);
	await expect(toggle).toHaveAccessibleName('Show side panel');
	await expect(toggle).toHaveAttribute('aria-expanded', 'false');
	await expect.poll(async () => (await canvas.boundingBox())!.width).toBeGreaterThan(openWidth + 300);

	// The refit leaves every step inside the wider canvas. fitView animates for
	// 200 ms after the width change, so re-read both boxes until it settles.
	await expectStepsInsideCanvas(editor, canvas);

	// The preference is saved for this user, not only in this browser.
	await expect.poll(() => state.patches.at(-1)?.workflow_editor_panel_open).toBe(false);
	await expect(editor.getByTestId('workflow-save')).toBeDisabled();

	// A reload starts collapsed from the stored preference.
	await page.reload();
	await expect(editor).toBeVisible();
	await expect(editor.getByTestId('workflow-inspector-panel')).toHaveCount(0);
	await expect(editor.getByTestId('workflow-panel-toggle')).toHaveAttribute('aria-expanded', 'false');
	await expect(editor.getByTestId('workflow-save')).toBeDisabled();

	// Selecting a step while collapsed does not reopen the panel, but the
	// selection is kept and reopening shows its inspector.
	await editor.getByTestId('workflow-node-plan').click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toHaveCount(0);
	await editor.getByTestId('workflow-panel-toggle').click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toBeVisible();
	await expect(editor.getByTestId('workflow-node-inspector').getByLabel('Name', { exact: true })).toHaveValue(
		'Implementer'
	);
	await expect(editor.getByTestId('workflow-save')).toBeDisabled();

	// An explicit "Edit" from a node's context menu opens the panel again and
	// inspects that node, even while the panel is collapsed.
	await editor.getByTestId('workflow-panel-toggle').click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toHaveCount(0);
	// The second collapse also refits the chart; wait for the canvas to widen,
	// for the fit to finish and for the node to stop moving before aiming at it.
	await expect.poll(async () => (await canvas.boundingBox())!.width).toBeGreaterThan(openWidth + 300);
	await expectStepsInsideCanvas(editor, canvas);
	await expect(async () => {
		const first = (await editor.getByTestId('workflow-node-done').boundingBox())!;
		await page.waitForTimeout(250);
		const second = (await editor.getByTestId('workflow-node-done').boundingBox())!;
		expect(second).toEqual(first);
	}).toPass({ timeout: 5000 });
	await editor.getByTestId('workflow-node-done').click({ button: 'right' });
	const edit = page.getByRole('menuitem', { name: 'Edit', exact: true });
	await expect(edit).toBeVisible();
	await edit.click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toBeVisible();
	await expect(editor.getByTestId('workflow-node-inspector').getByLabel('Name', { exact: true })).toHaveValue(
		'Done'
	);
	await expect(editor.getByTestId('workflow-panel-toggle')).toHaveAttribute('aria-expanded', 'true');
	await expect(editor.getByTestId('workflow-save')).toBeDisabled();
	await expect.poll(() => state.patches.at(-1)?.workflow_editor_panel_open).toBe(true);
});

test('phone: the panel collapses and opens again without losing the canvas', async ({ page }) => {
	await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/workflows/cheap');
	const editor = page.getByTestId('workflow-editor');
	await expect(editor).toBeVisible();
	await expect(editor.getByTestId('workflow-inspector-panel')).toBeVisible();

	const toggle = editor.getByTestId('workflow-panel-toggle');
	await toggle.click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toHaveCount(0);
	await expect(editor.getByTestId('workflow-canvas-area')).toBeVisible();
	await expect(editor.getByTestId('workflow-node-plan')).toBeVisible();

	await toggle.click();
	await expect(editor.getByTestId('workflow-inspector-panel')).toBeVisible();
	await expect(editor.getByTestId('workflow-canvas-area')).toBeVisible();
	await expect(editor.getByTestId('workflow-save')).toBeDisabled();
});

test('workflow header shows saved and draft states, with history and an icon-only save', async ({ page }) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/settings/workflows/cheap');
	const header = page.getByTestId('workflow-editor').locator('header').first();
	const badge = header.getByRole('button', { name: /^v4 · / });
	const save = header.getByRole('button', { name: 'Save version' });
	const back = header.getByRole('link', { name: 'Agents workflows' });
	await expect(back).toHaveAttribute('href', '/test/settings/workflows');
	await expect(header.getByRole('heading', { name: 'Optimized' })).toBeVisible();
	await expect(header.getByText('All changes saved')).toHaveCount(0);
	await expect(header.getByText('Version 4', { exact: true })).toHaveCount(0);
	await expect(badge).toHaveText('v4 · saved');
	await expect(badge).toHaveCSS('color', 'rgb(74, 222, 128)');
	await expect(save).toBeDisabled();
	await expect(save).toHaveText('');
	await expect(save.locator('svg.lucide-save')).toBeVisible();
	const badgeBox = (await badge.boundingBox())!;
	const saveBox = (await save.boundingBox())!;
	await page.mouse.move(saveBox.x + saveBox.width / 2, saveBox.y + saveBox.height / 2);
	await expect(page.getByRole('tooltip')).toHaveText('Save version');
	expect(saveBox.x - (badgeBox.x + badgeBox.width)).toBeCloseTo(12, 0);
	expect(badgeBox.x).toBeGreaterThan(1100);

	await badge.press('Enter');
	const popover = page.getByRole('dialog', { name: 'Saved version 4' });
	await expect(popover).toContainText('There are no pending edits.');
	await popover.getByRole('button', { name: 'Version history' }).click();
	const history = page.getByRole('dialog', { name: 'Version history' });
	await expect(history.getByRole('listitem')).toHaveText([/Version 4/, /Version 3/]);
	await page.keyboard.press('Escape');
	await expect(history).toBeHidden();
	await expect(badge).toBeFocused();

	const editor = page.getByTestId('workflow-editor');
	await editor.getByTestId('workflow-node-plan').click();
	const step = editor.getByTestId('workflow-node-inspector').getByRole('textbox', { name: 'Name', exact: true });
	await step.fill('Implementation');
	await expect(badge).toHaveText('v4 · draft');
	await expect(badge).toHaveCSS('color', 'rgb(250, 204, 21)');
	await expect(save).toBeEnabled();
	await badge.click();
	await expect(popover).toContainText('Pending edits are uncommitted.');
	await page.keyboard.press('Escape');
	await save.focus();
	await expect(page.getByRole('tooltip')).toHaveText('Save version');
	await save.click();
	await expect.poll(() => state.saves[0]?.expected_version).toBe(4);
	const saved = header.getByRole('button', { name: 'v5 · saved' });
	await expect(saved).toBeVisible();
	await expect(save).toBeDisabled();
	await saved.click();
	await page.getByRole('button', { name: 'Version history' }).click();
	await expect(history.getByRole('listitem')).toHaveText([/Version 5/, /Version 4/, /Version 3/]);
	await page.keyboard.press('Escape');
	await back.click();
	await expect(page).toHaveURL('/test/settings/workflows');
});

test('workflow title commits with Enter and cancels with Escape without moving the header actions', async ({ page }) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/test/settings/workflows/cheap');
	const editor = page.getByTestId('workflow-editor');
	const rename = editor.getByRole('button', { name: 'Rename workflow: Optimized' });
	const badge = editor.getByRole('button', { name: 'v4 · saved' });
	const save = editor.getByRole('button', { name: 'Save version' });
	await expect(rename).toBeVisible();
	const badgeBox = await badge.boundingBox();
	await rename.click();
	const title = editor.getByRole('textbox', { name: 'Name', exact: true });
	await expect(title).toBeFocused();
	await title.fill('A long name that should not shift the badge while editing');
	expect(await badge.boundingBox()).toEqual(badgeBox);
	await expect(save).toBeDisabled();
	await title.press('Escape');
	await expect(rename).toBeFocused();

	await rename.press('Enter');
	await title.fill('Optimized process');
	await title.press('Enter');
	await expect(editor.getByRole('button', { name: 'Rename workflow: Optimized process' })).toBeFocused();
	await expect(editor.getByRole('button', { name: 'v4 · draft' })).toBeVisible();
	expect(state.saves).toHaveLength(0);
	await save.click();
	await expect.poll(() => state.saves[0]?.name).toBe('Optimized process');
	await expect(editor.getByRole('button', { name: 'v5 · saved' })).toBeVisible();
});

test('a rename committed during a save stays in the next draft', async ({ page }) => {
	const state = await fixture(page);
	const { promise, resolve } = Promise.withResolvers<void>();
	state.saveResponse = promise;
	await page.goto('/test/settings/workflows/cheap');
	const editor = page.getByTestId('workflow-editor');
	const title = editor.getByRole('textbox', { name: 'Name', exact: true });
	const save = editor.getByRole('button', { name: 'Save version' });

	await editor.getByRole('button', { name: 'Rename workflow: Optimized' }).click();
	await title.fill('First rename');
	await title.press('Enter');
	await save.click();
	await expect.poll(() => state.saves[0]?.name).toBe('First rename');
	await expect(save).toBeDisabled();

	await editor.getByRole('button', { name: 'Rename workflow: First rename' }).click();
	await title.fill('Second rename');
	await title.press('Enter');
	resolve();
	await expect(editor.getByRole('button', { name: 'v5 · draft' })).toBeVisible();
	await save.click();
	await expect.poll(() => state.saves[1]).toMatchObject({ name: 'Second rename', expected_version: 5 });
	await expect(editor.getByRole('button', { name: 'v6 · saved' })).toBeVisible();
});

test('a terminal stage can save, reload and remove its assistant', async ({ page }) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1440, height: 900 });
	await page.goto('/test/settings/workflows/cheap');
	const node = page.getByTestId('workflow-node-done');
	await node.click();
	const assistant = page.getByRole('combobox', { name: 'Stage assistant' });
	await assistant.click();
	await page.getByRole('option', { name: 'Implementer', exact: true }).click();
	await page.getByRole('button', { name: 'Save version' }).click();
	await expect.poll(() => state.graph.nodes.find((n) => n.id === 'done')?.profile_id).toBe('p-impl');
	await page.reload();
	await node.click();
	await expect(assistant).toHaveText(/Implementer/);
	await assistant.click();
	await page.getByRole('option', { name: 'None', exact: true }).click();
	await page.getByRole('button', { name: 'Save version' }).click();
	await expect.poll(() => state.graph.nodes.find((n) => n.id === 'done')?.profile_id).toBeUndefined();
});
