import { test, expect, type Page } from '@playwright/test';

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
const graph = () => ({
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
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const workspace = { id: 'workspace-1', slug: 'test', name: 'Test Workspace', current_user_role: 'owner' };
		const summary = () => ({
			id: 'cheap',
			name: 'Cheap Workflow',
			description: '',
			builtin: false,
			customized: false,
			latest_version: 4,
			version_id: 'cheap-v4',
			archived: false,
			graph: graph(),
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
		if (path === '/api/workspaces/test/workflows/cheap') return route.fulfill({ json: { ...summary(), versions: [] } });
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
	await expect(async () => {
		const canvasBox = (await canvas.boundingBox())!;
		for (const id of ['start', 'plan', 'done']) {
			const box = (await editor.getByTestId(`workflow-node-${id}`).boundingBox())!;
			expect(inside(box, canvasBox), `${id} is inside the canvas`).toBe(true);
		}
	}).toPass({ timeout: 5000 });

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
