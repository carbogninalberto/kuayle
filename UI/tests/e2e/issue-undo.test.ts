import { expect, test, type Page, type WebSocketRoute } from '@playwright/test';

// KUA-51: reversible changes offer Undo in their toast for about eight
// seconds, and Cmd/Ctrl+Z runs it while the toast is visible.

const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000030';
const cycleId = '00000000-0000-0000-0000-000000000040';
const cyclePath = `/test/teams/${teamId}/cycles/${cycleId}`;
const now = '2026-01-01T00:00:00Z';

type Mutation = { method: string; path: string; body: any };

async function mockWorkspace(page: Page, options: { undoStatus?: number } = {}) {
	const sockets: WebSocketRoute[] = [];
	await page.routeWebSocket('**/ws', (socket) => sockets.push(socket));
	const errors: string[] = [];
	page.on('pageerror', (error) => errors.push(error.message));
	const mutations: Mutation[] = [];
	let listRequests = 0;
	const issues = Array.from({ length: 3 }, (_, i) => ({
		id: `00000000-0000-0000-0000-${String(100 + i).padStart(12, '0')}`,
		identifier: `ENG-${i + 1}`,
		title: `Undo issue ${i + 1}`,
		team_id: teamId,
		creator_id: 'user',
		status_id: statusId,
		status: 'todo',
		priority: 3,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		cycle_id: cycleId,
		project_id: null,
		labels: [],
		assignees: [],
		sort_order: i * 1000,
		version: 4,
		created_at: now,
		updated_at: now
	}));
	const deleted = new Set<string>();
	await page.route('https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json', (route) =>
		route.fulfill({ json: [] })
	);
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		const method = request.method();
		if (['PATCH', 'POST', 'DELETE'].includes(method)) {
			mutations.push({ method, path, body: request.postData() ? request.postDataJSON() : null });
		}
		if (path === '/api/auth/me') return route.fulfill({ json: { id: 'user', name: 'Owner', email: 'owner@example.com' } });
		if (path === '/api/preferences') return route.fulfill({ json: { issues_group_by: 'status', theme_mode: 'dark' } });
		if (path === '/api/workspaces') return route.fulfill({ json: [{ id: 'workspace', slug: 'test', name: 'Test' }] });
		if (path === '/api/workspaces/test')
			return route.fulfill({ json: { id: 'workspace', slug: 'test', name: 'Test', current_user_role: 'owner' } });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/teams')
			return route.fulfill({ json: [{ id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', created_at: now }] });
		if (path.endsWith('/statuses'))
			return route.fulfill({
				json: [{ id: statusId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', position: 0, is_default: true, project_ids: [] }]
			});
		if (path === `/api/workspaces/test/teams/${teamId}/cycles`)
			return route.fulfill({
				json: [{ id: cycleId, team_id: teamId, number: 1, name: 'Current cycle', status: 'active', progress: { total: 3, completed: 0, cancelled: 0 }, created_at: now }]
			});
		if (path === `/api/workspaces/test/teams/${teamId}/cycles/${cycleId}`)
			return route.fulfill({
				json: { id: cycleId, team_id: teamId, number: 1, name: 'Current cycle', status: 'active', progress: { total: 3, completed: 0, cancelled: 0 }, created_at: now }
			});
		if (path === '/api/workspaces/test/issues') {
			listRequests++;
			const visible = issues.filter((issue) => !deleted.has(issue.id));
			return route.fulfill({ json: { data: visible, total_count: visible.length, page: 1, has_more: false } });
		}
		if (path === '/api/workspaces/test/issues/bulk' && method === 'PATCH') {
			const body = request.postDataJSON();
			const items = body.issue_ids.map((id: string) => {
				const issue = issues.find((i) => i.id === id)!;
				const previous = { priority: issue.priority };
				Object.assign(issue, { priority: body.priority, version: issue.version + 1 });
				return { id, expected_version: issue.version, previous };
			});
			return route.fulfill({ json: { updated: items.length, undo: { items } } });
		}
		if (path === '/api/workspaces/test/issues/bulk/undo') return route.fulfill({ json: { updated: request.postDataJSON().items.length } });
		if (path === '/api/workspaces/test/issues/bulk' && method === 'DELETE') {
			for (const id of request.postDataJSON().issue_ids) deleted.add(id);
			return route.fulfill({ json: { deleted: request.postDataJSON().issue_ids.length, restorable_until: now } });
		}
		if (path === '/api/workspaces/test/issues/bulk/restore') {
			for (const id of request.postDataJSON().issue_ids) deleted.delete(id);
			return route.fulfill({ json: { restored: request.postDataJSON().issue_ids.length } });
		}
		const match = path.match(/^\/api\/workspaces\/test\/issues\/(ENG-\d+)(\/undo|\/restore)?$/);
		const issue = match && issues.find((i) => i.identifier === match[1]);
		if (issue) {
			if (match[2] === '/undo') {
				if (options.undoStatus === 409)
					return route.fulfill({
						status: 409,
						json: { error: { code: 'EDITED_MEANWHILE', message: 'This item was edited in the meantime, so nothing was undone.' } }
					});
				Object.assign(issue, request.postDataJSON().previous, { version: issue.version + 1 });
				return route.fulfill({ json: issue });
			}
			if (match[2] === '/restore') {
				deleted.delete(issue.id);
				return route.fulfill({ json: issue });
			}
			if (method === 'DELETE') {
				deleted.add(issue.id);
				return route.fulfill({ json: { status: 'deleted', restorable_until: now } });
			}
			if (method === 'PATCH') {
				const body = request.postDataJSON();
				const undo = body.priority !== undefined ? { expected_version: issue.version + 1, previous: { priority: issue.priority } } : undefined;
				Object.assign(issue, body, { version: issue.version + 1 });
				return route.fulfill({ json: { ...issue, undo } });
			}
			return route.fulfill({ json: issue });
		}
		if (path.endsWith('/github/status')) return route.fulfill({ json: { configured: false, repos: [] } });
		if (path.endsWith('/dev-machine-scope-setting')) return route.fulfill({ json: {} });
		if (['labels', 'members', 'views', 'projects', 'issue-templates', 'templates'].some((p) => path.endsWith('/' + p)))
			return route.fulfill({ json: [] });
		return route.fulfill({ status: 404, json: { error: { message: `Unhandled ${path}` } } });
	});
	return { errors, mutations, sockets, listRequests: () => listRequests, issues };
}

async function setPriorityFromMenu(page: Page, title: string, priority: string) {
	await page.getByTestId('scoped-issues').getByText(title, { exact: true }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: 'Priority' }).click();
	await page.getByRole('menuitem', { name: priority, exact: true }).click();
}

const undoRequests = (mutations: Mutation[]) => mutations.filter((m) => m.path.endsWith('/undo'));

test('a priority change offers Undo, which sends the previous value back with the version', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	await setPriorityFromMenu(page, 'Undo issue 1', 'Urgent');

	const toast = page.locator('[data-sonner-toast]').filter({ hasText: 'Issue updated' });
	await expect(toast).toBeVisible();
	await toast.getByRole('button', { name: 'Undo' }).click();

	await expect.poll(() => undoRequests(state.mutations).length).toBe(1);
	expect(undoRequests(state.mutations)[0]).toMatchObject({
		path: '/api/workspaces/test/issues/ENG-1/undo',
		body: { expected_version: 5, previous: { priority: 3 } }
	});
	await expect(toast).toBeHidden();
	expect(state.errors).toEqual([]);
});

test('Ctrl+Z undoes while the toast is visible, but not in a text field or after the toast closed', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	await setPriorityFromMenu(page, 'Undo issue 1', 'High');
	const toast = page.locator('[data-sonner-toast]').filter({ hasText: 'Issue updated' });
	await expect(toast).toBeVisible();

	// A text field keeps its own undo.
	await page.evaluate(() => {
		const input = document.createElement('textarea');
		input.id = 'undo-probe';
		document.body.append(input);
	});
	await page.locator('#undo-probe').focus();
	await page.keyboard.press('Control+z');
	await page.locator('#undo-probe').evaluate((el) => el.remove());
	expect(undoRequests(state.mutations)).toHaveLength(0);

	await page.keyboard.press('Control+z');
	await expect.poll(() => undoRequests(state.mutations).length).toBe(1);
	await expect(toast).toBeHidden();
	// It runs once: a second press has nothing left to undo.
	await page.keyboard.press('Control+z');

	// A new change, whose toast is closed: Ctrl+Z no longer undoes it.
	await setPriorityFromMenu(page, 'Undo issue 2', 'Low');
	const second = page.locator('[data-sonner-toast]').filter({ hasText: 'Issue updated' });
	await expect(second).toBeVisible();
	await second.getByRole('button', { name: 'Close toast' }).click();
	await expect(second).toBeHidden();
	await page.keyboard.press('Control+z');
	await page.waitForTimeout(300);
	expect(undoRequests(state.mutations)).toHaveLength(1);
	expect(state.errors).toEqual([]);
});

test('a refused undo explains that the item was edited in the meantime', async ({ page }) => {
	const state = await mockWorkspace(page, { undoStatus: 409 });
	await page.goto(cyclePath);
	await setPriorityFromMenu(page, 'Undo issue 1', 'Urgent');
	await page.locator('[data-sonner-toast]').filter({ hasText: 'Issue updated' }).getByRole('button', { name: 'Undo' }).click();
	await expect(page.getByText('This item was edited in the meantime, so nothing was undone.')).toBeVisible();
	expect(undoRequests(state.mutations)).toHaveLength(1);
});

test('a bulk edit is undone as one step', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	await expect(page.getByTestId('scoped-issues').getByText('Undo issue 1', { exact: true })).toBeVisible();
	await page.keyboard.press('Control+a');
	await page.getByRole('button', { name: 'Actions', exact: true }).click();
	await page.getByRole('button', { name: /Set priority/ }).click();
	await page.getByRole('button', { name: 'Urgent', exact: true }).click();

	const toast = page.locator('[data-sonner-toast]').filter({ has: page.getByRole('button', { name: 'Undo' }) });
	await expect(toast).toBeVisible();
	await toast.getByRole('button', { name: 'Undo' }).click();
	await expect.poll(() => undoRequests(state.mutations).length).toBe(1);
	const request = undoRequests(state.mutations)[0];
	expect(request.path).toBe('/api/workspaces/test/issues/bulk/undo');
	expect(request.body.items).toHaveLength(3);
	expect(request.body.items[0]).toMatchObject({ expected_version: 5, previous: { priority: 3 } });
	expect(state.errors).toEqual([]);
});

