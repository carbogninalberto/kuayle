import { test, expect, type Page } from '@playwright/test';

const me = '00000000-0000-0000-0000-000000000001';
const someone = '00000000-0000-0000-0000-000000000003';
const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000020';
const createdAt = '2026-01-01T00:00:00Z';

type CommentRow = { id: string; user_id: string; body: string; edited_at?: string | null };

// Mocks a workspace where the caller has `role` and ENG-1 has one comment of
// theirs and one of someone else's. Records the comment writes it receives.
async function mockIssue(page: Page, role: 'owner' | 'member') {
	await page.routeWebSocket('**/ws', () => {});
	const pageErrors: string[] = [];
	page.on('pageerror', (error) => pageErrors.push(error.message));
	const users: Record<string, { id: string; email: string; name: string; display_name: string; avatar_url: null }> = {
		[me]: { id: me, email: 'me@example.com', name: 'Me Myself', display_name: 'Me Myself', avatar_url: null },
		[someone]: { id: someone, email: 'other@example.com', name: 'Other Person', display_name: 'Other Person', avatar_url: null }
	};
	const workspace = { id: '00000000-0000-0000-0000-000000000002', name: 'Test Workspace', slug: 'test', current_user_role: role, created_at: createdAt, updated_at: createdAt };
	const team = { id: teamId, name: 'Engineering', key: 'ENG', color: '#6366f1', icon: 'layers', created_at: createdAt, updated_at: createdAt };
	const issue = {
		id: '00000000-0000-0000-0000-000000000101', identifier: 'ENG-1', title: 'Comment target', description: null,
		status: 'todo', status_id: statusId, status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		priority: 3, team_id: teamId, project_id: null, cycle_id: null, creator_id: me, assignee_id: null, creator: users[me],
		assignee: null, assignees: [], parent_id: null, due_date: null, sort_order: 1000, labels: [], sub_issue_count: 0, sub_issue_done: 0,
		created_at: createdAt, updated_at: createdAt
	};
	const state = {
		live: [
			{ id: 'c-mine', user_id: me, body: '<p>My first thought</p>' },
			{ id: 'c-other', user_id: someone, body: '<p>Their remark</p>' }
		] as CommentRow[],
		deleted: [] as CommentRow[],
		writes: [] as string[],
		bodies: [] as string[]
	};
	const toResponse = (c: CommentRow) => ({ ...c, issue_id: issue.id, user: users[c.user_id], created_at: createdAt, updated_at: createdAt });
	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		const path = url.pathname;
		const method = request.method();
		if (path === '/api/auth/me') return route.fulfill({ json: users[me] });
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/members')
			return route.fulfill({ json: Object.values(users).map((u) => ({ user_id: u.id, name: u.name, role: u.id === me ? role : 'member' })) });
		if (path === '/api/workspaces/test/issues') return route.fulfill({ json: { data: [issue], total_count: 1, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/issues/ENG-1') return route.fulfill({ json: issue });
		if (path === '/api/workspaces/test/issues/ENG-1/comments' && method === 'GET')
			return route.fulfill({ json: state.live.map(toResponse) });
		const one = path.match(/^\/api\/workspaces\/test\/issues\/ENG-1\/comments\/([^/]+)(\/restore)?$/);
		if (one) {
			const [, id, restore] = one;
			state.writes.push(`${method}${restore ?? ''} ${id}`);
			if (method === 'PATCH') {
				const row = state.live.find((c) => c.id === id)!;
				row.body = request.postDataJSON().body;
				row.edited_at = '2026-01-02T00:00:00Z';
				state.bodies.push(row.body);
				return route.fulfill({ json: toResponse(row) });
			}
			if (method === 'DELETE') {
				state.deleted.push(...state.live.filter((c) => c.id === id));
				state.live = state.live.filter((c) => c.id !== id);
				return route.fulfill({ json: { status: 'deleted', restorable_until: '2026-02-01T00:00:00Z' } });
			}
			if (restore) {
				state.live.push(...state.deleted.filter((c) => c.id === id));
				state.deleted = state.deleted.filter((c) => c.id !== id);
				return route.fulfill({ json: toResponse(state.live.find((c) => c.id === id)!) });
			}
		}
		if (path === '/api/workspaces/test/issues/ENG-1/github') return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/issues/ENG-1/forges') return route.fulfill({ json: { merge_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/dev-machines') return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/preferences') return route.fulfill({ json: { theme_mode: 'dark', issues_group_by: 'status' } });
		if (path.match(/\/teams\/[^/]+\/statuses$/))
			return route.fulfill({ json: [{ id: statusId, team_id: teamId, name: 'Todo', slug: 'todo', category: 'unstarted', color: '#6366f1', position: 0, is_default: true, project_ids: [], created_at: createdAt, updated_at: createdAt }] });
		return route.fulfill({ json: [] });
	});
	return { state, pageErrors };
}

