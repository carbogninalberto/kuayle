import { test, expect, type Page } from '@playwright/test';

const me = '00000000-0000-0000-0000-000000000001';
const someone = '00000000-0000-0000-0000-000000000003';
const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000020';
const createdAt = '2026-01-01T00:00:00Z';

type CommentRow = { id: string; user_id: string; body: string; edited_at?: string | null; replies?: CommentRow[] };

// Mocks a workspace where the caller has `role` and ENG-1 has one comment of
// theirs and one of someone else's. Records the comment writes it receives.
async function mockIssue(page: Page, role: 'owner' | 'member', description: string | null = null, comment = '<p>My first thought</p>') {
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
		id: '00000000-0000-0000-0000-000000000101', identifier: 'ENG-1', title: 'Comment target', description,
		status: 'todo', status_id: statusId, status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		priority: 3, team_id: teamId, project_id: null, cycle_id: null, creator_id: me, assignee_id: null, creator: users[me],
		assignee: null, assignees: [], parent_id: null, due_date: null, sort_order: 1000, labels: [], sub_issue_count: 0, sub_issue_done: 0,
		created_at: createdAt, updated_at: createdAt
	};
	const state = {
		live: [
			{ id: 'c-mine', user_id: me, body: comment },
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
		if (path === '/api/workspaces/test/issues/ENG-1') {
			if (method === 'PATCH') issue.description = request.postDataJSON().description;
			return route.fulfill({ json: issue });
		}
		if (path === '/api/workspaces/test/issues/ENG-1/comments' && method === 'GET')
			return route.fulfill({ json: state.live.map((c) => ({ ...toResponse(c), replies: c.replies?.map(toResponse) })) });
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
	return { state, pageErrors, issue };
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

test('a column resized in the description editor keeps its width', async ({ page }, testInfo) => {
	// Stored markup exactly as the editor emits it for a resized table: the
	// width lives in the cell colwidth attributes and the colgroup.
	const description = '<table style="width: 340px"><colgroup><col style="width: 180px"><col style="min-width: 50px"></colgroup><tbody><tr><th colwidth="180">Step</th><th>Result</th></tr><tr><td>Build</td><td>Passed</td></tr></tbody></table>';
	const comment = '<table style="width: 340px"><colgroup><col style="width: 180px"><col style="min-width: 50px"></colgroup><tbody><tr><th colwidth="180">Check</th><th>Outcome</th></tr></tbody></table>';
	const { pageErrors, issue } = await mockIssue(page, 'owner', description, comment);
	await page.goto('/test/issue/ENG-1');

	// A stored table with fixed column widths renders in the reader unchanged.
	await expect(commentCard(page, 'Check').getByRole('table').locator('col').first())
		.toHaveAttribute('style', 'width: 180px');

	// The description is the editor: drag the first column's edge wider.
	const header = page.getByRole('columnheader', { name: 'Step' });
	const box = await header.boundingBox();
	expect(box).not.toBeNull();
	await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
	await expect(page.locator('.column-resize-handle').first()).toBeVisible();
	await page.mouse.down();
	await page.mouse.move(box!.x + box!.width + 60, box!.y + box!.height / 2, { steps: 8 });
	await page.mouse.up();

	// The resized width survives the sanitizer into the saved description.
	await expect.poll(() => {
		const match = issue.description?.match(/colwidth="(\d+)"/) ?? null;
		return match ? Number(match[1]) : 0;
	}, { timeout: 10_000 }).toBeGreaterThan(200);
	expect(issue.description).toContain('<colgroup>');
	expect(issue.description).toMatch(/<col style="width: \d{3}px">/);
	expect(issue.description).not.toMatch(/position|background|url\(/);

	await page.screenshot({ path: testInfo.outputPath('table-column-resized.png') });

	// Reopening the issue parses the stored widths back into the editor.
	await page.reload();
	await expect(page.locator('.ProseMirror table col').first()).toHaveAttribute('style', /width: 2\d\dpx/);
	expect(pageErrors).toEqual([]);
});

test('a description whose widths live only in col styles keeps them through the editor', async ({ page }) => {
	// Storage the API can hand the app, self-closing columns included: the
	// sanitizer keeps <col style> widths and Tiptap reads widths only from
	// cell colwidth / <col width>, which these cells and cols do not carry.
	const description = '<table><colgroup><col style="width: 180px"/><col style="width: 160px"/><col width="25%"/></colgroup>' +
		'<tbody><tr><th>Step</th><th>Result</th><th>Note</th></tr><tr><td>Build</td><td>Passed</td><td>On main</td></tr></tbody></table>';
	const { pageErrors, issue } = await mockIssue(page, 'owner', description);
	await page.goto('/test/issue/ENG-1');

	// The col styles imported as widths; the % width did not become 25px.
	const cols = page.locator('.ProseMirror table col');
	await expect(cols.nth(0)).toHaveAttribute('style', /width: 180px/);
	await expect(cols.nth(1)).toHaveAttribute('style', /width: 160px/);
	await expect(cols.nth(2)).toHaveAttribute('style', /min-width: 50px/);

	// Saving writes the imported widths where the editor reads them back.
	await page.getByRole('columnheader', { name: 'Step' }).click();
	await page.keyboard.type('!');
	await expect.poll(() => issue.description ?? '', { timeout: 10_000 }).toContain('colwidth="180"');
	expect(issue.description).toContain('colwidth="160"');
	expect(issue.description).not.toMatch(/colwidth="25"|width: 25px/);
	expect(pageErrors).toEqual([]);
});

test('a resized comment column keeps its rendered width after saving, reloading and editing', async ({ page }) => {
	const comment = '<table><colgroup><col style="width: 160px"/><col/></colgroup>' +
		'<tbody><tr><th colspan="2">Checklist</th></tr><tr><td>Build</td><td>Ready</td></tr></tbody></table>';
	const { state } = await mockIssue(page, 'owner', null, comment);
	await page.goto('/test/issue/ENG-1');
	const card = commentCard(page, 'Checklist');
	const editor = card.getByTestId('comment-editor');
	const cell = card.getByRole('cell', { name: 'Build', exact: true });
	async function editComment() {
		await card.hover();
		await card.getByRole('button', { name: 'Comment actions' }).click();
		await page.getByRole('menuitem', { name: 'Edit' }).click();
		await expect(editor).toBeVisible();
	}
	await editComment();
	await expect(editor.locator('col').first()).toHaveCSS('width', '160px');
	await cell.scrollIntoViewIfNeeded();
	const box = (await cell.boundingBox())!;
	const edge = box.x + box.width - 2;
	const y = box.y + box.height / 2;
	await cell.hover({ position: { x: box.width - 2, y: box.height / 2 } });
	await expect(editor.locator('.column-resize-handle').first()).toBeVisible();
	await page.mouse.down();
	await page.mouse.move(edge + 60, y, { steps: 8 });
	await page.mouse.up();
	await editor.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => state.bodies.at(-1)).toMatch(/colwidth="\d+,0"/);
	const width = Number(state.bodies.at(-1)!.match(/colwidth="(\d+),0"/)![1]);
	expect(Math.abs(width - 220)).toBeLessThanOrEqual(2);
	await page.reload();
	await expect(cell).toHaveCSS('width', `${width}px`);

	await editComment();
	await cell.getByText('Build', { exact: true }).dblclick({ position: { x: 4, y: 8 } });
	await page.keyboard.insertText('Build checked');
	await editor.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => state.bodies.at(-1)).toContain('Build checked');
	expect(state.bodies.at(-1)).toContain(`colwidth="${width},0"`);
	await expect(card.getByRole('cell', { name: 'Build checked' })).toHaveCSS('width', `${width}px`);
});

test('a table with merged first-row cells keeps its resized widths', async ({ page }, testInfo) => {
	// The editor's markup for a resized merged cell: Tiptap writes a 0
	// colwidth entry for the spanned column, and derives the column layout
	// only from that first row, so the 0 entry must survive save and reload.
	const description = '<table style="width: 340px"><colgroup><col style="width: 180px"><col style="width: 160px"></colgroup>' +
		'<tbody><tr><th colspan="2" colwidth="180,0">Release</th></tr><tr><td>Build</td><td>Passed</td></tr></tbody></table>';
	const { pageErrors, issue } = await mockIssue(page, 'owner', description);
	await page.goto('/test/issue/ENG-1');

	// The stored sentinel width loads: the first column is 180px in the editor.
	await expect(page.locator('.ProseMirror table col').nth(0)).toHaveAttribute('style', /width: 180px/);

	// Drag the boundary after the first column wider.
	const cell = page.getByRole('cell', { name: 'Build' });
	const box = await cell.boundingBox();
	expect(box).not.toBeNull();
	await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
	await expect(page.locator('.column-resize-handle').first()).toBeVisible();
	await page.mouse.down();
	await page.mouse.move(box!.x + box!.width + 60, box!.y + box!.height / 2, { steps: 8 });
	await page.mouse.up();

	// The saved description keeps the first-row colwidth with its 0 entry.
	await expect.poll(() => {
		const match = issue.description?.match(/colwidth="(\d+),(\d+)"/) ?? null;
		return match ? Number(match[1]) : 0;
	}, { timeout: 10_000 }).toBeGreaterThan(200);
	expect(issue.description).toMatch(/colwidth="\d+,0"/);
	await page.screenshot({ path: testInfo.outputPath('table-merged-resized.png') });

	// Reopening the issue parses the stored widths back into the editor.
	await page.reload();
	await expect(page.locator('.ProseMirror table col').nth(0)).toHaveAttribute('style', /width: 2\d\dpx/);
	expect(pageErrors).toEqual([]);
});

test('issue and comment tables survive editing and reloading', async ({ page }, testInfo) => {
	// API fixtures represent the stored output of a Markdown table and an HTML table.
	const description = '<table><thead><tr><th>Step</th><th align="right">Result</th></tr></thead><tbody><tr><td>Build<br>CI</td><td align="right">Passed</td></tr></tbody></table>';
	const comment = 'Review summary.\n\n<table><thead><tr><th colspan="2">Review checklist</th></tr></thead><tbody><tr><td rowspan="2">UI</td><td>Desktop</td></tr><tr><td align="center">Mobile</td></tr></tbody></table>';
	const { state, pageErrors, issue } = await mockIssue(page, 'owner', description, comment);
	await page.goto('/test/issue/ENG-1');

	const descriptionTable = page.getByRole('table').filter({ has: page.getByRole('columnheader', { name: 'Step' }) });
	const result = descriptionTable.getByRole('cell', { name: 'Passed', exact: true });
	await expect(result).toBeVisible();
	await result.click();
	await page.keyboard.press('End');
	await page.keyboard.insertText(' on CI');
	await expect.poll(() => issue.description).toContain('Passed on CI');

	const mine = commentCard(page, 'Review checklist');
	await expect(mine.getByRole('cell', { name: 'Mobile' })).toBeVisible();
	await mine.hover();
	await mine.getByRole('button', { name: 'Comment actions' }).click();
	await page.getByRole('menuitem', { name: 'Edit' }).click();
	const editor = mine.getByTestId('comment-editor');
	const mobile = editor.getByRole('cell', { name: 'Mobile' });
	// Opening a legacy table keeps the existing cell minimum until a column is resized.
	await expect(mobile).toHaveCSS('min-width', '96px');
	await mobile.getByText('Mobile', { exact: true }).dblclick();
	await page.keyboard.insertText('Mobile checked');
	await page.screenshot({ path: testInfo.outputPath('issue-tables-editing.png') });
	await editor.getByRole('button', { name: 'Save' }).click();
	await expect.poll(() => state.bodies.at(-1)).toContain('Mobile checked');
	await expect(editor).toHaveCount(0);

	await page.reload();
	await expect(descriptionTable.getByRole('cell', { name: 'Passed on CI' })).toHaveAttribute('align', 'right');
	await expect(descriptionTable.getByRole('cell', { name: /Build\s+CI/ }).locator('br')).toHaveCount(1);
	await expect(mine).toContainText('Review summary.');
	await expect(mine.getByRole('table').getByRole('columnheader', { name: 'Review checklist' })).toHaveAttribute('colspan', '2');
	await expect(mine.getByRole('cell', { name: 'UI' })).toHaveAttribute('rowspan', '2');
	await expect(mine.getByRole('cell', { name: 'Mobile checked' })).toHaveAttribute('align', 'center');
	await page.screenshot({ path: testInfo.outputPath('issue-tables.png') });
	expect(pageErrors).toEqual([]);
});

test.describe('mobile workspace comments', () => {
	test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

	test('comment actions are visible before touch and return focus on Escape', async ({ page }, testInfo) => {
		const { state } = await mockIssue(page, 'owner');
		state.live[0].replies = [{ id: 'reply', user_id: me, body: '<p>One more detail</p>' }];
		await page.goto('/test/issue/ENG-1');
		const replyMenu = page.locator('.group\\/reply').filter({ hasText: 'One more detail' }).getByRole('button', { name: 'Comment actions' });
		await replyMenu.scrollIntoViewIfNeeded();
		expect(await replyMenu.evaluate((el) => el.checkVisibility({ checkOpacity: true }))).toBe(true);
		const menu = commentCard(page, 'My first thought').getByRole('button', { name: 'Comment actions' });
		await menu.scrollIntoViewIfNeeded();
		expect(await menu.evaluate((el) => el.checkVisibility({ checkOpacity: true }))).toBe(true);
		await page.screenshot({ path: testInfo.outputPath('touch-comments.png') });
		await menu.tap();
		await expect(page.getByRole('menuitem', { name: 'Edit' })).toBeVisible();
		await page.keyboard.press('Escape');
		await expect(menu).toBeFocused();
		await page.keyboard.press('Enter');
		await expect(page.getByRole('menuitem', { name: 'Delete' })).toBeVisible();
	});
});