test('deleting an issue offers Undo, which restores it', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	const list = page.getByTestId('scoped-issues');
	await list.getByText('Undo issue 2', { exact: true }).click({ button: 'right' });
	await page.getByRole('menuitem', { name: /Delete/ }).click();
	await expect(page.getByText('You can undo this right after deleting.')).toBeVisible();
	await page.getByRole('button', { name: 'Delete', exact: true }).click();

	const toast = page.locator('[data-sonner-toast]').filter({ hasText: 'Issue deleted' });
	await expect(toast).toBeVisible();
	await expect(list.getByText('Undo issue 2', { exact: true })).toBeHidden();
	await page.keyboard.press('Control+z');

	await expect.poll(() => state.mutations.filter((m) => m.path.endsWith('/restore')).length).toBe(1);
	expect(state.mutations.find((m) => m.path.endsWith('/restore'))?.path).toBe('/api/workspaces/test/issues/ENG-2/restore');
	await expect(list.getByText('Undo issue 2', { exact: true })).toBeVisible();
	expect(state.errors).toEqual([]);
});

test('another tab shows a restored issue when the realtime event arrives', async ({ page }) => {
	const state = await mockWorkspace(page);
	await page.goto(cyclePath);
	await expect(page.getByTestId('scoped-issues').getByText('Undo issue 1', { exact: true })).toBeVisible();
	await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
	const before = state.listRequests();
	state.sockets[0].send(JSON.stringify({ type: 'issue.restored', payload: { id: state.issues[0].id, identifier: 'ENG-1' } }));
	await expect.poll(() => state.listRequests()).toBeGreaterThan(before);
});