const commentCard = (page: Page, text: string) =>
	page.locator('.group\\/comment').filter({ hasText: text });

test('an owner edits their own comment and deletes another with Undo', async ({ page }, testInfo) => {
	const { state, pageErrors } = await mockIssue(page, 'owner');
	await page.goto('/test/issue/ENG-1');
	const mine = commentCard(page, 'My first thought');
	const theirs = commentCard(page, 'Their remark');
	await expect(mine).toBeVisible();

	// Their comment: Delete only, no Edit.
	await theirs.hover();
	await theirs.getByRole('button', { name: 'Comment actions' }).click();
	await expect(page.getByRole('menuitem', { name: 'Edit' })).toHaveCount(0);
	await page.getByRole('menuitem', { name: 'Delete' }).click();
	await expect.poll(() => state.writes).toEqual(['DELETE c-other']);
	await expect(theirs).toHaveCount(0);
	const toast = page.locator('[data-sonner-toast]').filter({ hasText: 'Comment deleted' });
	await expect(toast).toBeVisible();
	await toast.getByRole('button', { name: 'Undo' }).click();
	await expect.poll(() => state.writes).toEqual(['DELETE c-other', 'POST/restore c-other']);
	await expect(commentCard(page, 'Their remark')).toBeVisible();

	// Their own comment: Edit in place, then save.
	await mine.hover();
	await mine.getByRole('button', { name: 'Comment actions' }).click();
	await expect(page.getByRole('menuitem', { name: 'Delete' })).toBeVisible();
	await page.getByRole('menuitem', { name: 'Edit' }).click();
	const editor = mine.getByTestId('comment-editor');
	await expect(editor).toBeVisible();
	await editor.locator('[contenteditable="true"]').click();
	await page.keyboard.press('End');
	await page.keyboard.type(', revised');
	await page.screenshot({ path: testInfo.outputPath('comment-editing.png') });
	await editor.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => state.writes.at(-1)).toBe('PATCH c-mine');
	expect(state.bodies.at(-1)).toContain('My first thought, revised');
	await expect(commentCard(page, 'My first thought, revised').getByText('(edited)')).toBeVisible();
	await expect(mine.getByTestId('comment-editor')).toHaveCount(0);
	expect(pageErrors).toEqual([]);
});

test('a member may edit only their own comment and delete none', async ({ page }) => {
	const { state } = await mockIssue(page, 'member');
	await page.goto('/test/issue/ENG-1');
	const theirs = commentCard(page, 'Their remark');
	await expect(theirs).toBeVisible();
	await expect(theirs.getByRole('button', { name: 'Comment actions' })).toHaveCount(0);

	const mine = commentCard(page, 'My first thought');
	await mine.hover();
	await mine.getByRole('button', { name: 'Comment actions' }).click();
	await expect(page.getByRole('menuitem', { name: 'Edit' })).toBeVisible();
	await expect(page.getByRole('menuitem', { name: 'Delete' })).toHaveCount(0);
	await page.getByRole('menuitem', { name: 'Edit' }).click();
	await mine.getByTestId('comment-editor').getByRole('button', { name: 'Cancel' }).click();
	await expect(mine.getByTestId('comment-editor')).toHaveCount(0);
	expect(state.writes).toEqual([]);
});
