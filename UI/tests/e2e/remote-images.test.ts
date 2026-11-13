import { test, expect, type Page } from '@playwright/test';

const userId = '00000000-0000-0000-0000-000000000001';
const teamId = '00000000-0000-0000-0000-000000000010';
const statusId = '00000000-0000-0000-0000-000000000020';
const createdAt = '2026-01-01T00:00:00Z';
const assetPath = '/api/workspaces/test/assets/11111111-1111-4111-8111-111111111111';
const remoteHost = 'tracker.example';
const pixel = Buffer.from(
	'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
	'base64'
);

// Every request the page makes to the image host, in order.
async function watchRemoteHost(page: Page) {
	const requests: string[] = [];
	await page.route(`https://${remoteHost}/**`, (route) => {
		requests.push(route.request().url());
		return route.fulfill({ contentType: 'image/png', body: pixel });
	});
	return requests;
}

const placeholders = (page: Page) => page.getByRole('group', { name: `Image from ${remoteHost} not loaded` });
const loaded = (page: Page, name: string) =>
	page.getByRole('img', { name }).evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0);

async function mockIssue(page: Page, description: string, comments: string[]) {
	await page.routeWebSocket('**/ws', () => {});
	const state = { assetRequests: 0, writes: [] as string[] };
	const user = { id: userId, email: 'test@example.com', name: 'Test User', display_name: 'Test User', avatar_url: null };
	const workspace = {
		id: '00000000-0000-0000-0000-000000000002',
		name: 'Test Workspace',
		slug: 'test',
		logo_url: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const team = {
		id: teamId,
		name: 'Engineering',
		key: 'ENG',
		description: null,
		color: '#6366f1',
		icon: 'layers',
		triage_enabled: false,
		parent_auto_close_enabled: false,
		sub_issue_auto_close_enabled: false,
		issue_copy_prompt: null,
		created_at: createdAt,
		updated_at: createdAt
	};
	const status = {
		id: statusId,
		team_id: teamId,
		name: 'Todo',
		slug: 'todo',
		category: 'unstarted',
		color: '#6366f1',
		position: 0,
		is_default: true,
		project_ids: [],
		created_at: createdAt,
		updated_at: createdAt
	};
	const issue = {
		id: '00000000-0000-0000-0000-000000000101',
		identifier: 'ENG-1',
		title: 'Remote images wait for the reader',
		description,
		status: 'todo',
		status_id: statusId,
		status_info: { id: statusId, name: 'Todo', category: 'unstarted', color: '#6366f1', position: 0 },
		priority: 3,
		team_id: teamId,
		project_id: null,
		cycle_id: null,
		creator_id: userId,
		assignee_id: userId,
		creator: user,
		assignee: user,
		assignees: [user],
		parent_id: null,
		parent: null,
		due_date: null,
		sort_order: 1000,
		labels: [],
		sub_issue_count: 0,
		sub_issue_done: 0,
		created_at: createdAt,
		updated_at: createdAt
	};
	const commentRows = comments.map((body, index) => ({
		id: `00000000-0000-0000-0000-${String(200 + index).padStart(12, '0')}`,
		issue_id: issue.id,
		user_id: userId,
		body,
		user,
		replies: [],
		created_at: createdAt,
		updated_at: createdAt
	}));

	await page.route('https://raw.githubusercontent.com/**', (route) => route.fulfill({ json: [] }));
	await page.route('**/api/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		if (!path.startsWith('/api/')) return route.continue();
		if (path === assetPath) {
			state.assetRequests++;
			return route.fulfill({ contentType: 'image/png', body: pixel });
		}
		if (request.method() !== 'GET') state.writes.push(`${request.method()} ${path}`);
		if (path === '/api/auth/me') return route.fulfill({ json: user });
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
		if (path === '/api/workspaces') return route.fulfill({ json: [workspace] });
		if (path === '/api/workspaces/test') return route.fulfill({ json: workspace });
		if (path === '/api/workspaces/test/teams') return route.fulfill({ json: [team] });
		if (path === '/api/notifications') return route.fulfill({ json: { notifications: [], unread_count: 0 } });
		if (path === '/api/workspaces/test/issues')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path === '/api/workspaces/test/issues/ENG-1') return route.fulfill({ json: issue });
		if (path === '/api/workspaces/test/issues/ENG-1/comments') return route.fulfill({ json: commentRows });
		if (path === '/api/workspaces/test/issues/ENG-1/github')
			return route.fulfill({ json: { pull_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/issues/ENG-1/forges')
			return route.fulfill({ json: { merge_requests: [], branches: [], commits: [] } });
		if (path === '/api/workspaces/test/dev-machines')
			return route.fulfill({ json: { data: [], total_count: 0, page: 1, has_more: false } });
		if (path.match(/^\/api\/workspaces\/test\/teams\/[^/]+\/statuses$/)) return route.fulfill({ json: [status] });
		if (path.endsWith('/members')) return route.fulfill({ json: [{ user_id: userId, name: 'Test User', role: 'owner' }] });
		return route.fulfill({ json: [] });
	});
	return state;
}

async function mockPublicWiki(page: Page, content: string) {
	await page.route('**/api/**', (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path === assetPath) return route.fulfill({ contentType: 'image/png', body: pixel });
		if (path === '/api/public/wiki/tok123')
			return route.fulfill({
				json: {
					workspace_name: 'Test',
					root_id: 'root',
					pages: [{ id: 'root', parent_id: null, title: 'Handbook', content, position: 0, updated_at: createdAt }]
				}
			});
		return route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED' } } });
	});
}

test('remote images in an issue load only when the reader asks, uploads load at once', async ({ page }) => {
	const commentImage = `https://${remoteHost}/comment.png?reader=1`;
	const descriptionImage = `https://${remoteHost}/description.png`;
	const remoteRequests = await watchRemoteHost(page);
	const state = await mockIssue(page, `<p>Plan</p><p><img src="${descriptionImage}" alt="Diagram"></p>`, [
		`<p>Look at this <img src="${commentImage}" alt="Chart"></p><p><img src="${assetPath}" alt="Upload"></p>`
	]);
	await page.goto('/test/issue/ENG-1');

	// The uploaded image needs no interaction.
	await expect.poll(() => loaded(page, 'Upload')).toBe(true);
	expect(state.assetRequests).toBe(1);

	// One placeholder in the comment, one in the description editor; nothing was fetched.
	await expect(placeholders(page)).toHaveCount(2);
	await expect(placeholders(page).first()).toContainText(remoteHost);
	await page.waitForTimeout(500);
	expect(remoteRequests).toEqual([]);
	await expect(page.locator(`img[src*="${remoteHost}"]`)).toHaveCount(0);

	// The comment's control works from the keyboard and loads that image only.
	const comment = page.locator('.prose:not(.ProseMirror)').filter({ hasText: 'Look at this' });
	const load = comment.getByRole('button', { name: 'Load image' });
	await load.focus();
	await page.keyboard.press('Enter');
	await expect.poll(() => loaded(page, 'Chart')).toBe(true);
	await expect(comment.getByRole('img', { name: 'Chart' })).toHaveAttribute('src', commentImage);
	expect(remoteRequests).toEqual([commentImage]);
	await expect(placeholders(page)).toHaveCount(1);

	// The description is rendered by the editor, which waits the same way.
	const editor = page.locator('.tiptap').filter({ hasText: 'Plan' });
	await editor.getByRole('button', { name: 'Load image' }).click();
	await expect.poll(() => loaded(page, 'Diagram')).toBe(true);
	expect(remoteRequests).toEqual([commentImage, descriptionImage]);
	await expect(placeholders(page)).toHaveCount(0);

	// Loading is a choice of this reader: the stored content is not saved back.
	await page.waitForTimeout(1200);
	expect(state.writes).toEqual([]);
});

test('a public wiki page does not fetch remote images until asked', async ({ page }) => {
	const image = `https://${remoteHost}/public.png`;
	const remoteRequests = await watchRemoteHost(page);
	await mockPublicWiki(
		page,
		`<h2>Start</h2><p>Welcome <img src="${image}" alt="Banner"></p><p><img src="${assetPath}" alt="Upload"></p>`
	);
	await page.goto('/share/wiki/tok123');

	await expect(page.getByRole('heading', { name: 'Handbook', level: 1 })).toBeVisible();
	await expect.poll(() => loaded(page, 'Upload')).toBe(true);
	await expect(placeholders(page)).toHaveCount(1);
	await page.waitForTimeout(500);
	expect(remoteRequests).toEqual([]);

	await page.getByRole('button', { name: 'Load image' }).click();
	await expect.poll(() => loaded(page, 'Banner')).toBe(true);
	expect(remoteRequests).toEqual([image]);
	await expect(placeholders(page)).toHaveCount(0);
});

test('a reader can always load images from a host they trust', async ({ page }) => {
	const first = `https://${remoteHost}/first.png`;
	const second = `https://${remoteHost}/second.png`;
	const remoteRequests = await watchRemoteHost(page);
	await mockPublicWiki(page, `<p><img src="${first}" alt="First"></p><p><img src="${second}" alt="Second"></p>`);
	await page.goto('/share/wiki/tok123');

	await expect(placeholders(page)).toHaveCount(2);
	expect(remoteRequests).toEqual([]);
	await page
		.getByRole('button', { name: `Always load from ${remoteHost}` })
		.first()
		.click();
	await expect.poll(() => loaded(page, 'Second')).toBe(true);
	await expect(placeholders(page)).toHaveCount(0);
	expect([...remoteRequests].sort()).toEqual([first, second]);

	// The choice is kept for this browser.
	await page.reload();
	await expect.poll(() => loaded(page, 'First')).toBe(true);
	await expect(placeholders(page)).toHaveCount(0);
	expect(remoteRequests).toHaveLength(4);
});
